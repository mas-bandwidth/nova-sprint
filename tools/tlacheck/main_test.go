package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// checkout is the root of this repository, two directories above the package.
func checkout(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tla", tlc.CasesFile)); err != nil {
		t.Fatalf("not a checkout root: %v", err)
	}
	return root
}

// cli runs one command in process and returns its status, stdout and stderr.
func cli(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// TestGroupsPrintsTheRequiredGroupsAsJSON: groups --root <this checkout> exits 0
// and prints a JSON list holding "liveruns".
func TestGroupsPrintsTheRequiredGroupsAsJSON(t *testing.T) {
	code, out, errs := cli("groups", "--root", checkout(t))
	if code != exitOK {
		t.Fatalf("groups exited %d: %s", code, errs)
	}
	var groups []string
	if err := json.Unmarshal([]byte(out), &groups); err != nil {
		t.Fatalf("groups did not print a JSON list: %v: %q", err, out)
	}
	found := false
	for _, g := range groups {
		if g == "liveruns" {
			found = true
		}
	}
	if !found {
		t.Fatalf("groups list has no liveruns: %v", groups)
	}
}

// TestTheBinaryAcceptsThisTreesRunnerFiles: the binary built from this tree
// checks the checkout's runner files and prints no "built from other runner
// files" line.
func TestTheBinaryAcceptsThisTreesRunnerFiles(t *testing.T) {
	code, out, errs := cli("groups", "--root", checkout(t))
	if code != exitOK {
		t.Fatalf("groups exited %d: %s", code, errs)
	}
	all := out + errs
	if strings.Contains(all, "built from other runner files") {
		t.Fatalf("the binary refuses this tree's runner files: %s", all)
	}
}

// TestInputsNameTheModule: inputs MCLiveRuns.cfg names tla/LiveRuns.tla.
func TestInputsNameTheModule(t *testing.T) {
	code, out, errs := cli("inputs", "--root", checkout(t), "MCLiveRuns.cfg")
	if code != exitOK {
		t.Fatalf("inputs exited %d: %s", code, errs)
	}
	if !strings.Contains(out, "tla/LiveRuns.tla") {
		t.Fatalf("inputs did not name tla/LiveRuns.tla: %q", out)
	}
}

// TestMergeDropsAStaleRecord pins pkg/tlc.Carry: a base record whose fingerprint
// is not what its case reads now is dropped, never carried, and the merge writes
// the fresh run's record instead.
func TestMergeDropsAStaleRecord(t *testing.T) {
	root, cases, src := plan(t)
	a, b := cases[0], cases[1]
	freshA := record(t, src, a)
	freshB := record(t, src, b)
	staleB := freshB
	staleB.InputSHA256 = strings.Repeat("0", 64)

	kept, dropped, err := tlc.Carry(src, cases, []tlc.Record{staleB}, []tlc.Record{freshA})
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if len(kept) != 0 {
		t.Fatalf("Carry kept a stale record: %+v", kept)
	}
	if len(dropped) != 1 || dropped[0].Config != b.Config || dropped[0].Why != tlc.DroppedStale {
		t.Fatalf("Carry did not drop the stale record as DroppedStale: %+v", dropped)
	}

	base := filepath.Join(root, "base.tsv")
	runs := filepath.Join(root, "runs.tsv")
	out := filepath.Join(root, "out.tsv")
	writeRecords(t, base, freshA, staleB)
	writeRecords(t, runs, freshB)
	code, _, errs := cli("merge", "--root", root, "--keep", base, "--out", out, runs)
	if code != exitOK {
		t.Fatalf("merge exited %d: %s", code, errs)
	}
	got, err := tlc.ReadRecordsFile(out)
	if err != nil {
		t.Fatalf("read merged records: %v", err)
	}
	byConfig := map[string]tlc.Record{}
	for _, r := range got {
		byConfig[r.Config] = r
	}
	if byConfig[a.Config].InputSHA256 != freshA.InputSHA256 {
		t.Fatalf("merge did not carry the base's current record for %s", a.Config)
	}
	if byConfig[b.Config].InputSHA256 != freshB.InputSHA256 {
		t.Fatalf("merge carried the stale record for %s", b.Config)
	}

	// A stale base record with no fresh run for its case is dropped, not
	// carried: the merge then has no record for the case and refuses, writing
	// nothing.
	only := filepath.Join(root, "only.tsv")
	code, _, errs = cli("merge", "--root", root, "--keep", base, "--out", only)
	if code == exitOK {
		t.Fatalf("merge carried a stale record into a complete file: %s", errs)
	}
	if _, err := os.Stat(only); !os.IsNotExist(err) {
		t.Fatalf("merge wrote a records file that carries a stale record")
	}
}

// TestRunWithoutJavaRefusesAndStartsNothing: run with no java on PATH exits 2
// naming java and writes no records.
func TestRunWithoutJavaRefusesAndStartsNothing(t *testing.T) {
	dir := t.TempDir()
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	code, _, errs := cli("run", "--root", checkout(t), "--dir", dir, "--group", "liveruns")
	if code != exitRefused {
		t.Fatalf("run exited %d, want %d: %s", code, exitRefused, errs)
	}
	if !strings.Contains(errs, "java") {
		t.Fatalf("run did not name java: %s", errs)
	}
	if _, err := os.Stat(filepath.Join(dir, tlc.RunsFile)); !os.IsNotExist(err) {
		t.Fatalf("run started when java was absent (records written)")
	}
}

// plan writes a small checkout under a temp dir: two cases and their modules.
func plan(t *testing.T) (string, []tlc.Case, tlc.Source) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := "config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n" +
		"MCTinyA.cfg\tMCTinyA.tla\tpass\t-\tcheck\ttiny\trequired\t-\n" +
		"MCTinyB.cfg\tMCTinyB.tla\tpass\t-\tcheck\ttiny\trequired\t-\n"
	writeFile(t, filepath.Join(dir, tlc.CasesFile), cases)
	for _, name := range []string{"A", "B"} {
		writeFile(t, filepath.Join(dir, "MCTiny"+name+".cfg"), "SPECIFICATION Spec\n")
		writeFile(t, filepath.Join(dir, "MCTiny"+name+".tla"),
			"---- MODULE MCTiny"+name+" ----\nVARIABLE x\nInit == x = 0\nNext == x' = x\n====\n")
	}
	parsed, err := tlc.LoadCases(root)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	src, err := tlc.SourceAt(root)
	if err != nil {
		t.Fatalf("SourceAt: %v", err)
	}
	return root, parsed, src
}

