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

	"github.com/stretchr/testify/require"
)

// oneSourceRetiredTable names the paths that were copies of nova-tools code and are
// deleted: "<path>\t<what replaced it>" a line, '#' lines comments.
const oneSourceRetiredTable = "testdata/one-source-retired.txt"

// oneSourceRetiredRow is one row of the table.
type oneSourceRetiredRow struct {
	Path, Replacement string
}

// parseOneSourceRetired reads the table; a row that is not <path><TAB><replacement>, or a
// path listed twice, is a fault: every retirement names what took its place.
func parseOneSourceRetired(text string) ([]oneSourceRetiredRow, []string) {
	var rows []oneSourceRetiredRow
	var faults []string
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		p, repl, ok := strings.Cut(line, "\t")
		p, repl = strings.TrimSpace(p), strings.TrimSpace(repl)
		if !ok || p == "" || repl == "" {
			faults = append(faults, fmt.Sprintf("line %d: want `<path><TAB><what replaced it>`, got %q", i+1, line))
			continue
		}
		if seen[p] {
			faults = append(faults, fmt.Sprintf("line %d: %s is listed twice", i+1, p))
			continue
		}
		seen[p] = true
		rows = append(rows, oneSourceRetiredRow{Path: p, Replacement: repl})
	}
	return rows, faults
}

// retiredCopiesPresent is a finding for every listed path that exists under root.
func retiredCopiesPresent(root string, rows []oneSourceRetiredRow) []string {
	var out []string
	for _, r := range rows {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(r.Path))); err == nil {
			out = append(out, fmt.Sprintf("%s exists again: it was a copy of nova-tools code; delete it and use %s", r.Path, r.Replacement))
		}
	}
	return out
}

// limitsFields are the fields that make a struct type a copy of nova-tools' sexp.Limits.
var limitsFields = []string{"MaxBytes", "MaxDepth", "MaxNodes"}

// limitsCopies is a finding for every Go type under root named Limits whose struct has
// the fields MaxBytes, MaxDepth and MaxNodes: the bound nova-tools pkg/sexp defines once.
// A value of sexp.Limits is a use, not a copy, and is not a type declaration. .git, tmp,
// bin, node_modules and testdata directories are not part of the module's code.
func limitsCopies(root string) ([]string, error) {
	var out []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "tmp", "bin", "node_modules", "testdata":
				if p != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Limits" {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			has := map[string]bool{}
			for _, fld := range st.Fields.List {
				for _, name := range fld.Names {
					has[name.Name] = true
				}
			}
			for _, want := range limitsFields {
				if !has[want] {
					return true
				}
			}
			rel, _ := filepath.Rel(root, fset.Position(ts.Pos()).Filename)
			out = append(out, fmt.Sprintf("%s:%d: type Limits {MaxBytes, MaxDepth, MaxNodes} is a copy of nova-tools pkg/sexp.Limits; use sexp.Limits",
				filepath.ToSlash(rel), fset.Position(ts.Pos()).Line))
			return true
		})
		return nil
	})
	sort.Strings(out)
	return out, err
}

// TestNoRetiredOneSourceCopyReturns: the roadmap decoder, the s-expression reader and
// the roadmap generator have one source, nova-tools pkg/roadmap, pkg/sexp and
// tools/roadmap (the 10 MiB roadmap fix landed twice, nova-tools #5811 and nova-sprint
// #349, because they had two). Every path internal/ci/testdata/one-source-retired.txt
// lists is red if it exists, and so is any Go type in the module named Limits with the
// fields MaxBytes, MaxDepth and MaxNodes. The scans are first proved on a known yes and
// a known no.
func TestNoRetiredOneSourceCopyReturns(t *testing.T) {
	t.Parallel()

	// known yes and known no, on a tree whose answer is known
	fixture := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(fixture, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write("internal/old/old.go", "package old\n\ntype Limits struct {\n\tMaxBytes, MaxDepth int\n\tMaxNodes int\n}\n")
	write("internal/fine/fine.go", "package fine\n\ntype Limits struct{ MaxBytes, MaxDepth int }\n\ntype Bound struct{ MaxBytes, MaxDepth, MaxNodes int }\n\nvar L = struct{ MaxBytes, MaxDepth, MaxNodes int }{1, 2, 3}\n")
	write("internal/fine/testdata/x.go", "package x\n\ntype Limits struct{ MaxBytes, MaxDepth, MaxNodes int }\n")
	fixRows, faults := parseOneSourceRetired("# c\ninternal/old\tpkg/new\ninternal/gone\tpkg/other\n")
	require.Empty(t, faults)
	require.Equal(t, []string{"internal/old exists again: it was a copy of nova-tools code; delete it and use pkg/new"}, retiredCopiesPresent(fixture, fixRows))
	got, err := limitsCopies(fixture)
	require.NoError(t, err)
	require.Len(t, got, 1, "%v", got)
	require.Contains(t, got[0], "internal/old/old.go:3: type Limits")
	_, faults = parseOneSourceRetired("internal/a\n internal/b\tx\ninternal/b\ty\n")
	require.Len(t, faults, 2, "%v", faults)

	// the module itself
	root := "../.."
	raw, err := os.ReadFile(oneSourceRetiredTable)
	require.NoError(t, err)
	rows, faults := parseOneSourceRetired(string(raw))
	require.Empty(t, faults, "%s", oneSourceRetiredTable)
	require.NotEmpty(t, rows, "%s names nothing", oneSourceRetiredTable)
	found := retiredCopiesPresent(root, rows)
	limits, err := limitsCopies(root)
	require.NoError(t, err)
	found = append(found, limits...)
	require.Empty(t, found, "one true source: nova-sprint carries a copy of nova-tools' roadmap, sexp reader or their Limits:\n%s", strings.Join(found, "\n"))
}
