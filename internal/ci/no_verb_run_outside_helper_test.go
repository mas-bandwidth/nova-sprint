package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// verbSet is every single-word verb of nova-sprint (verbs.go): the first word of
// the argument list a verb runs with. A test that runs one without going through
// the functional helper could forward to a live sprint server (forward.go), so it
// is what this scan looks for.
var verbSet = map[string]bool{
	"init": true, "add": true, "quack": true, "preflight": true, "release": true,
	"resolve": true, "start": true, "stop": true, "run": true, "tick": true,
	"selftest": true, "take": true, "finish": true, "progress": true, "ask": true,
	"queue": true, "read": true, "accept": true, "rework": true, "return": true,
	"redo": true, "drop": true, "defer": true, "priority": true, "unpin": true,
	"rebase": true, "rank": true, "relink": true, "recut": true, "twin": true,
	"brief": true, "move": true, "tier": true, "merge": true, "land": true,
	"verify": true, "landed": true, "snapshot": true, "backup": true, "demo": true,
	"promote": true, "resume": true, "hold": true, "unhold": true, "fleet": true,
	"friend": true, "collect": true, "gc": true, "where": true, "inbox": true,
	"lane": true, "log": true, "version": true, "help": true, "memo": true,
	"seat": true, "set": true,
}

// helperFile is the one test file that runs verbs on the app directly (through
// the helper's runVerbOut / runInto); it is the allowed site.
const helperFile = "testhelpers_functional_test.go"

// TestNoTestRunsAVerbOutsideTheFunctionalHelper is the guard for the cold-read
// finds (#110's canary and #117's landing-stall test): a test that runs a
// nova-sprint verb any way but the functional helper could forward its verbs to a
// live sprint server. It scans every _test.go (functional tag or not) for a
// direct exec of the binary with a verb, and every app.run call whose receiver
// is an *app (typed, so a verb list built at runtime is still caught), outside
// the helper file.
func TestNoTestRunsAVerbOutsideTheFunctionalHelper(t *testing.T) {
	root := "../.."
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") && d.Name() != helperFile {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(files)

	var problems []string
	for _, path := range files {
		problems = append(problems, scanExecsBinaryWithVerb(path)...)
	}
	problems = append(problems, scanAppRunSites(t, root)...)
	require.Empty(t, problems, "a verb is run outside the functional helper:\n%s", strings.Join(problems, "\n"))
}

// scanExecsBinaryWithVerb finds a direct exec of the nova-sprint binary, in
// the one file: the source-level scan for running the verb as a binary. It
// flags an exec of the binary however the binary is named — a literal path, or
// a variable assigned from a path or a `go build` that names nova-sprint — and
// whether or not it carries its own --redis (the functional helper's safety
// marker). A build of the binary (`go build ...`) builds the command "go", so
// it is not flagged; the exec of the built binary is.
func scanExecsBinaryWithVerb(path string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil
	}
	binaryVars := collectBinaryVariables(f)
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Command" && execsNovaSprint(ce.Args, binaryVars) {
			pos := fset.Position(ce.Pos())
			out = append(out, pos.String()+": execs the nova-sprint binary without its own --redis; run the verb through the functional helper instead")
		}
		return true
	})
	return out
}

// collectBinaryVariables is the identifiers in a file whose value names the
// nova-sprint binary: a literal path whose basename is nova-sprint, or a
// filepath.Join whose last element is one. A test that builds the binary into a
// variable (exec.Command("go","build","-o",bin,"../nova-sprint")) and runs it
// later (exec.Command(bin, ...)) is caught by looking the exec up against these.
func collectBinaryVariables(f *ast.File) map[string]bool {
	vars := map[string]bool{}
	record := func(name string, e ast.Expr) {
		if name != "" && isNovaSprintName(e) {
			vars[name] = true
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(v.Rhs) {
					continue
				}
				record(id.Name, v.Rhs[i])
			}
		case *ast.ValueSpec:
			for i, id := range v.Names {
				if i < len(v.Values) {
					record(id.Name, v.Values[i])
				}
			}
		}
		return true
	})
	return vars
}

// isNovaSprintName reports whether an expression names the nova-sprint binary: a
// string literal whose basename is nova-sprint, or a filepath.Join whose last
// element is one.
func isNovaSprintName(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING && filepath.Base(strings.Trim(v.Value, `"`)) == "nova-sprint"
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Join" || len(v.Args) == 0 {
			return false
		}
		return isNovaSprintName(v.Args[len(v.Args)-1])
	}
	return false
}

// execsNovaSprint is whether the call execs the nova-sprint binary: the command
// (first argument) is the binary — a literal path or a tracked variable — and
// the call does not carry its own --redis (the functional helper always passes
// one, and a verb holding one never forwards, forward.go). A built binary is
// caught by its variable, so a test that builds then runs it is reported.
func execsNovaSprint(args []ast.Expr, binaryVars map[string]bool) bool {
	if len(args) == 0 {
		return false
	}
	first := args[0]
	binary := false
	switch v := first.(type) {
	case *ast.Ident:
		binary = binaryVars[v.Name]
	default:
		binary = isNovaSprintName(first)
	}
	if !binary {
		return false
	}
	for _, a := range args[1:] {
		if s := literalValue(a); s == "--redis" {
			return false
		}
	}
	return true
}

// scanAppRunSites loads the cmd/nova-sprint package (its test files, functional
// tag included) and flags every .run call whose receiver is an *app, outside the
// helper file. The receiver type is the go/types one, so a run whose verb list is
// built at runtime (append(strings.Fields(...), ...), a test's withEpoch(split(...)),
// and the like) is still flagged.
func scanAppRunSites(t *testing.T, root string) []string {
	cfg := &packages.Config{
		Mode:       packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedFiles,
		Dir:        root,
		Tests:      true,
		BuildFlags: []string{"-tags", "functional"},
	}
	pkgs, err := packages.Load(cfg, "./cmd/nova-sprint")
	if err != nil {
		require.NoError(t, err, "loading cmd/nova-sprint")
	}
	if packages.PrintErrors(pkgs) > 0 {
		var msgs []string
		packages.Visit(pkgs, nil, func(p *packages.Package) {
			for _, e := range p.Errors {
				msgs = append(msgs, e.Error())
			}
		})
		require.Empty(t, msgs, "cmd/nova-sprint does not type-check:\n%s", strings.Join(msgs, "\n"))
	}

	var out []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.TypesInfo == nil {
			return
		}
		for _, f := range p.Syntax {
			name := filepath.Base(p.Fset.Position(f.Pos()).Filename)
			if !strings.HasSuffix(name, "_test.go") || name == helperFile {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := ce.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "run" {
					return true
				}
				if receiverIsApp(p.TypesInfo, sel.X) {
					pos := p.Fset.Position(ce.Pos())
					out = append(out, pos.String()+": runs a verb direct on the app; use the functional helper (newFunctionalTestApp then runVerb)")
				}
				return true
			})
		}
	})
	sort.Strings(out)
	return out
}

// receiverIsApp reports whether the expression is an app (a pointer to the
// package main app type).
func receiverIsApp(info *types.Info, x ast.Expr) bool {
	t := info.TypeOf(x)
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "app" && obj.Pkg() != nil && obj.Pkg().Name() == "main"
}

// literalValue is the string value of an expression: a string literal itself, a
// composite literal as its first element's value, or a concatenation's first
// part.
func literalValue(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			return strings.Trim(v.Value, `"`)
		}
	case *ast.CompositeLit:
		if len(v.Elts) > 0 {
			return literalValue(v.Elts[0])
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return literalValue(v.X)
		}
	}
	return ""
}
