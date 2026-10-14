package tlc

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/testkit/fsx"
)

// recordsPlan writes a small checkout under a temp dir: two current cases and
// their modules, and a Source over it.
func recordsPlan(t *testing.T) (string, []Case, Source) {
	t.Helper()
	plan := "config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n" +
		"MCTinyA.cfg\tMCTinyA.tla\tpass\t-\tcheck\ttiny\trequired\t-\n" +
		"MCTinyB.cfg\tMCTinyB.tla\tpass\t-\tcheck\ttiny\trequired\t-\n"
	files := map[string]string{"tla/" + CasesFile: plan}
	for _, name := range []string{"A", "B"} {
		files["tla/MCTiny"+name+".cfg"] = "SPECIFICATION Spec\n"
		files["tla/MCTiny"+name+".tla"] = "---- MODULE MCTiny" + name + " ----\nVARIABLE x\nInit == x = 0\nNext == x' = x\n====\n"
	}
	root := fsx.Tree(t, files)
	parsed, err := LoadCases(root)
	require.NoError(t, err)
	src, err := SourceAt(root)
	require.NoError(t, err)
	return root, parsed, src
}

// currentRecord is a current measurement of one case.
func currentRecord(t *testing.T, src Source, c Case) Record {
	t.Helper()
	fp, files, err := src.Fingerprint(c.Config)
	require.NoError(t, err)
	return Record{
		Config: c.Config, Module: c.Module, InputSHA256: fp, InputFiles: files,
		JarSHA256: strings.Repeat("a", 64), JavaVersion: "21.0.1", Host: "linux-amd64", CPUs: 2,
		StartedUTC: "2026-10-11T00:00:00.000001+00:00", Workers: 2,
		Generated: "1", Distinct: "1", Seconds: "0.010", Exit: 0, Result: "PASS",
		Expected: c.Expected, Property: c.Property, Budget: "110", Mode: "bounded",
	}
}

// TestReadRecordsFileReadsAWellFormedRunsFile pins ReadRecordsFile on a file
// written by WriteRecords, and that ReadRecords returns the same records.
func TestReadRecordsFileReadsAWellFormedRunsFile(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	recs := []Record{currentRecord(t, src, cases[0]), currentRecord(t, src, cases[1])}
	path := filepath.Join(t.TempDir(), "RUNS.tsv")
	var buf bytes.Buffer
	require.NoError(t, WriteRecords(&buf, recs))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	got, err := ReadRecordsFile(path)
	require.NoError(t, err, "ReadRecordsFile")
	require.Len(t, got, len(recs))
	for i, r := range got {
		assert.Equal(t, recs[i].Config, r.Config, "record[%d]", i)
		assert.Equal(t, recs[i].InputSHA256, r.InputSHA256, "record[%d]", i)
		assert.Equal(t, recs[i].Exit, r.Exit, "record[%d]", i)
	}
}

// TestReadRecordsFileMissingFileRefuses: a missing records file is an error.
func TestReadRecordsFileMissingFileRefuses(t *testing.T) {
	t.Parallel()
	_, err := ReadRecordsFile(filepath.Join(t.TempDir(), "nope.tsv"))
	require.Error(t, err, "ReadRecordsFile on a missing file succeeded")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestReadRecordsHeaderOnlyReadsEmpty: a file with only the header reads as no
// records, not an error.
func TestReadRecordsHeaderOnlyIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := ReadRecords(strings.NewReader(strings.Join(RecordsHeader, "\t") + "\n"))
	require.NoError(t, err, "ReadRecords")
	assert.Empty(t, got)
}

// TestReadRecordsRefusesAMisShapedRow: a row with too few fields and a row with
// too many fields are both refused, naming the field count.
func TestReadRecordsRefusesAMisShapedRow(t *testing.T) {
	t.Parallel()
	head := strings.Join(RecordsHeader, "\t") + "\n"
	cases := []struct{ name, body string }{
		{"short row", head + "config\tmodule\n"},
		{"long row", head + strings.Join(make([]string, len(RecordsHeader)+1), "\t") + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadRecords(strings.NewReader(tc.body))
			require.Error(t, err, "ReadRecords accepted a mis-shaped row")
			assert.ErrorContains(t, err, "fields")
		})
	}
}

