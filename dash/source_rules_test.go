package dash

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestStoreOpenRunsUnderDeadline checks that cmd/nova-sprint/open.go calls store.Open with a context that
// has a deadline, so an unreachable PostgreSQL cannot hold startup for the OS connect timeout.
func TestStoreOpenRunsUnderDeadline(t *testing.T) {
	path := filepath.Join("..", "cmd", "nova-sprint", "open.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	deadlined := map[string]bool{}
	var openArg string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Rhs) == 1 {
				if c, ok := x.Rhs[0].(*ast.CallExpr); ok {
					if s, ok := c.Fun.(*ast.SelectorExpr); ok && (s.Sel.Name == "WithTimeout" || s.Sel.Name == "WithDeadline") {
						if id, ok := x.Lhs[0].(*ast.Ident); ok {
							deadlined[id.Name] = true
						}
					}
				}
			}
		case *ast.CallExpr:
			if s, ok := x.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Open" {
				if p, ok := s.X.(*ast.Ident); ok && p.Name == "store" && len(x.Args) > 0 {
					if id, ok := x.Args[0].(*ast.Ident); ok {
						openArg = id.Name
					}
				}
			}
		}
		return true
	})
	if openArg == "" {
		t.Fatal("store.Open call not found; recheck open.go")
	}
	if !deadlined[openArg] {
		t.Errorf("open.go: store.Open(%s, ...) has no deadline (%s is not from context.WithTimeout/WithDeadline)", openArg, openArg)
	}
}

// TestReadmeStatesTrackFixRule checks that README.md has a sentence stating the Fleet and Friends track's
// fix rule (a working work consumer at attempt 2 or more).
func TestReadmeStatesTrackFixRule(t *testing.T) {
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range regexp.MustCompile(`[^.]*\.`).FindAllString(string(b), -1) {
		l := strings.ToLower(s)
		if strings.Contains(l, "track") && strings.Contains(l, "attempt") {
			return
		}
	}
	t.Error("README has no sentence stating the track's fix rule (working work consumer, attempt >= 2)")
}

// TestNoUnusedDashboardSymbols checks that three symbols with no reader stay out of the non-test code:
// Cache.Data, workRow.Unpriced and the unreachable "-" branch of fleetRow.MarshalJSON.
func TestNoUnusedDashboardSymbols(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	s := src.String()
	var dead []string
	if strings.Contains(s, "func (c *Cache) Data() any") && !strings.Contains(s, ".Data()") {
		dead = append(dead, "Cache.Data (no non-test caller)")
	}
	reads := strings.Count(s, "Unpriced") - strings.Count(s, "Unpriced   bool") - strings.Count(s, "w.Unpriced = true")
	if strings.Contains(s, "Unpriced   bool") && reads == 0 {
		dead = append(dead, "workRow.Unpriced (never read)")
	}
	if strings.Contains(s, "f.OKPct = &p") && strings.Contains(s, "if f.OKPct != nil {") {
		dead = append(dead, `fleetRow.MarshalJSON "-" branch (OKPct never nil)`)
	}
	if len(dead) > 0 {
		t.Errorf("dead code remains: %s", strings.Join(dead, "; "))
	}
}

// TestFunctionsAreUnder60Lines checks that no production function in this package is over 60 lines; test
// functions over the limit are only logged.
func TestFunctionsAreUnder60Lines(t *testing.T) {
	for _, pattern := range []string{"*.go"} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				n := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
				if n <= 60 {
					continue
				}
				if strings.HasSuffix(path, "_test.go") {
					t.Logf("test function %s:%s is %d lines", path, fn.Name.Name, n)
				} else {
					t.Errorf("%s:%s is %d lines", path, fn.Name.Name, n)
				}
			}
		}
	}
}
