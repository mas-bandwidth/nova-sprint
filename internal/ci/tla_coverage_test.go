package ci

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
	"github.com/stretchr/testify/require"
)

// COVERAGE.tsv is the map from a state machine in the code to the TLA+ module
// that models it and the card that owes a model when there is none:
// machine, code, module, status, owed_by, note. The test below holds the file
// to the tree: every row is well formed, every module named is a model file,
// every model file is named by exactly one row, and every current row's model
// has a reversed witness whose run record is fresh.
//
// The check on a root and its rows is checkCoverage so the fixtures in this
// file exercise it without the repository.
var coverageHeader = []string{"machine", "code", "module", "status", "owed_by", "note"}

// coverageStatuses are the statuses a row may hold.
var coverageStatuses = map[string]bool{"current": true, "partial": true, "stale": true, "missing": true}

// owedByRE is a card's name: a lowercase token, as a release's entries name one.
var owedByRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._~-]*$`)

// coverageRow is one data row of tla/COVERAGE.tsv.
type coverageRow struct {
	Machine string
	Code    string
	Module  string
	Status  string
	OwedBy  string
	Note    string
}

// readCoverage reads and parses tla/COVERAGE.tsv under path, refusing a header
// that is not the current layout and a row of the wrong width.
func readCoverage(path string) ([]coverageRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // ignored: the map is opened only for reading
	rows, err := parseCoverage(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return rows, nil
}

func parseCoverage(r io.Reader) ([]coverageRow, error) {
	cr := csv.NewReader(r)
	cr.Comma = '\t'
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("COVERAGE.tsv is not tab-separated text: %v", err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("COVERAGE.tsv is empty")
	}
	if strings.Join(recs[0], "\t") != strings.Join(coverageHeader, "\t") {
		return nil, fmt.Errorf("COVERAGE.tsv has header %q, want %q", strings.Join(recs[0], ","), strings.Join(coverageHeader, ","))
	}
	var out []coverageRow
	for n, f := range recs[1:] {
		if len(f) != len(coverageHeader) {
			return nil, fmt.Errorf("COVERAGE.tsv row %d has %d fields, want %d", n+2, len(f), len(coverageHeader))
		}
		out = append(out, coverageRow{Machine: f[0], Code: f[1], Module: f[2], Status: f[3], OwedBy: f[4], Note: f[5]})
	}
	return out, nil
}

// checkCoverage holds tla/COVERAGE.tsv under root to the tree: the shape of
// every row, the files the rows name, and the rule that every model file is
// named by exactly one row. It reads root/tla/<Module>.tla and root/<code>, so
// a fixture calls it with a temporary root and its own rows.
func checkCoverage(root string, rows []coverageRow) error {
	named := map[string][]string{} // module name -> machines whose module column names it
	seen := map[string]bool{}
	for i, row := range rows {
		where := fmt.Sprintf("tla/COVERAGE.tsv row %d", i+2)
		if row.Machine != "" {
			where = fmt.Sprintf("row %q", row.Machine)
		}
		if row.Machine == "" {
			return fmt.Errorf("%s: empty machine", where)
		}
		if seen[row.Machine] {
			return fmt.Errorf("%s: machine listed twice", where)
		}
		seen[row.Machine] = true
		if row.Note == "" || row.Note == "-" {
			return fmt.Errorf("%s: note is empty or -", where)
		}
		if !coverageStatuses[row.Status] {
			return fmt.Errorf("%s: status %q is not one of current, partial, stale, missing", where, row.Status)
		}
		if row.Code != "-" {
			for _, code := range strings.Split(row.Code, ",") {
				if code == "" || code == "-" {
					return fmt.Errorf("%s: code %q names no file", where, row.Code)
				}
				info, err := os.Stat(filepath.Join(root, code))
				if err != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("%s: code file %s is not a regular file", where, code)
				}
			}
		}
		if row.Module == "-" {
			if row.Status != "missing" {
				return fmt.Errorf("%s: a %s row names no module", where, row.Status)
			}
		} else {
			if row.Status == "missing" {
				return fmt.Errorf("%s: a missing row names the module %s", where, row.Module)
			}
			for _, module := range strings.Split(row.Module, ",") {
				if module == "" {
					return fmt.Errorf("%s: module column %q names an empty module", where, row.Module)
				}
				info, err := os.Stat(filepath.Join(root, "tla", module+".tla"))
				if err != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("%s: module %s has no tla/%s.tla", where, module, module)
				}
				named[module] = append(named[module], row.Machine)
			}
		}
		if row.Status == "current" {
			if row.OwedBy != "-" {
				return fmt.Errorf("%s: a current row has owed_by %q, want -", where, row.OwedBy)
			}
		} else if !owedByRE.MatchString(row.OwedBy) {
			return fmt.Errorf("%s: a %s row has owed_by %q, which does not name a card", where, row.Status, row.OwedBy)
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "tla", "*.tla"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".tla")
		if strings.HasPrefix(name, "MC") {
			continue
		}
		switch machines := named[name]; len(machines) {
		case 0:
			return fmt.Errorf("tla/%s.tla is named by no row of tla/COVERAGE.tsv; add one (status partial, stale or missing names the card that owes it)", name)
		case 1:
		default:
			return fmt.Errorf("tla/%s.tla is named by two rows: %s and %s", name, machines[0], machines[1])
		}
	}
	return nil
}

// TestEveryStateMachineHasACurrentModel holds tla/COVERAGE.tsv to the models:
// its rows are well formed (checkCoverage), every model file is named by
// exactly one row (the check the nova-tools half lacked), a row that is not
// current is a skip that names its card, and a current row's module has a
// reversed witness and a fresh passing run record for every case it declares.
func TestEveryStateMachineHasACurrentModel(t *testing.T) {
	root := "../.."
	rows, err := readCoverage(filepath.Join(root, "tla", "COVERAGE.tsv"))
	require.NoError(t, err)
	if err := checkCoverage(root, rows); err != nil {
		t.Errorf("tla/COVERAGE.tsv: %v", err)
	}
	cases, err := tlc.LoadCases(root)
	require.NoError(t, err)
	runs, err := tlc.ReadRecordsFile(filepath.Join(root, "tla", "RUNS.tsv"))
	require.NoError(t, err)
	record := map[string]tlc.Record{}
	for _, r := range runs {
		record[r.Config] = r
	}
	src, err := tlc.SourceAt(root)
	require.NoError(t, err)
	for _, row := range rows {
		t.Run(row.Machine, func(t *testing.T) {
			if row.Status != "current" {
				t.Skipf("owed by %s", row.OwedBy)
			}
			for _, module := range strings.Split(row.Module, ",") {
				checkCurrentModule(t, src, module, cases, record)
			}
		})
	}
}

// checkCurrentModule holds one current row's module to its cases and runs: the
// module has at least one reversed witness (a case whose expected is not pass),
// and every case of it has a run record that passed and whose input_sha256 is
// the fingerprint of the case now. A case with no record is allowed only when
// its debt column names why.
func checkCurrentModule(t *testing.T, src tlc.Source, module string, cases []tlc.Case, runs map[string]tlc.Record) {
	t.Helper()
	var mine []tlc.Case
	for _, c := range cases {
		if c.Module == "MC"+module+".tla" {
			mine = append(mine, c)
		}
	}
	require.NotEmpty(t, mine, "module %s has no case in tla/CASES.tsv", module)
	witness := false
	for _, c := range mine {
		if c.Expected != "pass" {
			witness = true
		}
	}
	require.True(t, witness, "module %s has no reversed witness (a case whose expected is not pass)", module)
	for _, c := range mine {
		rec, ok := runs[c.Config]
		if !ok {
			if c.Debt == "-" {
				t.Errorf("case %s of module %s has no record in tla/RUNS.tsv", c.Config, module)
			}
			continue
		}
		require.Equal(t, "PASS", rec.Result, "case %s did not pass its last run", c.Config)
		fp, _, err := src.Fingerprint(c.Config)
		require.NoError(t, err, c.Config)
		require.Equal(t, rec.InputSHA256, fp, "case %s's record is stale: rerun TLC", c.Config)
	}
}

// fixtureRoot writes a root holding tla/COVERAGE.tsv (data rows only) and the
// named files under tla/.
func fixtureRoot(t *testing.T, data string, models ...string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tla"), 0o755))
	for _, m := range models {
		require.NoError(t, os.WriteFile(filepath.Join(root, "tla", m), []byte("---- MODULE generated ----\n====\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "tla", "COVERAGE.tsv"),
		[]byte(strings.Join(coverageHeader, "\t")+"\n"+data), 0o644))
	return root
}

func fixtureRows(t *testing.T, root string) []coverageRow {
	t.Helper()
	rows, err := readCoverage(filepath.Join(root, "tla", "COVERAGE.tsv"))
	require.NoError(t, err)
	return rows
}

// TestTLACoverageNamesEveryModel pins the check nova-tools' test lacked: every
// tla/<X>.tla that is not an MC instance is named by exactly one row's module
// column. A model named by no row fails naming the file and the remedy; one
// named by two rows fails naming both.
func TestTLACoverageNamesEveryModel(t *testing.T) {
	t.Run("a model named by no row", func(t *testing.T) {
		root := fixtureRoot(t, "a\t-\tA\tpartial\tcard-a\ta note\n", "A.tla", "B.tla")
		err := checkCoverage(root, fixtureRows(t, root))
		require.Error(t, err)
		require.Contains(t, err.Error(), "tla/B.tla is named by no row of tla/COVERAGE.tsv")
		require.Contains(t, err.Error(), "status partial, stale or missing")
	})
	t.Run("a model named by two rows", func(t *testing.T) {
		root := fixtureRoot(t, "a\t-\tA\tpartial\tcard-a\ta note\nb\t-\tA\tpartial\tcard-b\tb note\n", "A.tla")
		err := checkCoverage(root, fixtureRows(t, root))
		require.Error(t, err)
		require.Contains(t, err.Error(), "tla/A.tla is named by two rows")
		require.Contains(t, err.Error(), "a")
		require.Contains(t, err.Error(), "b")
	})
}

// TestTLACoverageRefusesABadRow pins the shape of a row: the eight a row may
// be broken in (the same the nova-tools test lists), plus an empty machine, a
// machine listed twice, an empty note, and a module a missing row names. Each
// fails naming the row.
func TestTLACoverageRefusesABadRow(t *testing.T) {
	tests := []struct {
		name string
		row  string
		want string
	}{
		{"missing with owed_by -", "a\t-\t-\tmissing\t-\tn\n", "does not name a card"},
		{"missing with empty owed_by", "a\t-\t-\tmissing\t\tn\n", "does not name a card"},
		{"stale with owed_by -", "a\t-\tA\tstale\t-\tn\n", "does not name a card"},
		{"current with owed_by set", "a\t-\tA\tcurrent\tcard-a\tn\n", "has owed_by \"card-a\", want -"},
		{"current with no module", "a\t-\t-\tcurrent\t-\tn\n", "names no module"},
		{"a code file that is not there", "a\tinternal/nope.go\tA\tcurrent\t-\tn\n", "is not a regular file"},
		{"a module with no .tla", "a\t-\tZ\tpartial\tcard-z\tn\n", "has no tla/Z.tla"},
		{"status done", "a\t-\t-\tdone\t-\tn\n", "status \"done\""},
		{"empty machine", "\t-\tA\tpartial\tcard-a\tn\n", "empty machine"},
		{"machine listed twice", "a\t-\tA\tpartial\tcard-a\tn\na\t-\tA\tpartial\tcard-a\tn\n", "listed twice"},
		{"an empty note", "a\t-\tA\tpartial\tcard-a\t\n", "note is empty"},
		{"a missing row that names a module", "a\t-\tA\tmissing\tcard-a\tn\n", "a missing row names the module A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := fixtureRoot(t, tt.row, "A.tla")
			err := checkCoverage(root, fixtureRows(t, root))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}