// TestReadRecordsKeepsADuplicateRow: a records file with the same config twice
// reads both rows; the duplicate is Merge's business, not ReadRecords'.
func TestReadRecordsKeepsADuplicateRow(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	rec := currentRecord(t, src, cases[0])
	var buf bytes.Buffer
	require.NoError(t, WriteRecords(&buf, []Record{rec, rec}))
	got, err := ReadRecords(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err, "ReadRecords")
	require.Len(t, got, 2, "the duplicate row is kept")
	assert.Equal(t, got[0].Config, got[1].Config)
}

// TestReadRecordsAcceptsCRLFAndTrailingWhitespace: CRLF line endings parse, and
// trailing whitespace on the last (string) field is absorbed into the value,
// while trailing whitespace on a numeric field is refused.
func TestReadRecordsAcceptsCRLFAndTrailingWhitespace(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	rec := currentRecord(t, src, cases[0])
	var buf bytes.Buffer
	require.NoError(t, WriteRecords(&buf, []Record{rec}))

	crlf := strings.ReplaceAll(buf.String(), "\n", "\r\n")
	got, err := ReadRecords(strings.NewReader(crlf))
	require.NoError(t, err, "ReadRecords on CRLF")
	require.Len(t, got, 1)
	assert.Equal(t, rec.Config, got[0].Config)

	// Trailing whitespace on the last field (mode, a string) is absorbed into
	// the value and still parses.
	withSpace := strings.TrimSuffix(buf.String(), "\n") + " \n"
	trailing, err := ReadRecords(strings.NewReader(withSpace))
	require.NoError(t, err, "ReadRecords with trailing whitespace")
	require.Len(t, trailing, 1)
	assert.Equal(t, rec.Mode+" ", trailing[0].Mode)

	// Trailing whitespace on a numeric field is refused.
	numSpace := strings.Replace(buf.String(), "\t2\t", "\t2 \t", 1)
	_, err = ReadRecords(strings.NewReader(numSpace))
	require.Error(t, err, "ReadRecords accepted trailing whitespace on a numeric field")
	assert.ErrorContains(t, err, "not a count")
}

// TestWriteRecordsReadRecordsRoundTripByteForByte: WriteRecords then ReadRecords
// reconstructs the original records exactly, and the written bytes are stable.
func TestWriteRecordsReadRecordsRoundTripByteForByte(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	recs := []Record{currentRecord(t, src, cases[0]), currentRecord(t, src, cases[1])}
	var buf bytes.Buffer
	require.NoError(t, WriteRecords(&buf, recs))
	raw := buf.Bytes()
	got, err := ReadRecords(bytes.NewReader(raw))
	require.NoError(t, err, "ReadRecords")
	var again bytes.Buffer
	require.NoError(t, WriteRecords(&again, got))
	assert.Equal(t, raw, again.Bytes(), "WriteRecords then ReadRecords then WriteRecords is not byte-for-byte")
}

// TestPlatformLabelsTheMachineAndRefusesOthers: Platform joins goos and goarch
// and refuses a pair that is not a recorded platform.
func TestPlatformLabelsTheMachineAndRefusesOthers(t *testing.T) {
	t.Parallel()
	label, err := Platform("linux", "amd64")
	require.NoError(t, err, "Platform")
	assert.Equal(t, "linux-amd64", label)
	assert.True(t, ValidPlatform(label), "linux-amd64 is not a valid platform")
	_, err = Platform("windows", "amd64")
	assert.Error(t, err, "Platform accepted windows-amd64")
	assert.False(t, ValidPlatform("darwin-arm64"), "darwin-arm64 is valid today")
}

