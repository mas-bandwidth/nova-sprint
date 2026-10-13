package main

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// setVerbFlags is every flag cmdSet declares (fs.String, fs.Bool, ... with a literal name),
// read from the verb's own source, so a flag added to `set` by any path (a SetReq field or a
// With* wrapper) is seen.
func setVerbFlags(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(gotoken.NewFileSet(), "verbs.go", nil, 0)
	require.NoError(t, err)
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "cmdSet" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "fs" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == gotoken.STRING {
				name, _ := strconv.Unquote(lit.Value)
				out = append(out, name)
			}
			return true
		})
	}
	return out
}

// undeclaredFlags is every flag that is neither a listed non-setting nor maps to a
// property row (HomeProp or HomeCarry) of sprint.Defaults.
func undeclaredFlags(flags []string) []string {
	var out []string
	for _, fl := range flags {
		if slices.Contains(sprint.NotSettings, "set --"+fl) {
			continue
		}
		prop, ok := sprint.SetFlagProp[fl]
		if !ok {
			prop = strings.ReplaceAll(fl, "-", "_")
		}
		d, ok := sprint.DefaultOf(prop)
		if !ok || (d.Home != sprint.HomeProp && d.Home != sprint.HomeCarry) {
			out = append(out, "--"+fl+" ("+prop+")")
		}
	}
	return out
}

// TestEverySetFlagHasADeclaredDefault is the class test over the `set` verb's full flag
// list (its SetReq fields and its With* wrappers alike: friend-finish, pegged-bound, base):
// each flag maps to a Defaults row the clear's set-defaults step writes or carries, or is a
// listed non-setting. Reversed witness: a new flag with no row is red.
func TestEverySetFlagHasADeclaredDefault(t *testing.T) {
	t.Parallel()
	flags := setVerbFlags(t)
	require.GreaterOrEqual(t, len(flags), 20, "the set verb's flags were read: %v", flags)
	for _, want := range []string{"friend-finish", "pegged-bound", "base", "read-cards", "alarm-ready"} {
		require.Contains(t, flags, want, "the wrapper flags are read too")
	}
	assert.Empty(t, undeclaredFlags(flags), "a set flag with no declared default")
	assert.Equal(t, []string{"--new-knob (new_knob)"}, undeclaredFlags(append(flags, "new-knob")), "the witness is red")
}
