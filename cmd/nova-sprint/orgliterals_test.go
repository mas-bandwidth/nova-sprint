package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoOrgLiteralsOutsideImports: no string in the shipped Go code names
// our org; a fork with another org works by configuration alone (the
// install repository is NOVA_SPRINT_REPO or the module path, nova-tools is
// matched under any org). Import paths are the module's, not a literal.
func TestNoOrgLiteralsOutsideImports(t *testing.T) {
	t.Parallel()
	const org = "mas-bandwidth"
	fset := token.NewFileSet()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != "../.." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if _, ok := n.(*ast.ImportSpec); ok {
				return false
			}
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, org) {
				t.Errorf("%s: a string names the org: %s", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
