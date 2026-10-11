package ci

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
	"github.com/stretchr/testify/require"
)

// coverageHeader is the header of tla/COVERAGE.tsv.
var coverageHeader = []string{"machine", "code", "module", "status", "owed_by", "note"}

// owedByRE is the shape a non-current row's owed_by must match: a lower-case
// card name.
var owedByRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._~-]*$`)

// coverageRow is one data row of tla/COVERAGE.tsv.
type coverageRow struct {
	machine string
	code    string
	module  string
	status  string
	owedBy  string
	note    string
}

// readCoverageRows parses a COVERAGE.tsv file into its data rows.
func readCoverageRows(t *testing.T, path string) []coverageRow {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	cr := csv.NewReader(f)
	cr.Comma = '\t'
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	all, err := cr.ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, all, "%s is empty", path)
	require.Equal(t, coverageHeader, all[0], "%s has a different header", path)
	var out []coverageRow
	for n, r := range all[1:] {
		require.Len(t, r, len(coverageHeader), "%s row %d has %d fields, want %d", path, n+2, len(r), len(coverageHeader))
		out = append(out, coverageRow{machine: r[0], code: r[1], module: r[2], status: r[3], owedBy: r[4], note: r[5]})
	}
	return out
}

// coverageProblems returns every problem checks (1) and (2) find in rows
// against root. It reads only the tree under root, never the repository, so
// the fixtures call it with a temp root.
func coverageProblems(root string, rows []coverageRow) []string {
	var out []string
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.machine]++
	}
	for m, n := range seen {
		if n > 1 {
			out = append(out, fmt.Sprintf("machine %q is listed %d times", m, n))
		}
	}
	for _, r := range rows {
		out = append(out, r.problems(root)...)
	}
	out = append(out, moduleNamingProblems(root, rows)...)
	return out
}

// problems returns the check (1) problems of a single row.
func (r coverageRow) problems(root string) []string {
	var out []string
	if r.machine == "" {
		out = append(out, "a row has an empty machine")
	}
	if r.note == "" || r.note == "-" {
		out = append(out, fmt.Sprintf("machine %q: note is empty or \"-\"", r.machine))
	}
	if r.status != "current" && r.status != "partial" && r.status != "stale" && r.status != "missing" {
		out = append(out, fmt.Sprintf("machine %q: status %q is not one of current, partial, stale, missing", r.machine, r.status))
	}
	if r.code != "-" {
		for _, f := range strings.Split(r.code, ",") {
			if !isRegularFile(filepath.Join(root, f)) {
				out = append(out, fmt.Sprintf("machine %q: code file %q is not a regular file", r.machine, f))
			}
		}
	}
	switch {
	case r.status == "missing" && r.module != "-":
		out = append(out, fmt.Sprintf("machine %q: a missing row names module %q", r.machine, r.module))
	case r.status != "missing" && r.module == "-":
		out = append(out, fmt.Sprintf("machine %q: a %s row names no module", r.machine, r.status))
	}
	if r.module != "-" {
		for _, m := range strings.Split(r.module, ",") {
			if !isRegularFile(filepath.Join(root, "tla", m+".tla")) {
				out = append(out, fmt.Sprintf("machine %q: module %q has no tla/%s.tla", r.machine, m, m))
			}
		}
	}
	if r.status == "current" && r.owedBy != "-" {
		out = append(out, fmt.Sprintf("machine %q: a current row's owed_by is %q, not \"-\"", r.machine, r.owedBy))
	}
	if r.status != "current" && !owedByRE.MatchString(r.owedBy) {
		out = append(out, fmt.Sprintf("machine %q: a %s row's owed_by %q does not match ^[a-z0-9][a-z0-9._~-]*$", r.machine, r.status, r.owedBy))
	}
	return out
}

// moduleNamingProblems returns the check (2) problems: every tla/<X>.tla whose
// name does not start with MC must be named by exactly one row's module column.
func moduleNamingProblems(root string, rows []coverageRow) []string {
	paths, _ := filepath.Glob(filepath.Join(root, "tla", "*.tla"))
	namedBy := map[string][]string{}
	for _, r := range rows {
		if r.module == "-" {
			continue
		}
		for _, m := range strings.Split(r.module, ",") {
			namedBy[m] = append(namedBy[m], r.machine)
		}
	}
	var out []string
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".tla")
		if strings.HasPrefix(name, "MC") {
			continue
		}
		n := namedBy[name]
		switch len(n) {
		case 0:
			out = append(out, fmt.Sprintf("tla/%s.tla is named by no row of tla/COVERAGE.tsv; add one (status partial, stale or missing names the card that owes it)", name))
		case 1:
		default:
			out = append(out, fmt.Sprintf("tla/%s.tla is named by %d rows: %s", name, len(n), strings.Join(n, ", ")))
		}
	}
	return out
}

// isRegularFile reports whether path is a regular file.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// writeCoverage writes rows as a tla/COVERAGE.tsv under root.
func writeCoverage(t *testing.T, root string, rows []coverageRow) {
	t.Helper()
	dir := filepath.Join(root, "tla")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var b strings.Builder
	b.WriteString(strings.Join(coverageHeader, "\t") + "\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{r.machine, r.code, r.module, r.status, r.owedBy, r.note}, "\t") + "\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "COVERAGE.tsv"), []byte(b.String()), 0o644))
}

// writeTLA writes an empty tla/<name>.tla module under root.
func writeTLA(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, "tla")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".tla"), []byte("---- MODULE "+name+" ----\n====\n"), 0o644))
}

// TestCoverageRefusesAModuleWithNoRow pins check (2): a tla/<X>.tla named by no
// row fails naming X.
func TestCoverageRefusesAModuleWithNoRow(t *testing.T) {
	root := t.TempDir()
	writeTLA(t, root, "A")
	writeTLA(t, root, "B")
	rows := []coverageRow{{machine: "m1", code: "-", module: "A", status: "partial", owedBy: "tla-a", note: "a note"}}
	writeCoverage(t, root, rows)

	problems := coverageProblems(root, rows)
	require.Contains(t, problems, "tla/B.tla is named by no row of tla/COVERAGE.tsv; add one (status partial, stale or missing names the card that owes it)")
}

// TestCoverageRefusesAModuleNamedTwice pins check (2): a tla/<X>.tla named by
// two rows fails naming both.
func TestCoverageRefusesAModuleNamedTwice(t *testing.T) {
	root := t.TempDir()
	writeTLA(t, root, "A")
	rows := []coverageRow{
		{machine: "m1", code: "-", module: "A", status: "partial", owedBy: "tla-a1", note: "a note"},
		{machine: "m2", code: "-", module: "A", status: "partial", owedBy: "tla-a2", note: "a note"},
	}
	writeCoverage(t, root, rows)

	problems := coverageProblems(root, rows)
	require.Len(t, problems, 1)
	require.Contains(t, problems[0], "tla/A.tla is named by")
	require.Contains(t, problems[0], "m1")
	require.Contains(t, problems[0], "m2")
}

// TestCoverageRefusesABadRow pins check (1): the eight shapes
// nova-tools' TestTLACoverageRefusesAnOwedRowWithNoCard lists each fail.
func TestCoverageRefusesABadRow(t *testing.T) {
	cases := []struct {
		name     string
		row      coverageRow
		tlaFiles []string
		want     string
	}{
		{
			name: "missing with owed_by dash",
			row:  coverageRow{machine: "m1", code: "-", module: "-", status: "missing", owedBy: "-", note: "a machine with no module"},
			want: `does not match`,
		},
		{
			name: "missing with empty owed_by",
			row:  coverageRow{machine: "m1", code: "-", module: "-", status: "missing", owedBy: "", note: "a machine with no module"},
			want: `does not match`,
		},
		{
			name:     "stale with wrong owed_by",
			row:      coverageRow{machine: "m1", code: "-", module: "A", status: "stale", owedBy: "WrongCard", note: "n"},
			tlaFiles: []string{"A"},
			want:     `does not match`,
		},
		{
			name:     "current with wrong owed_by",
			row:      coverageRow{machine: "m1", code: "-", module: "A", status: "current", owedBy: "somecard", note: "n"},
			tlaFiles: []string{"A"},
			want:     `not "-"`,
		},
		{
			name: "current with no module",
			row:  coverageRow{machine: "m1", code: "-", module: "-", status: "current", owedBy: "-", note: "n"},
			want: `names no module`,
		},
		{
			name: "code file that does not exist",
			row:  coverageRow{machine: "m1", code: "internal/nope.go", module: "-", status: "missing", owedBy: "tla-x", note: "n"},
			want: `not a regular file`,
		},
		{
			name: "module with no tla",
			row:  coverageRow{machine: "m1", code: "-", module: "Ghost", status: "partial", owedBy: "tla-x", note: "n"},
			want: `has no tla/Ghost.tla`,
		},
		{
			name:     "status done",
			row:      coverageRow{machine: "m1", code: "-", module: "A", status: "done", owedBy: "tla-x", note: "n"},
			tlaFiles: []string{"A"},
			want:     `is not one of current, partial, stale, missing`,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range c.tlaFiles {
				writeTLA(t, root, name)
			}
			writeCoverage(t, root, []coverageRow{c.row})

			problems := coverageProblems(root, []coverageRow{c.row})
			require.Len(t, problems, 1, "expected exactly one problem, got %v", problems)
			require.Contains(t, problems[0], c.want)
		})
	}
}

// TestEveryStateMachineHasACurrentModel reads the real tla/COVERAGE.tsv and
// runs checks (1)-(3).
func TestEveryStateMachineHasACurrentModel(t *testing.T) {
	root := "../.."
	rows := readCoverageRows(t, filepath.Join(root, "tla", "COVERAGE.tsv"))
	for _, p := range coverageProblems(root, rows) {
		t.Error(p)
	}
	checkCurrentModels(t, root, rows)
}

// checkCurrentModels runs check (3): for each row a subtest that skips a
// non-current row naming its owed_by, and for a current row requires each named
// module to hold a reversed witness whose cases are all measured PASS on the
// inputs the case reads now.
func checkCurrentModels(t *testing.T, root string, rows []coverageRow) {
	var hasCurrent bool
	for _, r := range rows {
		if r.status == "current" {
			hasCurrent = true
		}
	}
	if !hasCurrent {
		return
	}
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
	for _, r := range rows {
		r := r
		t.Run(r.machine, func(t *testing.T) {
			if r.status != "current" {
				t.Skipf("owed by %s", r.owedBy)
				return
			}
			for _, m := range strings.Split(r.module, ",") {
				moduleFile := "MC" + m + ".tla"
				var moduleCases []tlc.Case
				for _, c := range cases {
					if c.Module == moduleFile {
						moduleCases = append(moduleCases, c)
					}
				}
				require.NotEmpty(t, moduleCases, "module %s of machine %s has no case in tla/CASES.tsv", m, r.machine)
				var hasWitness bool
				for _, c := range moduleCases {
					if c.Expected != "pass" {
						hasWitness = true
					}
				}
				require.True(t, hasWitness, "module %s of machine %s has no case whose expected is not pass (a reversed witness)", m, r.machine)
				for _, c := range moduleCases {
					rec, ok := byConfig[c.Config]
					if !ok {
						require.NotEqual(t, "-", c.Debt, "case %s of module %s has no tla/RUNS.tsv record and no debt", c.Config, m)
						continue
					}
					require.Equal(t, "PASS", rec.Result, "case %s of module %s has result %s, not PASS", c.Config, m, rec.Result)
					fp, _, err := src.Fingerprint(c.Config)
					require.NoError(t, err, "fingerprinting case %s: %v", c.Config, err)
					require.Equal(t, fp, rec.InputSHA256, "case %s of module %s was measured on other inputs (recorded %s, now %s)", c.Config, m, rec.InputSHA256, fp)
				}
			}
		})
	}
}