// TestStaleGroupsNamesGroupsWithNoCurrentRecord: a stale record marks its group
// stale; current records leave no group stale; a case with no record at all is
// stale.
func TestStaleGroupsNamesGroupsWithNoCurrentRecord(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	a, b := cases[0], cases[1]
	fresh := currentRecord(t, src, a)
	stale := currentRecord(t, src, b)
	stale.InputSHA256 = strings.Repeat("0", 64)

	rows := []struct {
		name    string
		records []Record
		want    []string
	}{
		{"missing record", []Record{fresh}, []string{"tiny"}},
		{"stale record", []Record{fresh, stale}, []string{"tiny"}},
		{"all current", []Record{fresh, currentRecord(t, src, b)}, []string{}},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := StaleGroups(src, cases, tc.records)
			require.NoError(t, err, "StaleGroups")
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestMergeOrdersByPlanAndRefusesStale: Merge returns records in plan order, and
// refuses a stale record and a case with no record.
func TestMergeOrdersByPlanAndRefusesStale(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	a, b := cases[0], cases[1]
	freshA := currentRecord(t, src, a)
	freshB := currentRecord(t, src, b)

	merged, err := Merge(src, cases, []Record{freshB, freshA})
	require.NoError(t, err, "Merge")
	require.Len(t, merged, 2)
	assert.Equal(t, a.Config, merged[0].Config)
	assert.Equal(t, b.Config, merged[1].Config)

	staleA := freshA
	staleA.InputSHA256 = strings.Repeat("0", 64)
	_, err = Merge(src, cases, []Record{staleA, freshB})
	require.Error(t, err, "Merge accepted a stale record")
	assert.ErrorContains(t, err, "other inputs")

	_, err = Merge(src, cases, []Record{freshA})
	require.Error(t, err, "Merge accepted a missing case")
	var missing *MissingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, []string{b.Config}, missing.Cases)
}

// TestMergeRefusesAnUndeclaredCase: a record for a case the plan does not
// declare is refused.
func TestMergeRefusesAnUndeclaredCase(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	rec := currentRecord(t, src, cases[0])
	rec.Config = "MCGhost.cfg"
	_, err := Merge(src, cases, []Record{rec, currentRecord(t, src, cases[1])})
	require.Error(t, err, "Merge accepted an undeclared case")
	assert.ErrorContains(t, err, "no such case")
}

// TestMergeRefusesTwoJars: a set of records measured with two jars is refused,
// naming the jars and the groups to run again.
func TestMergeRefusesTwoJars(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	a, b := cases[0], cases[1]
	ra := currentRecord(t, src, a)
	rb := currentRecord(t, src, b)
	rb.JarSHA256 = strings.Repeat("b", 64)
	_, err := Merge(src, cases, []Record{ra, rb})
	require.Error(t, err, "Merge accepted two jars")
	assert.ErrorContains(t, err, "jars")
	assert.ErrorContains(t, err, "tiny")
}

// TestMissingErrorNamesTheCases: MissingError.Error names the declared cases
// that have no record.
func TestMissingErrorNamesTheCases(t *testing.T) {
	t.Parallel()
	e := &MissingError{Cases: []string{"MCA.cfg", "MCB.cfg"}}
	s := e.Error()
	assert.Contains(t, s, "2")
	assert.Contains(t, s, "MCA.cfg")
	assert.Contains(t, s, "MCB.cfg")
}

// TestCarryDropsStaleAndGoneRecords: Carry keeps current declared base records
// and drops stale ones and ones whose case the plan no longer declares.
func TestCarryDropsStaleAndGoneRecords(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	a, b := cases[0], cases[1]
	freshA := currentRecord(t, src, a)
	freshB := currentRecord(t, src, b)

	staleB := freshB
	staleB.InputSHA256 = strings.Repeat("0", 64)
	gone := Record{Config: "MCGone.cfg", Module: "MCGone.tla", InputSHA256: strings.Repeat("1", 64),
		JarSHA256: strings.Repeat("a", 64), JavaVersion: "21", Host: "linux-amd64", Exit: 0, Result: "PASS",
		Expected: "pass", Property: "-", Budget: "110", Mode: "bounded"}

	kept, dropped, err := Carry(src, cases, []Record{staleB, gone}, []Record{freshA})
	require.NoError(t, err, "Carry")
	assert.Empty(t, kept)
	require.Len(t, dropped, 2)
	byWhy := map[string]bool{}
	for _, d := range dropped {
		byWhy[d.Why] = true
	}
	assert.True(t, byWhy[DroppedStale], "want one stale")
	assert.True(t, byWhy[DroppedGone], "want one gone")

	kept, dropped, err = Carry(src, cases, []Record{freshB}, nil)
	require.NoError(t, err, "Carry")
	require.Len(t, kept, 1)
	assert.Equal(t, b.Config, kept[0].Config)
	assert.Empty(t, dropped)
}
