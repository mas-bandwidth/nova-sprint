package steps

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLintPure: the package reads no clock and no outside world (list item
// 2): no time.Now, no net, no os/exec, no store.Store; Now is an input and
// the store is values. And every function is under 60 lines (the build
// rules).
func TestLintPure(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			assert.NotContains(t, []string{"net", "os/exec", "os", "net/http", "os/signal", "syscall"}, p, "%s imports %s", name, p)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok {
					sel := id.Name + "." + x.Sel.Name
					assert.NotEqual(t, "time.Now", sel, "%s reads the clock at %s", name, fset.Position(x.Pos()))
					assert.NotEqual(t, "store.Store", sel, "%s names the store at %s; the steps take values", name, fset.Position(x.Pos()))
					assert.NotEqual(t, "time.Since", sel, "%s reads the clock at %s", name, fset.Position(x.Pos()))
					assert.NotEqual(t, "time.Sleep", sel, "%s sleeps at %s", name, fset.Position(x.Pos()))
				}
			case *ast.FuncDecl:
				if x.Body != nil {
					lines := fset.Position(x.Body.Rbrace).Line - fset.Position(x.Body.Lbrace).Line + 1
					assert.LessOrEqual(t, lines, 60, "%s: %s is %d lines", name, x.Name.Name, lines)
				}
			}
			return true
		})
	}
}
