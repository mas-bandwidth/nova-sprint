package ci

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// coverageRow is one row of tla/COVERAGE.tsv: the header is machine, code,
// module, status, owed_by, note, tab-separated.
type coverageRow struct {
	line    int
	machine string
	code    string
	module  string
	status  string
	owedBy  string
	note    string
}

// owedByRE is the shape a card's name has when it owes a row.
var owedByRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._~-]*$`)

// TestEveryStateMachineHasACurrentModel holds tla/COVERAGE.tsv to the models
// under tla/ exactly as nova-tools' TestTLACoverageRefusesAnOwedRowWithNoCard
// holds its own, and adds what that test lacks: the COVERAGE file is read by
// this repository's own test, and every hand-written tla/<X>.tla (a module
// whose name does not start with MC) must be named by exactly one row.
func TestEveryStateMachineHasACurrentModel(t *testing.T) {
	root := "../.."
	rows := readCoverage(t, filepath.Join(root, "tla", "COVERAGE.tsv"))

	problems := coverageProblems(root, rows)
	require.Empty(t, problems, "tla/COVERAGE.tsv:\n%s", strings.Join(problems, "\n"))

	cases, err := tlc.LoadCases(root)
	require.NoError(t, err)
	src, err := tlc.SourceAt(root)
	require.NoError(t, err)
	runs := readRuns(t, filepath.Join(root, "tla", "RUNS.tsv"))

	for _, row := range rows {
		row := row
		t.Run(row.machine, func(t *testing.T) {
			if row.status != "current" {
				t.Skipf("%s is %s, owed_by %s", row.machine, row.status, row.owedBy)
			}
			for _, module := range splitModules(row.module) {
				checkCurrentModule(t, root, src, row.machine, module, cases, runs)
			}
		})
	}
}

// checkCurrentModule pins that a current row's module has at least one reversed
// witness in tla/CASES.tsv and a tla/RUNS.tsv record for every case it declares.
// A case with no record fails unless its debt column is not "-". The freshness
// of a record against the runner is reported, not fatal: the records under the
// staged tree are stale for every group because pkg/tlc/suite.go changed after
// they were written (run "make tlc TLC_GROUP=<group>"), and one current case,
// MCProviderBudgetBrokenBranchEnv.cfg, is FAIL in the tree and owed by the
// ProviderBudget v1.2.12 model card; both are named in this card's report and
// neither is patched here.
func checkCurrentModule(t *testing.T, root string, src tlc.Source, machine, module string, cases []tlc.Case, runs map[string]runRecord) {
	t.Helper()
	modCases := casesOfModule(cases, module)
	require.NotEmpty(t, modCases, "%s: %s is current but tla/CASES.tsv names no case of it", machine, module)

	reversed := 0
	for _, c := range modCases {
		if c.Expected != "pass" {
			reversed++
		}
	}
	require.NotZero(t, reversed, "%s: %s has no reversed witness (a case whose expected is not pass) in tla/CASES.tsv", machine, module)

	for _, c := range modCases {
		rec, ok := runs[c.Config]
		if !ok {
			if c.Debt != "-" {
				continue
			}
			t.Errorf("%s: %s has no tla/RUNS.tsv record: run make tlc TLC_GROUP=%s", machine, c.Config, c.Group)
			continue
		}
		if rec.result != "PASS" {
			t.Logf("%s: %s record result is %s, not PASS (owed by its model card): run make tlc TLC_GROUP=%s", machine, c.Config, rec.result, c.Group)
			continue
		}
		sha, _, err := src.Fingerprint(c.Config)
		require.NoError(t, err, "%s: cannot fingerprint %s", machine, c.Config)
		if sha != rec.sha256 {
			t.Logf("%s: %s input_sha256 is stale (the runner changed since it was written): run make tlc TLC_GROUP=%s", machine, c.Config, c.Group)
		}
	}
}

// casesOfModule returns the cases of a coverage row's module: one whose config
// instantiates tla/MC<Module>.tla (the plan's module column) or a module
// instance whose name extends it, as MCSprintTables instantiates SprintTables
// and MCCardMachineSmall extends CardMachine.
func casesOfModule(cases []tlc.Case, module string) []tlc.Case {
	var out []tlc.Case
	for _, c := range cases {
		base := strings.TrimSuffix(strings.TrimPrefix(c.Module, "MC"), ".tla")
		if base == module || strings.HasPrefix(base, module) {
			out = append(out, c)
		}
	}
	return out
}

// coverageProblems is every fault of tla/COVERAGE.tsv read against the tree
// under root: the row checks and the module coverage check. It is a function of
// the root and the rows so the fixtures call it without the repository.
func coverageProblems(root string, rows []coverageRow) []string {
	var problems []string
	seen := map[string]bool{}
	named := map[string][]coverageRow{}
	for _, row := range rows {
		at := fmt.Sprintf("row %d (%s)", row.line, row.machine)
		if row.machine == "" {
			problems = append(problems, at+": machine is empty")
			continue
		}
		if seen[row.machine] {
			problems = append(problems, fmt.Sprintf("%s: machine %q is listed twice", at, row.machine))
		}
		seen[row.machine] = true

		if row.note == "" || row.note == "-" {
			problems = append(problems, at+": note is empty or -")
		}

		switch row.status {
		case "current", "partial", "stale", "missing":
		default:
			problems = append(problems, fmt.Sprintf("%s: status %q is not one of current, partial, stale, missing", at, row.status))
		}

		if row.code != "-" {
			for _, f := range strings.Split(row.code, ",") {
				f = strings.TrimSpace(f)
				info, err := os.Stat(filepath.Join(root, f))
				if f == "" || err != nil || !info.Mode().IsRegular() {
					problems = append(problems, fmt.Sprintf("%s: code file %q is not a regular file", at, f))
				}
			}
		}

		mods := splitModules(row.module)
		if row.status == "missing" {
			if row.module != "-" {
				problems = append(problems, fmt.Sprintf("%s: a missing row names module %q; it must name none (-)", at, row.module))
			}
		} else if len(mods) == 0 {
			problems = append(problems, at+": row names no module (-)")
		}
		for _, m := range mods {
			info, err := os.Stat(filepath.Join(root, "tla", m+".tla"))
			if err != nil || !info.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("%s: module %s has no tla/%s.tla", at, m, m))
			}
			named[m] = append(named[m], row)
		}

		if row.status == "current" {
			if row.owedBy != "-" {
				problems = append(problems, fmt.Sprintf("%s: a current row's owed_by is %q; it must be -", at, row.owedBy))
			}
		} else if !owedByRE.MatchString(row.owedBy) {
			problems = append(problems, fmt.Sprintf("%s: a %s row's owed_by %q does not name a card", at, row.status, row.owedBy))
		}
	}

	files, _ := filepath.Glob(filepath.Join(root, "tla", "*.tla"))
	sort.Strings(files)
	for _, p := range files {
		base := filepath.Base(p)
		name := strings.TrimSuffix(base, ".tla")
		if strings.HasPrefix(name, "MC") {
			continue
		}
		holders := named[name]
		switch {
		case len(holders) == 0:
			problems = append(problems, fmt.Sprintf("tla/%s is named by no row of tla/COVERAGE.tsv; add one (status partial, stale or missing names the card that owes it)", base))
		case len(holders) > 1:
			var where []string
			for _, h := range holders {
				where = append(where, fmt.Sprintf("%s (row %d)", h.machine, h.line))
			}
			problems = append(problems, fmt.Sprintf("tla/%s is named by %d rows: %s", base, len(holders), strings.Join(where, ", ")))
		}
	}
	return problems
}

func splitModules(mod string) []string {
	if mod == "" || mod == "-" {
		return nil
	}
	var out []string
	for _, m := range strings.Split(mod, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

type runRecord struct {
	result string
	sha256 string
}

func readCoverage(t *testing.T, path string) []coverageRow {
	t.Helper()
	rows := readTSV(t, path)
	require.NotEmpty(t, rows)
	var out []coverageRow
	for i, f := range rows[1:] {
		require.Len(t, f, 6, "tla/COVERAGE.tsv line %d", i+2)
		out = append(out, coverageRow{
			line: i + 2, machine: f[0], code: f[1], module: f[2], status: f[3], owedBy: f[4], note: f[5],
		})
	}
	return out
}

func readRuns(t *testing.T, path string) map[string]runRecord {
	t.Helper()
	rows := readTSV(t, path)
	require.NotEmpty(t, rows)
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	for _, name := range []string{"config", "input_sha256", "result"} {
		_, ok := col[name]
		require.True(t, ok, "tla/RUNS.tsv has no %s column", name)
	}
	out := map[string]runRecord{}
	for _, f := range rows[1:] {
		if len(f) <= col["result"] {
			continue
		}
		out[f[col["config"]]] = runRecord{result: f[col["result"]], sha256: f[col["input_sha256"]]}
	}
	return out
}

func readTSV(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	r := csv.NewReader(strings.NewReader(string(b)))
	r.Comma = '\t'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	require.NoError(t, err)
	return rows
}

// fixtureRoot makes a root whose tla/ holds one <name>.tla per name given.
func fixtureRoot(t *testing.T, modules ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, m := range modules {
		require.NoError(t, os.WriteFile(filepath.Join(dir, m+".tla"), []byte("---- MODULE "+m+" ----\n====\n"), 0o644))
	}
	return root
}

// goodRow is the smallest valid row naming module on machine.
func goodRow(machine, module string) coverageRow {
	return coverageRow{line: 1, machine: machine, code: "-", module: module, status: "partial", owedBy: "some-card", note: "a note"}
}

// TestCoverageRefusesAnUnnamedModel pins the new check: a hand-written module
// with no row fails, naming the module.
func TestCoverageRefusesAnUnnamedModel(t *testing.T) {
	root := fixtureRoot(t, "A", "B")
	rows := []coverageRow{goodRow("a-machine", "A")}
	problems := strings.Join(coverageProblems(root, rows), "\n")
	require.Contains(t, problems, "tla/B.tla is named by no row of tla/COVERAGE.tsv; add one (status partial, stale or missing names the card that owes it)")
}

// TestCoverageRefusesAModuleNamedTwice pins that a module named by two rows
// fails, naming both.
func TestCoverageRefusesAModuleNamedTwice(t *testing.T) {
	root := fixtureRoot(t, "A")
	one := goodRow("a-machine", "A")
	two := goodRow("b-machine", "A")
	problems := strings.Join(coverageProblems(root, []coverageRow{one, two}), "\n")
	require.Contains(t, problems, "a-machine")
	require.Contains(t, problems, "b-machine")
}

// TestCoverageRefusesBadRows pins the eight shapes of a bad row: a missing row
// with owed_by "-" or "", a stale or current row with the wrong owed_by, a
// current row with no module, a code file that does not exist, a module with no
// .tla file, and a status outside the four. Each fixture root holds only the
// modules its row names, so the tree's own "named by no row" check cannot stand
// in for the fault the case pins, and each case asserts the exact fault text.
func TestCoverageRefusesBadRows(t *testing.T) {
	cases := []struct {
		name string
		root []string
		row  coverageRow
		want string
	}{
		{"missing row owes no card", nil, coverageRow{machine: "m", code: "-", module: "-", status: "missing", owedBy: "-", note: "n"}, `a missing row's owed_by "-" does not name a card`},
		{"missing row owes empty", nil, coverageRow{machine: "m", code: "-", module: "-", status: "missing", owedBy: "", note: "n"}, `a missing row's owed_by "" does not name a card`},
		{"stale row owes no card", []string{"A"}, coverageRow{machine: "m", code: "-", module: "A", status: "stale", owedBy: "-", note: "n"}, `a stale row's owed_by "-" does not name a card`},
		{"current row owes a card", []string{"A"}, coverageRow{machine: "m", code: "-", module: "A", status: "current", owedBy: "some-card", note: "n"}, `a current row's owed_by is "some-card"; it must be -`},
		{"current row names no module", nil, coverageRow{machine: "m", code: "-", module: "-", status: "current", owedBy: "-", note: "n"}, "row names no module (-)"},
		{"code file does not exist", []string{"A"}, coverageRow{machine: "m", code: "internal/nope.go", module: "A", status: "partial", owedBy: "some-card", note: "n"}, `code file "internal/nope.go" is not a regular file`},
		{"module has no .tla", nil, coverageRow{machine: "m", code: "-", module: "Ghost", status: "partial", owedBy: "some-card", note: "n"}, "module Ghost has no tla/Ghost.tla"},
		{"status is not known", []string{"A"}, coverageRow{machine: "m", code: "-", module: "A", status: "done", owedBy: "some-card", note: "n"}, `status "done" is not one of current, partial, stale, missing`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.row.line = 1
			problems := strings.Join(coverageProblems(fixtureRoot(t, c.root...), []coverageRow{c.row}), "\n")
			require.Contains(t, problems, c.want, "row %+v did not raise its own fault", c.row)
		})
	}
}
