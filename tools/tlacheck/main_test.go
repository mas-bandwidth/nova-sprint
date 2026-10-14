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

	"github.com/mas-bandwidth/nova-sprint/internal/testkit/fsx"
	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkout is the root of this repository, two directories above the package.
func checkout(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "tla", tlc.CasesFile))
	require.NoErrorf(t, err, "not a checkout root")
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
	t.Parallel()
	code, out, errs := cli("groups", "--root", checkout(t))
	require.Equalf(t, exitOK, code, "groups exited %d: %s", code, errs)
	var groups []string
	require.NoErrorf(t, json.Unmarshal([]byte(out), &groups), "groups did not print a JSON list: %q", out)
	assert.Contains(t, groups, "liveruns", "groups list has no liveruns")
}

// TestTheBinaryAcceptsThisTreesRunnerFiles: the binary built from this tree
// checks the checkout's runner files and prints no "built from other runner
// files" line.
func TestTheBinaryAcceptsThisTreesRunnerFiles(t *testing.T) {
	t.Parallel()
	code, out, errs := cli("groups", "--root", checkout(t))
	require.Equalf(t, exitOK, code, "groups exited %d: %s", code, errs)
	assert.NotContains(t, out+errs, "built from other runner files", "the binary refuses this tree's runner files")
}

// TestInputsNameTheModule: inputs MCLiveRuns.cfg names tla/LiveRuns.tla.
func TestInputsNameTheModule(t *testing.T) {
	t.Parallel()
	code, out, errs := cli("inputs", "--root", checkout(t), "MCLiveRuns.cfg")
	require.Equalf(t, exitOK, code, "inputs exited %d: %s", code, errs)
	assert.Contains(t, out, "tla/LiveRuns.tla", "inputs did not name tla/LiveRuns.tla")
}

// TestMergeDropsAStaleRecord pins pkg/tlc.Carry: a base record whose fingerprint
// is not what its case reads now is dropped, never carried, and the merge writes
// the fresh run's record instead.
func TestMergeDropsAStaleRecord(t *testing.T) {
	t.Parallel()
	root, cases, src := plan(t)
	a, b := cases[0], cases[1]
	freshA := record(t, src, a)
	freshB := record(t, src, b)
	staleB := freshB
	staleB.InputSHA256 = strings.Repeat("0", 64)

	kept, dropped, err := tlc.Carry(src, cases, []tlc.Record{staleB}, []tlc.Record{freshA})
	require.NoError(t, err)
	assert.Empty(t, kept, "Carry kept a stale record")
	require.Len(t, dropped, 1)
	assert.Equal(t, b.Config, dropped[0].Config)
	assert.Equal(t, tlc.DroppedStale, dropped[0].Why)

	base := filepath.Join(root, "base.tsv")
	runs := filepath.Join(root, "runs.tsv")
	out := filepath.Join(root, "out.tsv")
	writeRecords(t, base, freshA, staleB)
	writeRecords(t, runs, freshB)
	code, _, errs := cli("merge", "--root", root, "--keep", base, "--out", out, runs)
	require.Equalf(t, exitOK, code, "merge exited %d: %s", code, errs)
	got, err := tlc.ReadRecordsFile(out)
	require.NoError(t, err)
	byConfig := map[string]tlc.Record{}
	for _, r := range got {
		byConfig[r.Config] = r
	}
	assert.Equalf(t, freshA.InputSHA256, byConfig[a.Config].InputSHA256, "merge did not carry the base's current record for %s", a.Config)
	assert.Equalf(t, freshB.InputSHA256, byConfig[b.Config].InputSHA256, "merge carried the stale record for %s", b.Config)

	// A stale base record with no fresh run for its case is dropped, not
	// carried: the merge then has no record for the case and refuses, writing
	// nothing.
	only := filepath.Join(root, "only.tsv")
	code, _, errs = cli("merge", "--root", root, "--keep", base, "--out", only)
	assert.NotEqualf(t, exitOK, code, "merge carried a stale record into a complete file: %s", errs)
	_, err = os.Stat(only)
	assert.True(t, os.IsNotExist(err), "merge wrote a records file that carries a stale record")
}

// TestRunWithoutJavaRefusesAndStartsNothing: run with no java on PATH exits 2
// naming java and writes no records. It cannot be parallel: it sets PATH.
func TestRunWithoutJavaRefusesAndStartsNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	code, _, errs := cli("run", "--root", checkout(t), "--dir", dir, "--group", "liveruns")
	require.Equalf(t, exitRefused, code, "run exited %d: %s", code, errs)
	assert.Contains(t, errs, "java", "run did not name java")
	_, err := os.Stat(filepath.Join(dir, tlc.RunsFile))
	assert.True(t, os.IsNotExist(err), "run started when java was absent (records written)")
}

// plan writes a small checkout under a temp dir: two cases and their modules.
func plan(t *testing.T) (string, []tlc.Case, tlc.Source) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	casesTSV := "config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n" +
		"MCTinyA.cfg\tMCTinyA.tla\tpass\t-\tcheck\ttiny\trequired\t-\n" +
		"MCTinyB.cfg\tMCTinyB.tla\tpass\t-\tcheck\ttiny\trequired\t-\n"
	fsx.Write(t, filepath.Join(dir, tlc.CasesFile), casesTSV)
	for _, name := range []string{"A", "B"} {
		fsx.Write(t, filepath.Join(dir, "MCTiny"+name+".cfg"), "SPECIFICATION Spec\n")
		fsx.Write(t, filepath.Join(dir, "MCTiny"+name+".tla"),
			"---- MODULE MCTiny"+name+" ----\nVARIABLE x\nInit == x = 0\nNext == x' = x\n====\n")
	}
	parsed, err := tlc.LoadCases(root)
	require.NoErrorf(t, err, "LoadCases")
	src, err := tlc.SourceAt(root)
	require.NoErrorf(t, err, "SourceAt")
	return root, parsed, src
}

