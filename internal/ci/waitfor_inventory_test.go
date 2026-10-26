package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var waitForColumns = []string{
	"edge", "waiter", "waits_on", "code", "ends_by",
	"deadline", "judgment", "action", "class", "owed_by",
}

// collectSprintJudgmentKinds parses internal/sprint/*.go (non-test files) with
// go/parser and returns the sorted list of judgment kind constant names (N*)
// from TickDecisions, WaitRefused, WaitAllowedOnACard, WaitRefusedWhenItNamesACard,
// Decisions, AlarmTypes, and every Kind: Judgment Note/Group/cond/Stop/judgment()
// write in the package.
func collectSprintJudgmentKinds(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "internal", "sprint")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	fset := token.NewFileSet()
	kinds := map[string]bool{}
	addIdent := func(expr ast.Expr) {
		if id, ok := expr.(*ast.Ident); ok && strings.HasPrefix(id.Name, "N") && id.Name != "Note" && id.Name != "NeedJudgment" {
			kinds[id.Name] = true
		}
	}

	targetMaps := map[string]bool{
		"TickDecisions":               true,
		"WaitRefused":                 true,
		"WaitAllowedOnACard":          true,
		"WaitRefusedWhenItNamesACard": true,
		"Decisions":                   true,
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoError(t, err)

		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				for i, id := range x.Names {
					if i >= len(x.Values) {
						continue
					}
					if targetMaps[id.Name] {
						if cl, ok := x.Values[i].(*ast.CompositeLit); ok {
							for _, elt := range cl.Elts {
								if kv, ok := elt.(*ast.KeyValueExpr); ok {
									addIdent(kv.Key)
								}
							}
						}
					}
					if id.Name == "AlarmTypes" {
						if cl, ok := x.Values[i].(*ast.CompositeLit); ok {
							for _, elt := range cl.Elts {
								addIdent(elt)
							}
						}
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					if idx, ok := lhs.(*ast.IndexExpr); ok {
						if m, ok := idx.X.(*ast.Ident); ok && targetMaps[m.Name] {
							addIdent(idx.Index)
						}
					}
				}
			case *ast.CallExpr:
				if fn, ok := x.Fun.(*ast.Ident); ok {
					if fn.Name == "judgment" && len(x.Args) >= 1 {
						addIdent(x.Args[0])
					}
					if fn.Name == "stop" && len(x.Args) >= 2 {
						addIdent(x.Args[1])
					}
				}
			case *ast.FuncDecl:
				if x.Name != nil && x.Name.Name == "IsLanderStanding" && x.Body != nil {
					ast.Inspect(x.Body, func(inner ast.Node) bool {
						if be, ok := inner.(*ast.BinaryExpr); ok {
							addIdent(be.X)
							addIdent(be.Y)
						}
						return true
					})
				}
			case *ast.CompositeLit:
				typeName := ""
				switch t := x.Type.(type) {
				case *ast.Ident:
					typeName = t.Name
				case *ast.SelectorExpr:
					typeName = t.Sel.Name
				}
				switch typeName {
				case "Note", "Group":
					isJudgment := false
					var typExpr ast.Expr
					for _, elt := range x.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						k, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if k.Name == "Kind" {
							switch v := kv.Value.(type) {
							case *ast.Ident:
								isJudgment = v.Name == "Judgment"
							case *ast.SelectorExpr:
								isJudgment = v.Sel.Name == "Judgment"
							}
						}
						if k.Name == "Type" {
							typExpr = kv.Value
						}
					}
					if isJudgment && typExpr != nil {
						addIdent(typExpr)
					}
				case "cond", "Stop":
					for _, elt := range x.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "typ" {
								addIdent(kv.Value)
							}
						}
					}
				}
			}
			return true
		})
	}

	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// waitforInventoryProblems checks the parsed TSV rows of tla/WAITFOR.tsv against
// the required columns and the collected judgment kinds.
func waitforInventoryProblems(rows [][]string, requiredKinds []string) []string {
	if len(rows) == 0 {
		return []string{"tla/WAITFOR.tsv is empty"}
	}
	var problems []string
	if !slices.Equal(rows[0], waitForColumns) {
		problems = append(problems, fmt.Sprintf("tla/WAITFOR.tsv header = %v, want %v", rows[0], waitForColumns))
		return problems
	}
	seenJudgments := map[string]bool{}
	seenEdges := map[string]bool{}
	for i, r := range rows[1:] {
		line := i + 2
		if len(r) != len(waitForColumns) {
			problems = append(problems, fmt.Sprintf("tla/WAITFOR.tsv line %d has %d columns, want %d", line, len(r), len(waitForColumns)))
			continue
		}
		for c, val := range r {
			if strings.TrimSpace(val) == "" {
				problems = append(problems, fmt.Sprintf("tla/WAITFOR.tsv line %d column %s is empty", line, waitForColumns[c]))
			}
		}
		edge := r[0]
		if seenEdges[edge] {
			problems = append(problems, fmt.Sprintf("tla/WAITFOR.tsv line %d duplicates edge %q", line, edge))
		}
		seenEdges[edge] = true
		switch r[4] {
		case "progress", "deadline", "coordinator", "-":
		default:
			problems = append(problems, fmt.Sprintf("tla/WAITFOR.tsv line %d ends_by %q is not progress, deadline, coordinator, or -", line, r[4]))
		}
		for _, j := range strings.Split(r[6], ",") {
			j = strings.TrimSpace(j)
			if j != "" && j != "-" {
				seenJudgments[j] = true
			}
		}
	}
	for _, k := range requiredKinds {
		if !seenJudgments[k] {
			problems = append(problems, fmt.Sprintf("judgment kind %s has no row in tla/WAITFOR.tsv", k))
		}
	}
	return problems
}

// TestWaitForInventoryNamesEveryJudgmentKind parses internal/sprint with
// go/parser, collects every judgment kind in TickDecisions, WaitRefused,
// WaitAllowedOnACard and the package, and verifies tla/WAITFOR.tsv covers each
// one, with a reversed-witness subtest that removes a judgment kind row from a
// test-only copy and requires the check to fail naming that kind.
func TestWaitForInventoryNamesEveryJudgmentKind(t *testing.T) {
	t.Parallel()
	root := "../.."
	kinds := collectSprintJudgmentKinds(t, root)
	require.NotEmpty(t, kinds, "no judgment kinds collected from internal/sprint")

	tsvPath := filepath.Join(root, "tla", "WAITFOR.tsv")
	rows := readTSV(t, tsvPath)
	problems := waitforInventoryProblems(rows, kinds)
	require.Empty(t, problems, "tla/WAITFOR.tsv problems:\n%s", strings.Join(problems, "\n"))

	t.Run("reversed_witness_missing_judgment_kind", func(t *testing.T) {
		t.Parallel()
		target := "NCannotAsk"
		require.Contains(t, kinds, target)
		filtered := [][]string{rows[0]}
		for _, r := range rows[1:] {
			if len(r) > 6 && r[6] == target {
				continue
			}
			filtered = append(filtered, r)
		}
		got := strings.Join(waitforInventoryProblems(filtered, kinds), "\n")
		require.Contains(t, got, "judgment kind "+target+" has no row in tla/WAITFOR.tsv")
	})
}
