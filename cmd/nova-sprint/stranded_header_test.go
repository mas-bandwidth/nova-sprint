package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeStrandedBrief writes a passing brief under dir whose one header key sits below
// the first prose line, and returns its path. The header block carries the lines a card
// needs; unless the stranded key is DEPENDS-ON, `DEPENDS-ON: -` is in the block.
func writeStrandedBrief(t *testing.T, dir, id, key, value string) string {
	t.Helper()
	header := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\n"
	if key != "DEPENDS-ON" {
		header += "DEPENDS-ON: -\n"
	}
	header += "PATHS: internal/" + id + ".go\n"
	header += "Fix " + id + ".\n" + key + ": " + value
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(header)), 0o600))
	return path
}

// strandedLine is the line the stranded key lands on: 5 when the header carries no
// DEPENDS-ON line, 6 when it does.
func strandedLine(key string) int {
	if key == "DEPENDS-ON" {
		return 5
	}
	return 6
}

// A brief whose DEPENDS-ON: sits below the first prose line is refused, exit 2, nothing
// written, the refusal naming the key and its line: the gate reads only the header block,
// so the key would never be read and the card would be dealt before its dependencies
// (the owner, 2026-10-11, on epoch 17).
func TestAddRefusesADependsOnBelowTheProse(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	path := writeStrandedBrief(t, t.TempDir(), "c1", "DEPENDS-ON", "-")
	before := ta.applies()
	code, out, errs := ta.do("add --one --stream s1 --brief-file " + path)
	require.Equal(t, 2, code, "a stranded DEPENDS-ON: exit %d\n%s%s", code, out, errs)
	require.NotContains(t, out, "MOVED", "a stranded DEPENDS-ON wrote: %s", out)
	require.Contains(t, errs, "stranded-key", "the refusal does not name the check: %s", errs)
	require.Contains(t, errs, "DEPENDS-ON: on line 5", "the refusal does not name the key and its line: %s", errs)
	require.Equal(t, before, ta.applies(), "a refused add wrote")
	code, _, _ = ta.do("card --fields c1")
	require.NotEqual(t, 0, code, "the card is on the table after a refused add")
}

// brief <id> --brief-file holds the new brief to the same lint: a stranded key in it is
// refused, exit 2, nothing written, the card's brief unchanged.
func TestBriefRefusesADependsOnBelowTheProse(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream s1 --brief-file " + writeHeaderBrief(t, t.TempDir(), "b1", "-", "internal/b1.go"))
	was := ta.primary("b1").F("brief")
	before := ta.applies()
	bad := writeStrandedBrief(t, t.TempDir(), "b1", "DEPENDS-ON", "-")
	code, out, errs := ta.do("brief b1 --brief-file " + bad)
	require.Equal(t, 2, code, "a stranded DEPENDS-ON: exit %d\n%s%s", code, out, errs)
	require.NotContains(t, out, "MOVED", "a stranded DEPENDS-ON wrote: %s", out)
	require.Contains(t, errs, "stranded-key", "the refusal does not name the check: %s", errs)
	require.Contains(t, errs, "DEPENDS-ON: on line 5", "the refusal does not name the key and its line: %s", errs)
	require.Contains(t, errs, "fails the card lint", "the refusal is not the lint's: %s", errs)
	require.Equal(t, before, ta.applies(), "a refused brief wrote")
	require.Equal(t, was, ta.primary("b1").F("brief"), "a refused brief changed the card")
}

// Every header key the card format names is refused when it sits below the first prose
// line: PRIORITY:, PATHS:, NEW:, SHARED:, REPO:, BASE:, tier: and RESULT: alike, naming
// the key and its line, exit 2, nothing written.
func TestAddRefusesEveryHeaderKeyBelowTheProse(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	for _, key := range []string{"PRIORITY", "PATHS", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT"} {
		path := writeStrandedBrief(t, t.TempDir(), "c1", key, "x")
		before := ta.applies()
		code, out, errs := ta.do("add --one --stream s1 --brief-file " + path)
		require.Equal(t, 2, code, "%s below the prose: exit %d\n%s%s", key, code, out, errs)
		require.NotContains(t, out, "MOVED", "%s below the prose wrote: %s", key, out)
		require.Contains(t, errs, key+": on line "+strconv.Itoa(strandedLine(key)), "%s: the refusal does not name the key and its line: %s", key, errs)
		require.Equal(t, before, ta.applies(), "%s below the prose wrote", key)
	}
}

// preflight reports the same finding through the same path: a stranded key is one form
// finding, the brief's card id, the check and the key and its line.
func TestPreflightReportsAStrandedHeaderKey(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeStrandedBrief(t, dir, "c1", "DEPENDS-ON", "-")
	code, out, errs := ta.do("preflight --brief-dir " + dir)
	require.Equal(t, 1, code, "a stranded key: exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "PREFLIGHT c1 FAIL form:", "preflight does not report the finding: %s", out)
	require.Contains(t, out, "stranded-key", "preflight does not name the check: %s", out)
	require.Contains(t, out, "DEPENDS-ON: on line 5", "preflight does not name the key and its line: %s", out)
}