// record is a current measurement of one case.
func record(t *testing.T, src tlc.Source, c tlc.Case) tlc.Record {
	t.Helper()
	fp, files, err := src.Fingerprint(c.Config)
	require.NoErrorf(t, err, "Fingerprint")
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
	require.NoError(t, err)
	require.NoError(t, tlc.WriteRecords(f, recs))
	require.NoError(t, f.Close())
}

// fakeJava writes an executable `java` that answers `-version` on standard error
// and otherwise reports a clean TLC finish on standard output.
func fakeJava(t *testing.T) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-version\" ]; then\n" +
		"  echo 'openjdk version \"21.0.1\" 2025-04-15' >&2\n" +
		"  exit 0\nfi\n" +
		"echo 'Model checking completed. No error has been found.'\n" +
		"echo '1 states generated, 1 distinct states found'\n" +
		"exit 0\n"
	dir := fsx.Tree(t, map[string]string{"java": fsx.Exec(script)})
	return filepath.Join(dir, "java")
}

// TestRunVerbOverAFixtureDirectory: `run` writes RUNS.tsv for the selected
// group when a fake java is provided, and exits 0.
func TestRunVerbOverAFixtureDirectory(t *testing.T) {
	t.Parallel()
	root, _, _ := plan(t)
	dir := t.TempDir()
	jar := filepath.Join(t.TempDir(), "tlc.jar")
	fsx.Write(t, jar, "jar")
	code, out, errs := cli("run", "--root", root, "--dir", dir, "--group", "tiny",
		"--jar", jar, "--java", fakeJava(t))
	require.Equalf(t, exitOK, code, "run exited %d: %s", code, errs)
	recs, err := tlc.ReadRecordsFile(filepath.Join(dir, tlc.RunsFile))
	require.NoError(t, err)
	assert.Lenf(t, recs, 2, "run wrote %d records, want 2: %s", len(recs), out)
}

// TestInputsRefusesAConfigNotInThePlan: `inputs` with a config the plan does
// not declare exits 2.
func TestInputsRefusesAConfigNotInThePlan(t *testing.T) {
	t.Parallel()
	root, _, _ := plan(t)
	code, _, errs := cli("inputs", "--root", root, "MCGhost.cfg")
	assert.Equalf(t, exitRefused, code, "inputs exited %d: %s", code, errs)
}

// TestMergeFailsWhenARecordIsMissing: `merge` exits 1 when the merged file has
// no record for a declared case.
func TestMergeFailsWhenARecordIsMissing(t *testing.T) {
	t.Parallel()
	root, cases, src := plan(t)
	freshA := record(t, src, cases[0])
	runs := filepath.Join(root, "runs.tsv")
	writeRecords(t, runs, freshA)
	out := filepath.Join(root, "out.tsv")
	code, _, errs := cli("merge", "--root", root, "--out", out, runs)
	assert.Equalf(t, exitFailed, code, "merge exited %d: %s", code, errs)
	_, err := os.Stat(out)
	assert.True(t, os.IsNotExist(err), "merge wrote a file on failure")
}

// TestReadRunsOnAMissingFile: readRuns returns no records and no error when the
// tree has no RUNS.tsv.
func TestReadRunsOnAMissingFile(t *testing.T) {
	t.Parallel()
	recs, err := readRuns(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, recs, "readRuns on a missing file returned records, want nil")
}

// TestHelpPrintsUsageAndExitsOK: `help` prints the usage and exits 0.
func TestHelpPrintsUsageAndExitsOK(t *testing.T) {
	t.Parallel()
	code, out, _ := cli("help")
	assert.Equalf(t, exitOK, code, "help exited %d", code)
	assert.Contains(t, out, "usage: tlacheck <verb>", "help did not print usage")
}

// TestRunRefusesAnUnknownVerb: an unknown verb exits 2.
func TestRunRefusesAnUnknownVerb(t *testing.T) {
	t.Parallel()
	code, _, errs := cli("bogus")
	assert.Equalf(t, exitRefused, code, "unknown verb exited %d: %s", code, errs)
}

// TestGroupsStalePrintsEmptyWhenNothingIsStale: `groups --stale` over a plan
// whose records are all current prints an empty JSON list and exits 0.
func TestGroupsStalePrintsEmptyWhenNothingIsStale(t *testing.T) {
	t.Parallel()
	root, cases, src := plan(t)
	writeRecords(t, filepath.Join(root, "tla", tlc.RunsFile), record(t, src, cases[0]), record(t, src, cases[1]))
	code, out, errs := cli("groups", "--stale", "--root", root)
	require.Equalf(t, exitOK, code, "groups --stale exited %d: %s", code, errs)
	assert.Equalf(t, "[]", strings.TrimSpace(out), "groups --stale")
}

func TestRunRefusesInvalidParallelWidths(t *testing.T) {
	t.Parallel()
	root := checkout(t)
	max := runtime.NumCPU() / 2
	if max < 1 {
		max = 1
	}
	for _, value := range []string{"0", strconv.Itoa(max + 1)} {
		t.Run("parallel="+value, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			code, _, errs := cli("run", "--root", root, "--dir", dir, "--parallel", value)
			assert.Equalf(t, exitRefused, code, "--parallel %s exited %d: %s", value, code, errs)
			assert.Contains(t, errs, "parallel must be between", "--parallel refusal did not explain the allowed range")
			_, err := os.Stat(filepath.Join(dir, tlc.RunsFile))
			assert.True(t, os.IsNotExist(err), "--parallel %s wrote records", value)
		})
	}
}
