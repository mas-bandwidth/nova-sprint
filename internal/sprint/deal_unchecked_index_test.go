package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dealPathFiles are the files of the deal path: Deal and dealPlan, and the
// files of the functions dealPlan calls that live in internal/sprint
// (round.go, width.go, fleet_quiet.go; gate_host.go when present).
var dealPathFiles = []string{
	"bench_deal.go",
	"steps_work.go",
	"round.go",
	"width.go",
	"fleet_quiet.go",
	"gate_host.go",
}

// TestNoDealPathIndexesAnUncheckedSlice is the class test for the panic at
// bench_deal.go:137: benchRefusal indexed bench[0] on an empty bench. Every
// constant index on a slice or string identifier in the deal path sits under a
// guard naming len(x) in an enclosing if or for condition of the same function,
// or carries the comment `// nonempty: {why}` on its line. It lists each site
// it finds, so a new unguarded one fails it by file:line. No process, no
// network: go/parser and go/ast on the package's own source.
func TestNoDealPathIndexesAnUncheckedSlice(t *testing.T) {
	t.Parallel()
	var sites []string
	for _, name := range dealPathFiles {
		src, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		require.NoError(t, err)
		lines := strings.Split(string(src), "\n")
		parents := parentMap(f)
		ast.Inspect(f, func(n ast.Node) bool {
			ix, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			id, ok := ix.X.(*ast.Ident)
			if !ok || !constantIndex(ix.Index) {
				return true
			}
			if lenGuarded(parents, ix, id.Name) {
				return true
			}
			pos := fset.Position(ix.Pos())
			if pos.Line >= 1 && pos.Line <= len(lines) && strings.Contains(lines[pos.Line-1], "// nonempty:") {
				return true
			}
			sites = append(sites, pos.String()+" "+id.Name+"[...]")
			return true
		})
	}
	assert.Empty(t, sites, "every constant index on a slice or string identifier sits under a len() guard or carries // nonempty:\n%s", strings.Join(sites, "\n"))
}

// parentMap is every node's parent in f, for walking up to an index's guards.
func parentMap(f *ast.File) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// lenGuarded says the index sits under an enclosing if or for condition of the
// same function that names len(name).
func lenGuarded(parents map[ast.Node]ast.Node, ix *ast.IndexExpr, name string) bool {
	for cur := parents[ix]; cur != nil; cur = parents[cur] {
		var cond ast.Expr
		switch s := cur.(type) {
		case *ast.IfStmt:
			cond = s.Cond
		case *ast.ForStmt:
			cond = s.Cond
		default:
			continue
		}
		if cond != nil && namesLen(cond, name) {
			return true
		}
	}
	return false
}

// namesLen says n contains a call len(name).
func namesLen(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "len" || len(call.Args) != 1 {
			return true
		}
		arg, ok := call.Args[0].(*ast.Ident)
		if ok && arg.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// constantIndex says e is an integer literal index (x[0], x[1]).
func constantIndex(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return false
	}
	_, err := strconv.Atoi(lit.Value)
	return err == nil
}
