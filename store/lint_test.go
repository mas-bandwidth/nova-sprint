package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Unit tests use no real time, no sockets and no outside services (Glenn,
// 2026-10-02): anything that starts a PostgreSQL or reads the clock is a
// functional test, and its name says so. This refuses a Test function whose
// name lacks "Functional" and that reads time.Now itself or reaches startPG or the initdb
// binary through the package's own functions. (Helpers that stamp an Op with
// time.Now are not followed: the Fake takes the time as data, so no test waits
// on it.)
func TestUnitTestsUseNoRealThing(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	type fnInfo struct {
		file  string
		calls map[string]bool // package-level names it mentions
		real  []string        // the servers it starts itself
		clock bool            // it reads time.Now itself
	}
	fns := map[string]*fnInfo{}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err, name)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil { // methods too, by name: coarse, and only ever too strict
				continue
			}
			info := &fnInfo{file: name, calls: map[string]bool{}}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Ident:
					info.calls[x.Name] = true
					if x.Name == "startPG" {
						info.real = append(info.real, "startPG")
					}
				case *ast.BasicLit:
					if x.Kind == token.STRING && x.Value == `"`+"init"+`db"` {
						info.real = append(info.real, "the initdb binary")
					}
				case *ast.SelectorExpr:
					if p, ok := x.X.(*ast.Ident); ok && p.Name == "time" && x.Sel.Name == "Now" {
						info.clock = true
					}
				}
				return true
			})
			fns[fn.Name.Name] = info
		}
	}
	var bad []string
	for name, info := range fns {
		if !strings.HasPrefix(name, "Test") || strings.Contains(name, "Functional") {
			continue
		}
		if info.clock {
			bad = append(bad, info.file+": "+name+" reads time.Now")
		}
		seen := map[string]bool{name: true}
		queue := []string{name}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, r := range fns[cur].real {
				bad = append(bad, info.file+": "+name+" reaches "+r+" in "+cur)
			}
			for c := range fns[cur].calls {
				if _, ok := fns[c]; ok && !seen[c] {
					seen[c] = true
					queue = append(queue, c)
				}
			}
		}
	}
	sort.Strings(bad)
	require.Empty(t, bad, "rename these TestFunctional..., or give them the time as an argument")
}
