package ci

import (
	"encoding/csv"
	"errors"
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

// coverageRow is one row of tla/COVERAGE.tsv: the machine a model claims, the
// code files it reads, the modules that model it, its status, the card that
// owes a non-current row, and a note.
type coverageRow struct {
	machine string
	code    string
	module  string
	status  string
	owedBy  string
	note    string
}

var coverageHeader = []string{"machine", "code", "module", "status", "owed_by", "note"}

var owedByRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._~-]*$`)

// parseCoverage reads the rows of tla/COVERAGE.tsv.
func parseCoverage(text string) ([]coverageRow, error) {
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = '\t'
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("tla/COVERAGE.tsv is not tab-separated text: %v", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("tla/COVERAGE.tsv is empty")
	}
	if strings.Join(rows[0], "\t") != strings.Join(coverageHeader, "\t") {
		return nil, fmt.Errorf("tla/COVERAGE.tsv header is %q, want %q", rows[0], coverageHeader)
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	var out []coverageRow
	for n, row := range rows[1:] {
		if len(row) != len(rows[0]) {
			return nil, fmt.Errorf("tla/COVERAGE.tsv row %d has %d fields, want %d", n+2, len(row), len(rows[0]))
		}
		out = append(out, coverageRow{
			machine: row[col["machine"]],
			code:    row[col["code"]],
			module:  row[col["module"]],
			status:  row[col["status"]],
			owedBy:  row[col["owed_by"]],
			note:    row[col["note"]],
		})
	}
	return out, nil
}

// checkCoverage holds the rows of tla/COVERAGE.tsv to the tree under root: it
// refuses a bad row and a tla/<X>.tla module that no row (or two rows) names.
// It is split out so the fixtures call it with a temporary root and their own
// rows, without the repository.
func checkCoverage(root string, rows []coverageRow) error {
	seen := map[string]bool{}
	for _, r := range rows {
		if r.machine == "" {
			return errors.New("tla/COVERAGE.tsv has a row with an empty machine")
		}
		if seen[r.machine] {
			return fmt.Errorf("tla/COVERAGE.tsv lists machine %q twice", r.machine)
		}
		seen[r.machine] = true
	}
	for _, r := range rows {
		if r.note == "" || r.note == "-" {
			return fmt.Errorf("tla/COVERAGE.tsv row %q has note %q, not an explanation", r.machine, r.note)
		}
		switch r.status {
		case "current", "partial", "stale", "missing":
		default:
			return fmt.Errorf("tla/COVERAGE.tsv row %q has status %q, not current, partial, stale or missing", r.machine, r.status)
		}
		if r.code != "-" {
			for _, f := range strings.Split(r.code, ",") {
				info, err := os.Stat(filepath.Join(root, f))
				if err != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("tla/COVERAGE.tsv row %q names code file %q, which is not a regular file", r.machine, f)
				}
			}
		}
		switch {
		case r.status == "missing" && r.module != "-":
			return fmt.Errorf("tla/COVERAGE.tsv row %q is missing but names module %q", r.machine, r.module)
		case r.status != "missing" && r.module == "-":
			return fmt.Errorf("tla/COVERAGE.tsv row %q names no module", r.machine)
		}
		for _, m := range strings.Split(r.module, ",") {
			if m == "-" {
				continue
			}
			info, err := os.Stat(filepath.Join(root, "tla", m+".tla"))
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("tla/COVERAGE.tsv row %q names module %q with no tla/%s.tla", r.machine, m, m)
			}
		}
		if r.status == "current" {
			if r.owedBy != "-" {
				return fmt.Errorf("tla/COVERAGE.tsv row %q is current but owes %q", r.machine, r.owedBy)
			}
		} else if !owedByRE.MatchString(r.owedBy) {
			return fmt.Errorf("tla/COVERAGE.tsv row %q has owed_by %q, not a card name", r.machine, r.owedBy)
		}
	}

	namedBy := map[string][]string{}
	for _, r := range rows {
		for _, m := range strings.Split(r.module, ",") {
			if m == "-" {
				continue
			}
			namedBy[m] = append(namedBy[m], r.machine)
		}
	}
	models, err := filepath.Glob(filepath.Join(root, "tla", "*.tla"))
	if err != nil {
		return err
	}
	sort.Strings(models)
	for _, p := range models {
		name := filepath.Base(p)
		if strings.HasPrefix(name, "MC") {
			continue
		}
		module := strings.TrimSuffix(name, ".tla")
		machines := namedBy[module]
		switch {
		case len(machines) == 0:
			return fmt.Errorf("tla/%s is named by no row of tla/COVERAGE.tsv; add one (status partial, stale or missing names the card that owes it)", name)
		case len(machines) > 1:
			sort.Strings(machines)
			return fmt.Errorf("tla/%s is named by two rows of tla/COVERAGE.tsv (%s)", name, strings.Join(machines, ", "))
		}
	}
	return nil
}

// TestEveryStateMachineHasACurrentModel holds tla/COVERAGE.tsv to the tree: a
// non-current row names the card that owes it and is skipped; a current row
// must have a reversed witness and a passing, current TLC record for every one
// of its cases.
func TestEveryStateMachineHasACurrentModel(t *testing.T) {
	root := "../.."
	data, err := os.ReadFile(filepath.Join(root, "tla", "COVERAGE.tsv"))
	require.NoError(t, err)
	rows, err := parseCoverage(string(data))
	require.NoError(t, err)
	require.NoError(t, checkCoverage(root, rows))

	for _, r := range rows {
		r := r
		t.Run(r.machine, func(t *testing.T) {
			if r.status != "current" {
				t.Skipf("row owes %s", r.owedBy)
				return
			}
			checkCurrentModel(t, root, r)
		})
	}
}

// checkCurrentModel verifies a current row's modules against tla/CASES.tsv and
// tla/RUNS.tsv: each module has a reversed witness (a case whose expected is
// not pass), and every case of the module has a record with result PASS whose
// input_sha256 is the fingerprint the case has now.
func checkCurrentModel(t *testing.T, root string, r coverageRow) {
	t.Helper()
	src, err := tlc.SourceAt(root)
	require.NoError(t, err)
	cases, err := tlc.LoadCases(root)
	require.NoError(t, err)
	records, err := tlc.ReadRecordsFile(filepath.Join(root, "tla", tlc.RunsFile))
	require.NoError(t, err)
	byConfig := map[string]tlc.Record{}
	for _, rec := range records {
		byConfig[rec.Config] = rec
	}
	byModule := map[string][]tlc.Case{}
	for _, c := range cases {
		byModule[c.Module] = append(byModule[c.Module], c)
	}
	for _, module := range strings.Split(r.module, ",") {
		modFile := "MC" + module + ".tla"
		modCases := byModule[modFile]
		if len(modCases) == 0 {
			t.Fatalf("module %s has no case in tla/CASES.tsv", module)
		}
		witness := false
		for _, c := range modCases {
			if c.Expected != "pass" {
				witness = true
			}
		}
		if !witness {
			t.Fatalf("module %s has no reversed witness (a case whose expected is not pass)", module)
		}
		for _, c := range modCases {
			rec, ok := byConfig[c.Config]
			if !ok {
				if c.Debt == "-" {
					t.Fatalf("module %s case %s has no tla/RUNS.tsv record and no debt", module, c.Config)
				}
				continue
			}
			if rec.Result != "PASS" {
				t.Fatalf("module %s case %s record has result %s, not PASS", module, c.Config, rec.Result)
			}
			fp, _, err := src.Fingerprint(c.Config)
			require.NoError(t, err)
			if rec.InputSHA256 != fp {
				t.Fatalf("module %s case %s record is stale (input_sha256 %s, now %s)", module, c.Config, rec.InputSHA256, fp)
			}
		}
	}
}

func writeModel(t *testing.T, root, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tla"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tla", name), []byte("---- MODULE "+strings.TrimSuffix(name, ".tla")+" ----\n====\n"), 0o644))
}

// TestCoverageNamesEveryModule pins check (2): every tla/<X>.tla whose name
// does not start with MC is named by exactly one row's module column.
func TestCoverageNamesEveryModule(t *testing.T) {
	t.Run("a module named by no row", func(t *testing.T) {
		root := t.TempDir()
		writeModel(t, root, "A.tla")
		writeModel(t, root, "B.tla")
		rows := []coverageRow{{machine: "a", code: "-", module: "A", status: "partial", owedBy: "card-a", note: "x"}}
		err := checkCoverage(root, rows)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tla/B.tla is named by no row of tla/COVERAGE.tsv")
	})

	t.Run("a module named by two rows", func(t *testing.T) {
		root := t.TempDir()
		writeModel(t, root, "A.tla")
		rows := []coverageRow{
			{machine: "a", code: "-", module: "A", status: "partial", owedBy: "card-a", note: "x"},
			{machine: "b", code: "-", module: "A", status: "partial", owedBy: "card-b", note: "y"},
		}
		err := checkCoverage(root, rows)
		require.Error(t, err)
		require.Contains(t, err.Error(), "named by two rows")
		require.Contains(t, err.Error(), "a")
		require.Contains(t, err.Error(), "b")
	})
}

// TestCoverageRefusesABadRow pins check (1): the eight bad-row shapes a
// COVERAGE.tsv must refuse, each naming the row.
func TestCoverageRefusesABadRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		row     coverageRow
		models  []string
		wantErr string
	}{
		{
			name:    "missing with owed_by -",
			row:     coverageRow{machine: "m1", code: "-", module: "-", status: "missing", owedBy: "-", note: "x"},
			wantErr: `row "m1" has owed_by "-", not a card name`,
		},
		{
			name:    "missing with owed_by empty",
			row:     coverageRow{machine: "m2", code: "-", module: "-", status: "missing", owedBy: "", note: "x"},
			wantErr: `row "m2" has owed_by "", not a card name`,
		},
		{
			name:    "stale with the wrong owed_by",
			row:     coverageRow{machine: "m3", code: "-", module: "A", status: "stale", owedBy: "-", note: "x"},
			models:  []string{"A.tla"},
			wantErr: `row "m3" has owed_by "-", not a card name`,
		},
		{
			name:    "current with the wrong owed_by",
			row:     coverageRow{machine: "m4", code: "-", module: "A", status: "current", owedBy: "card-x", note: "x"},
			models:  []string{"A.tla"},
			wantErr: `row "m4" is current but owes "card-x"`,
		},
		{
			name:    "current with no module",
			row:     coverageRow{machine: "m5", code: "-", module: "-", status: "current", owedBy: "-", note: "x"},
			wantErr: `row "m5" names no module`,
		},
		{
			name:    "a code file that does not exist",
			row:     coverageRow{machine: "m6", code: "missing.go", module: "A", status: "partial", owedBy: "card-m6", note: "x"},
			models:  []string{"A.tla"},
			wantErr: `row "m6" names code file "missing.go", which is not a regular file`,
		},
		{
			name:    "a module with no .tla",
			row:     coverageRow{machine: "m7", code: "-", module: "Ghost", status: "partial", owedBy: "card-m7", note: "x"},
			wantErr: `row "m7" names module "Ghost" with no tla/Ghost.tla`,
		},
		{
			name:    "status done",
			row:     coverageRow{machine: "m8", code: "-", module: "A", status: "done", owedBy: "card-m8", note: "x"},
			models:  []string{"A.tla"},
			wantErr: `row "m8" has status "done", not current, partial, stale or missing`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, m := range tc.models {
				writeModel(t, root, m)
			}
			err := checkCoverage(root, []coverageRow{tc.row})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