// record is a current measurement of one case.
func record(t *testing.T, src tlc.Source, c tlc.Case) tlc.Record {
	t.Helper()
	fp, files, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	return tlc.Record{
		Config: c.Config, Module: c.Module, InputSHA256: fp, InputFiles: files,
		JarSHA256: strings.Repeat("a", 64), JavaVersion: "21.0.1", Host: "linux-amd64", CPUs: 2,
		StartedUTC: "2026-10-11T00:00:00.000001+00:00", Workers: 2,
		Generated: "1", Distinct: "1", Seconds: "0.010", Exit: 0, Result: "PASS",
		Expected: c.Expected, Property: c.Property, Budget: "110", Mode: "bounded",
	}
}

func writeRecords(t *testing.T, path string, recs ...tlc.Record) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tlc.WriteRecords(f, recs); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunRefusesInvalidParallelWidths(t *testing.T) {
	root := checkout(t)
	max := runtime.NumCPU() / 2
	if max < 1 {
		max = 1
	}
	for _, value := range []string{"0", strconv.Itoa(max + 1)} {
		dir := t.TempDir()
		code, _, errs := cli("run", "--root", root, "--dir", dir, "--parallel", value)
		if code != exitRefused {
			t.Fatalf("--parallel %s exited %d, want %d: %s", value, code, exitRefused, errs)
		}
		if !strings.Contains(errs, "parallel must be between") {
			t.Fatalf("--parallel %s refusal did not explain the allowed range: %s", value, errs)
		}
		if _, err := os.Stat(filepath.Join(dir, tlc.RunsFile)); !os.IsNotExist(err) {
			t.Fatalf("--parallel %s wrote records", value)
		}
	}
}
