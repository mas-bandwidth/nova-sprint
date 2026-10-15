package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoRetiredOneSourceCopyReturns is the guard that the retired copies of
// nova-tools code do not come back. The roadmap, the s-expression reader and the
// Limits that bounds it have one source: nova-tools pkg/roadmap and pkg/sexp
// (nova-tools#5746). nova-sprint carried its own copies (internal/roadmapdoc,
// internal/worklang, tools/roadmap), which is why the 10 MiB roadmap bound of
// nova-tools#5811 landed twice, as nova-sprint#349: Limits was defined in both
// repos. Every retired path is one row of internal/ci/testdata/one-source-retired.txt
// with what replaced it, and this scan refuses each listed path that exists, plus
// any Go type in the module named Limits with the fields MaxBytes, MaxDepth and
// MaxNodes: the copy returning under another path. Value literals of sexp.Limits
// (land_ledgers.go's 16 MiB evidence bound, internal/workfile's tree bound) are
// uses, not copies, and stay.
func TestNoRetiredOneSourceCopyReturns(t *testing.T) {
	t.Parallel()
	root := "../.."
	raw, err := os.ReadFile(filepath.Join(root, oneSourceRetiredTable))
	require.NoError(t, err)
	rows, faults := parseRetiredPaths(string(raw))
	require.Empty(t, faults, "%s", oneSourceRetiredTable)
	require.NotEmpty(t, rows, "%s names nothing", oneSourceRetiredTable)

	problems := retiredPathProblems(root, rows)
	found, err := limitsCopyProblems(root)
	require.NoError(t, err, "scanning the module for a Limits type")
	problems = append(problems, found...)
	sort.Strings(problems)
	assert.Empty(t, problems, "one true source: the roadmap and sexp readers are %s's pkg/roadmap and pkg/sexp (%s):\n%s",
		novaToolsModule, oneSourceRetiredTable, strings.Join(problems, "\n"))
}

// oneSourceRetiredTable names the retired paths: "<path>\t<what replaced it>" a
// line, '#' lines comments.
const oneSourceRetiredTable = "internal/ci/testdata/one-source-retired.txt"

// retiredPath is one row of the table.
type retiredPath struct{ Path, Replacement string }

// parseRetiredPaths reads the table: blank lines and # comments skipped. A row
// that is not `<path><TAB><what replaced it>`, with both sides, is a fault: every
// retirement names what took its place.
func parseRetiredPaths(src string) ([]retiredPath, []string) {
	var rows []retiredPath
	var faults []string
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, replacement, ok := strings.Cut(line, "\t")
		p, replacement = strings.TrimSpace(p), strings.TrimSpace(replacement)
		if !ok || p == "" || replacement == "" {
			faults = append(faults, fmt.Sprintf("line %d: want `<path><TAB><what replaced it>`, got %q", i+1, line))
			continue
		}
		rows = append(rows, retiredPath{Path: p, Replacement: replacement})
	}
	return rows, faults
}

// retiredPathProblems is every row whose path exists under root.
func retiredPathProblems(root string, rows []retiredPath) []string {
	var out []string
	for _, r := range rows {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(r.Path))); err == nil {
			out = append(out, fmt.Sprintf("%s is retired; use %s", r.Path, r.Replacement))
		}
	}
	return out
}

