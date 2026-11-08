// Command unitlint refuses a unit test that uses real time, sleeps, sockets,
// the network or a child process, and any test, unit or functional, that
// does not call t.Parallel() first (docs/TESTING.md). CI runs the unit tier with
// -skip Functional and the functional tier with -run Functional, so a unit test
// is a test, fuzz target or example whose name does not contain "Functional";
// the helpers it reaches in its package's test files are checked with it.
// An exception is a comment on the offending line (for t.Parallel, the func
// line) that says "unitlint:" and why.
//
//	go run ./tools/unitlint [dir]
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// banned maps an import path to the names in it a unit test may not use.
var banned = map[string]map[string]bool{
	"time":    set("Now", "Sleep", "After", "AfterFunc", "Tick", "NewTicker", "NewTimer", "Since", "Until"),
	"context": set("WithTimeout", "WithDeadline", "WithTimeoutCause", "WithDeadlineCause"),
	"net": set("Dial", "DialTimeout", "DialTCP", "DialUDP", "DialUnix", "DialIP", "Dialer",
		"Listen", "ListenPacket", "ListenTCP", "ListenUDP", "ListenUnix", "ListenUnixgram", "ListenIP",
		"LookupHost", "LookupIP", "LookupAddr", "LookupPort", "LookupCNAME", "LookupMX", "LookupTXT"),
	"net/http/httptest": set("NewServer", "NewTLSServer", "NewUnstartedServer"),
	"os/exec":           set("Command", "CommandContext"),
}

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	dirs := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (d.Name() == "testdata" || d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, "_test.go") {
			dirs[filepath.Dir(path)] = append(dirs[filepath.Dir(path)], path)
		}
		return nil
	})
	bad := 0
	keys := make([]string, 0, len(dirs))
	for dir := range dirs {
		keys = append(keys, dir)
	}
	sort.Strings(keys)
	for _, dir := range keys {
		if err != nil {
			break
		}
		var n int
		n, err = check(dirs[dir])
		bad += n
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "unitlint:", err)
		os.Exit(2)
	}
	if bad > 0 {
		fmt.Fprintf(os.Stderr, "unitlint: %d finding(s): a unit test runs in parallel and uses no real time, sleep, network or processes; put Functional in the test's name, or take time as an argument\n", bad)
		os.Exit(1)
	}
}

// unit reports whether a top-level function is a unit-tier root.
func unit(name string) bool {
	if strings.Contains(name, "Functional") {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example")
}

type file struct {
	fset    *token.FileSet
	f       *ast.File
	local   map[string]string // import name -> banned path
	excused map[int]bool
}

// check reports the banned uses reached from the unit-tier tests of one
// directory's test files (both the package and its _test package).
func check(paths []string) (int, error) {
	serial := 0
	var files []file
	funcs := map[string][]*ast.FuncDecl{}   // helper functions by name
	methods := map[string][]*ast.FuncDecl{} // methods by name, of any type
	var roots []ast.Node
	for _, path := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return 0, err
		}
		fl := file{fset: fset, f: f, local: map[string]string{}, excused: map[int]bool{}}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if _, ok := banned[p]; ok {
				name := p[strings.LastIndex(p, "/")+1:]
				if imp.Name != nil {
					name = imp.Name.Name
				}
				fl.local[name] = p
			}
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.Contains(c.Text, "unitlint:") {
					fl.excused[fset.Position(c.Pos()).Line] = true
				}
			}
		}
		files = append(files, fl)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && fn.Name.Name != "TestMain" && !parallel(fn) && !fl.excused[fset.Position(fn.Pos()).Line] {
				fmt.Printf("%s: %s does not call t.Parallel() first\n", fset.Position(fn.Pos()), fn.Name.Name)
				serial++
			}
			switch {
			case !ok:
				roots = append(roots, decl)
			case fn.Recv == nil && unit(fn.Name.Name):
				roots = append(roots, fn)
			case fn.Recv != nil:
				methods[fn.Name.Name] = append(methods[fn.Name.Name], fn)
			case !strings.HasPrefix(fn.Name.Name, "Test") && !strings.HasPrefix(fn.Name.Name, "Benchmark"):
				funcs[fn.Name.Name] = append(funcs[fn.Name.Name], fn)
			}
		}
	}
	// Reach every helper named from a root, transitively.
	reached := map[ast.Node]bool{}
	queue := roots
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if reached[n] {
			continue
		}
		reached[n] = true
		ast.Inspect(n, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.SelectorExpr:
				for _, fn := range methods[x.Sel.Name] {
					queue = append(queue, fn)
				}
				ast.Inspect(x.X, func(y ast.Node) bool { // x.Sel is a method or field, never a helper
					if id, ok := y.(*ast.Ident); ok {
						for _, fn := range funcs[id.Name] {
							queue = append(queue, fn)
						}
					}
					return true
				})
				return false
			case *ast.Ident:
				for _, fn := range funcs[x.Name] {
					queue = append(queue, fn)
				}
			}
			return true
		})
	}
	bad := serial
	for _, fl := range files {
		if len(fl.local) == 0 {
			continue
		}
		for _, decl := range fl.f.Decls {
			if !reached[decl] {
				continue
			}
			where := "a package-level declaration"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				where = fn.Name.Name
			}
			ast.Inspect(decl, func(x ast.Node) bool {
				sel, ok := x.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || id.Obj != nil {
					return true
				}
				p, ok := fl.local[id.Name]
				if !ok || !banned[p][sel.Sel.Name] {
					return true
				}
				pos := fl.fset.Position(sel.Pos())
				if fl.excused[pos.Line] {
					return true
				}
				fmt.Printf("%s: %s.%s in %s, which the unit tier runs\n", pos, id.Name, sel.Sel.Name, where)
				bad++
				return true
			})
		}
	}
	return bad, nil
}

// parallel reports whether a test's first statement is t.Parallel().
func parallel(fn *ast.FuncDecl) bool {
	if len(fn.Body.List) == 0 || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) != 1 {
		return false
	}
	es, ok := fn.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Parallel" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == fn.Type.Params.List[0].Names[0].Name
}
