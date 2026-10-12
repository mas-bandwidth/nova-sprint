package tlc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordsPlan writes a small checkout under a temp dir: two current cases and
// their modules, and a Source over it.
func recordsPlan(t *testing.T) (string, []Case, Source) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := "config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n" +
		"MCTinyA.cfg\tMCTinyA.tla\tpass\t-\tcheck\ttiny\trequired\t-\n" +
		"MCTinyB.cfg\tMCTinyB.tla\tpass\t-\tcheck\ttiny\trequired\t-\n"
	if err := os.WriteFile(filepath.Join(dir, CasesFile), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B"} {
		if err := os.WriteFile(filepath.Join(dir, "MCTiny"+name+".cfg"), []byte("SPECIFICATION Spec\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "MCTiny"+name+".tla"),
			[]byte("---- MODULE MCTiny"+name+" ----\nVARIABLE x\nInit == x = 0\nNext == x' = x\n====\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := LoadCases(root)
	if err != nil {
		t.Fatal(err)
	}
	src, err := SourceAt(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, parsed, src
}

// currentRecord is a current measurement of one case.
func currentRecord(t *testing.T, src Source, c Case) Record {
	t.Helper()
	fp, files, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := WriteRecords(&buf, recs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRecordsFile(path)
	if err != nil {
		t.Fatalf("ReadRecordsFile: %v", err)
	}
	if len(got) != len(recs) {
		t.Fatalf("read %d records, want %d", len(got), len(recs))
	}
	for i, r := range got {
		if r.Config != recs[i].Config || r.InputSHA256 != recs[i].InputSHA256 || r.Exit != recs[i].Exit {
			t.Fatalf("record[%d]=%+v want %+v", i, r, recs[i])
		}
	}
}

// TestReadRecordsFileMissingFileRefuses: a missing records file is an error.
func TestReadRecordsFileMissingFileRefuses(t *testing.T) {
	t.Parallel()
	_, err := ReadRecordsFile(filepath.Join(t.TempDir(), "nope.tsv"))
	if err == nil {
		t.Fatal("ReadRecordsFile on a missing file succeeded")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadRecordsFile error is not a not-exist error: %v", err)
	}
}

// TestReadRecordsHeaderOnlyReadsEmpty: a file with only the header reads as no
// records, not an error.
func TestReadRecordsHeaderOnlyIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := ReadRecords(strings.NewReader(strings.Join(RecordsHeader, "\t") + "\n"))
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("read %d records from a header-only file, want 0", len(got))
	}
}

// TestReadRecordsRefusesAShortRow: a row with too few fields is refused.
func TestReadRecordsRefusesAShortRow(t *testing.T) {
	t.Parallel()
	short := strings.Join(RecordsHeader, "\t") + "\nconfig\tmodule\n"
	_, err := ReadRecords(strings.NewReader(short))
	if err == nil {
		t.Fatal("ReadRecords accepted a short row")
	}
	if !strings.Contains(err.Error(), "fields") {
		t.Fatalf("short-row error does not say fields: %v", err)
	}
}

// TestReadRecordsRefusesALongRow: a row with too many fields is refused.
func TestReadRecordsRefusesALongRow(t *testing.T) {
	t.Parallel()
	long := strings.Join(RecordsHeader, "\t") + "\n" + strings.Join(make([]string, len(RecordsHeader)+1), "\t") + "\n"
	_, err := ReadRecords(strings.NewReader(long))
	if err == nil {
		t.Fatal("ReadRecords accepted a long row")
	}
	if !strings.Contains(err.Error(), "fields") {
		t.Fatalf("long-row error does not say fields: %v", err)
	}
}

// TestReadRecordsKeepsADuplicateRow: a records file with the same config twice
// reads both rows; the duplicate is Merge's business, not ReadRecords'.
func TestReadRecordsKeepsADuplicateRow(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	rec := currentRecord(t, src, cases[0])
	var buf bytes.Buffer
	if err := WriteRecords(&buf, []Record{rec, rec}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRecords(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d rows, want 2 (the duplicate row is kept)", len(got))
	}
	if got[0].Config != got[1].Config {
		t.Fatalf("duplicate rows differ: %s vs %s", got[0].Config, got[1].Config)
	}
}

// TestReadRecordsAcceptsCRLFAndTrailingWhitespace: CRLF line endings parse, and
// trailing whitespace on the last (string) field is absorbed into the value,
// while trailing whitespace on a numeric field is refused.
func TestReadRecordsAcceptsCRLFAndTrailingWhitespace(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	rec := currentRecord(t, src, cases[0])
	var buf bytes.Buffer
	if err := WriteRecords(&buf, []Record{rec}); err != nil {
		t.Fatal(err)
	}
	crlf := strings.ReplaceAll(buf.String(), "\n", "\r\n")
	got, err := ReadRecords(strings.NewReader(crlf))
	if err != nil {
		t.Fatalf("ReadRecords on CRLF: %v", err)
	}
	if len(got) != 1 || got[0].Config != rec.Config {
		t.Fatalf("CRLF read wrong records: %+v", got)
	}

	// Trailing whitespace on the last field (mode, a string) is absorbed into
	// the value and still parses.
	withSpace := strings.TrimSuffix(buf.String(), "\n") + " \n"
	trailing, err := ReadRecords(strings.NewReader(withSpace))
	if err != nil {
		t.Fatalf("ReadRecords with trailing whitespace: %v", err)
	}
	if len(trailing) != 1 || trailing[0].Mode != rec.Mode+" " {
		t.Fatalf("trailing whitespace read %+v, want mode %q", trailing, rec.Mode+" ")
	}

	// Trailing whitespace on a numeric field is refused.
	numSpace := strings.Replace(buf.String(), "\t2\t", "\t2 \t", 1)
	if _, err := ReadRecords(strings.NewReader(numSpace)); err == nil {
		t.Fatal("ReadRecords accepted trailing whitespace on a numeric field")
	} else if !strings.Contains(err.Error(), "not a count") {
		t.Fatalf("numeric-field whitespace error does not say 'not a count': %v", err)
	}
}

// TestWriteRecordsReadRecordsRoundTripByteForByte: WriteRecords then ReadRecords
// reconstructs the original records exactly, and the written bytes are stable.
func TestWriteRecordsReadRecordsRoundTripByteForByte(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	recs := []Record{currentRecord(t, src, cases[0]), currentRecord(t, src, cases[1])}
	var buf bytes.Buffer
	if err := WriteRecords(&buf, recs); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	got, err := ReadRecords(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	var again bytes.Buffer
	if err := WriteRecords(&again, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, again.Bytes()) {
		t.Fatal("WriteRecords then ReadRecords then WriteRecords is not byte-for-byte")
	}
}

// TestPlatformLabelsTheMachineAndRefusesOthers: Platform joins goos and goarch
// and refuses a pair that is not a recorded platform.
func TestPlatformLabelsTheMachineAndRefusesOthers(t *testing.T) {
	t.Parallel()
	label, err := Platform("linux", "amd64")
	if err != nil {
		t.Fatalf("Platform: %v", err)
	}
	if label != "linux-amd64" {
		t.Fatalf("Platform= %q, want linux-amd64", label)
	}
	if !ValidPlatform(label) {
		t.Fatal("linux-amd64 is not a valid platform")
	}
	if _, err := Platform("windows", "amd64"); err == nil {
		t.Fatal("Platform accepted windows-amd64")
	}
	if ValidPlatform("darwin-arm64") {
		t.Fatal("darwin-arm64 is valid today")
	}
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

	got, err := StaleGroups(src, cases, []Record{fresh})
	if err != nil {
		t.Fatalf("StaleGroups: %v", err)
	}
	if len(got) != 1 || got[0] != "tiny" {
		t.Fatalf("StaleGroups with a missing record = %v, want [tiny]", got)
	}

	got, err = StaleGroups(src, cases, []Record{fresh, stale})
	if err != nil {
		t.Fatalf("StaleGroups: %v", err)
	}
	if len(got) != 1 || got[0] != "tiny" {
		t.Fatalf("StaleGroups with a stale record = %v, want [tiny]", got)
	}

	got, err = StaleGroups(src, cases, []Record{fresh, currentRecord(t, src, b)})
	if err != nil {
		t.Fatalf("StaleGroups: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("StaleGroups with all current records = %v, want []", got)
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
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(merged) != 2 || merged[0].Config != a.Config || merged[1].Config != b.Config {
		t.Fatalf("Merge order = %v, want plan order A,B", merged)
	}

	staleA := freshA
	staleA.InputSHA256 = strings.Repeat("0", 64)
	if _, err := Merge(src, cases, []Record{staleA, freshB}); err == nil {
		t.Fatal("Merge accepted a stale record")
	} else if !strings.Contains(err.Error(), "other inputs") {
		t.Fatalf("Merge stale error has no 'other inputs': %v", err)
	}

	_, err = Merge(src, cases, []Record{freshA})
	if err == nil {
		t.Fatal("Merge accepted a missing case")
	}
	var missing *MissingError
	if !errors.As(err, &missing) {
		t.Fatalf("Merge missing error is not *MissingError: %v", err)
	}
	if len(missing.Cases) != 1 || missing.Cases[0] != b.Config {
		t.Fatalf("Merge missing cases = %v, want [%s]", missing.Cases, b.Config)
	}
}

// TestMergeRefusesAnUndeclaredCase: a record for a case the plan does not
// declare is refused.
func TestMergeRefusesAnUndeclaredCase(t *testing.T) {
	t.Parallel()
	_, cases, src := recordsPlan(t)
	rec := currentRecord(t, src, cases[0])
	rec.Config = "MCGhost.cfg"
	_, err := Merge(src, cases, []Record{rec, currentRecord(t, src, cases[1])})
	if err == nil {
		t.Fatal("Merge accepted an undeclared case")
	}
	if !strings.Contains(err.Error(), "no such case") {
		t.Fatalf("Merge undeclared error = %v", err)
	}
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
	if err == nil {
		t.Fatal("Merge accepted two jars")
	}
	if !strings.Contains(err.Error(), "jars") || !strings.Contains(err.Error(), "tiny") {
		t.Fatalf("Merge two-jar error = %v", err)
	}
}

// TestMissingErrorNamesTheCases: MissingError.Error names the declared cases
// that have no record.
func TestMissingErrorNamesTheCases(t *testing.T) {
	t.Parallel()
	e := &MissingError{Cases: []string{"MCA.cfg", "MCB.cfg"}}
	s := e.Error()
	if !strings.Contains(s, "2") || !strings.Contains(s, "MCA.cfg") || !strings.Contains(s, "MCB.cfg") {
		t.Fatalf("MissingError.Error=%q", s)
	}
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
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if len(kept) != 0 {
		t.Fatalf("Carry kept %d records, want 0: %+v", len(kept), kept)
	}
	if len(dropped) != 2 {
		t.Fatalf("Carry dropped %d, want 2: %+v", len(dropped), dropped)
	}
	byWhy := map[string]bool{}
	for _, d := range dropped {
		byWhy[d.Why] = true
	}
	if !byWhy[DroppedStale] || !byWhy[DroppedGone] {
		t.Fatalf("Carry dropped = %+v, want one stale and one gone", dropped)
	}

	kept, dropped, err = Carry(src, cases, []Record{freshB}, nil)
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if len(kept) != 1 || kept[0].Config != b.Config || len(dropped) != 0 {
		t.Fatalf("Carry of a current record = kept %v dropped %v", kept, dropped)
	}
}
