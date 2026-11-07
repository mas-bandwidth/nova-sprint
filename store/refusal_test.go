package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A live store holds the sprint's cards and its log: a refusal never advises
// recreating it (that is how every card was lost on 2026-11-06), and never
// names a caller's environment variable (the store takes a dsn).
func TestRefusalsNeverAdviseRecreate(t *testing.T) {
	// Every text Migrate and CheckSchema can refuse a store with that is built
	// without a database.
	texts := map[string]string{"foreign schema": errForeign.Error()}
	for name, c := range map[string]struct {
		version int
		hash    string
	}{
		"other version": {7, schemaHash},
		"old version":   {1, ""},
		"other hash":    {SchemaVersion, "abc"},
		"no hash":       {SchemaVersion, ""},
	} {
		err := metaRefusal(c.version, c.hash)
		require.Error(t, err, name)
		texts[name] = err.Error()
	}
	for name, text := range texts {
		assert.NotContains(t, strings.ToLower(text), "recreate", name)
		assert.NotContains(t, text, "NOVA_", name)
	}

	// The rest of the refusals need a database to reach, so the source says it:
	// no string literal in the store's own code advises recreating or names an
	// environment variable.
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err, name)
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				assert.NotContains(t, strings.ToLower(lit.Value), "recreate", "%s: %s", name, lit.Value)
				assert.NotContains(t, lit.Value, "NOVA_PG", "%s: %s", name, lit.Value)
			}
			return true
		})
	}
}