// limitsCopyProblems walks the module's .go files and reports every Limits
// definition limitsDeclarations finds, as "<rel>:<line>: ...".
func limitsCopyProblems(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "tmp", "bin", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, limitsDeclarations(filepath.ToSlash(rel), src)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// limitsBoundFields are the three bounds of sexp.Limits: a Limits type with all
// three is the copy, whatever file it stands in.
var limitsBoundFields = map[string]bool{"MaxBytes": true, "MaxDepth": true, "MaxNodes": true}

// limitsDeclarations is every Limits definition in one .go file's source, as
// "<rel>:<line>: ..." lines: a type named Limits whose struct has the three
// bound fields, or a package-level `var Limits = ...{MaxBytes: ..., MaxDepth:
// ..., MaxNodes: ...}`. A function named Limits, or a literal under any other
// name, is a use of sexp.Limits, not a copy, and is not reported.
func limitsDeclarations(rel string, src []byte) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil // not a Go file the scan can read: the build gate owns that
	}
	var out []string
	for _, decl := range f.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch g.Tok {
		case token.TYPE:
			for _, spec := range g.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != "Limits" {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || !hasFields(st, limitsBoundFields) {
					continue
				}
				out = append(out, fmt.Sprintf("%s:%d: type Limits with MaxBytes, MaxDepth and MaxNodes is defined here; use %s/pkg/sexp",
					rel, fset.Position(ts.Pos()).Line, novaToolsModule))
			}
		case token.VAR:
			for _, spec := range g.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "Limits" || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok || !hasKeys(lit, limitsBoundFields) {
					continue
				}
				out = append(out, fmt.Sprintf("%s:%d: var Limits with MaxBytes, MaxDepth and MaxNodes is defined here; use %s/pkg/sexp",
					rel, fset.Position(vs.Pos()).Line, novaToolsModule))
			}
		}
	}
	return out
}

// hasFields says whether st declares every name in want as a field.
func hasFields(st *ast.StructType, want map[string]bool) bool {
	seen := map[string]bool{}
	if st.Fields == nil {
		return false
	}
	for _, fld := range st.Fields.List {
		for _, n := range fld.Names {
			seen[n.Name] = true
		}
	}
	for name := range want {
		if !seen[name] {
			return false
		}
	}
	return true
}

// hasKeys says whether lit carries every name in want as a keyed element.
func hasKeys(lit *ast.CompositeLit, want map[string]bool) bool {
	seen := map[string]bool{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				seen[id.Name] = true
			}
		}
	}
	for name := range want {
		if !seen[name] {
			return false
		}
	}
	return true
}

// TestTheRetiredOneSourceScanSeesAKnownYesAndAKnownNo proves the scan on cases
// whose answer is known, both ways: a path row only fires on a path that exists,
// and the Limits scan fires on the copy's two forms (a type, a package-level
// var) but not on a use of sexp.Limits (another name, a function, a partial
// type).
func TestTheRetiredOneSourceScanSeesAKnownYesAndAKnownNo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "roadmapdoc"), 0o755))
	rows, faults := parseRetiredPaths("internal/roadmapdoc\tnova-tools pkg/roadmap\ninternal/worklang\tnova-tools pkg/sexp\n\n# a comment\n")
	require.Empty(t, faults)
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"internal/roadmapdoc is retired; use nova-tools pkg/roadmap"},
		retiredPathProblems(dir, rows), "the path that exists is red, the one that does not is not")
	_, faults = parseRetiredPaths("internal/roadmapdoc nova-tools pkg/roadmap\ninternal/worklang\t\n")
	assert.Len(t, faults, 2, "a row with no tab, and a row with no replacement, are faults")

	assert.Equal(t, []string{"x.go:2: type Limits with MaxBytes, MaxDepth and MaxNodes is defined here; use " + novaToolsModule + "/pkg/sexp"},
		limitsDeclarations("x.go", []byte("package x\ntype Limits struct {\n\tMaxBytes int\n\tMaxDepth int\n\tMaxNodes int\n}\n")),
		"the copy as a type")
	assert.Len(t, limitsDeclarations("y.go", []byte("package y\nvar Limits = worklang.Limits{MaxBytes: 10 << 20, MaxDepth: 8, MaxNodes: 10 << 20}\n")), 1,
		"the copy as a package-level var")
	assert.Empty(t, limitsDeclarations("z.go", []byte(
		"package z\nvar landDataLimits = sexp.Limits{MaxBytes: 16 << 20, MaxNodes: 16 << 20, MaxDepth: roadmap.Limits.MaxDepth}\n"+
			"func Limits(maxBytes int) sexp.Limits { return sexp.Limits{MaxBytes: maxBytes, MaxDepth: 16, MaxNodes: maxBytes} }\n"+
			"type Limits struct { MaxBytes int }\n")),
		"a literal under another name, a function named Limits and a partial type are uses, not copies")
}
