package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// walkShipped calls visit for every file of the repository the binary is
// built from with one of exts: tests, testdata and dot directories are not
// shipped.
func walkShipped(t *testing.T, visit func(path string), exts ...string) {
	t.Helper()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != "../.." {
			return filepath.SkipDir
		}
		if d.IsDir() || strings.HasSuffix(path, "_test.go") || !slices.ContainsFunc(exts, func(e string) bool { return strings.HasSuffix(path, e) }) {
			return nil
		}
		visit(path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// providerNames are API-rate providers and models a sprint may use; none is
// named in the shipped code or the built-in policy, in a string, an
// identifier or a comment. A provider is the sprint's own policy's
// catalog() "providers"; tests use made-up names.
var providerNames = []string{"deepseek", "inception", "mercury", "openrouter"}

// TestNoProviderNamesInCode: the shipped Go code and the built-in policy
// name no provider; a sprint's providers come from its policy alone.
func TestNoProviderNamesInCode(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	named := func(at, text string) {
		lower := strings.ToLower(text)
		for _, name := range providerNames {
			if strings.Contains(lower, name) {
				t.Errorf("%s names the provider %s: %s", at, name, strings.TrimSpace(text))
			}
		}
	}
	walkShipped(t, func(path string) {
		if strings.HasSuffix(path, ".star") {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			named(path, string(b))
			return
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range f.Comments {
			named(fset.Position(c.Pos()).String(), c.Text())
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					named(fset.Position(x.Pos()).String(), x.Value)
				}
			case *ast.Ident:
				named(fset.Position(x.Pos()).String(), x.Name)
			}
			return true
		})
	}, ".go", ".star")
}
