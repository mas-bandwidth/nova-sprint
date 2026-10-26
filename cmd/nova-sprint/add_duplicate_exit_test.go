package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// duplicateExitBrief is a card brief whose REPO and PATHS overlap the open card
// of the test, with the THE TASK paragraph the duplicate check reads.
func duplicateExitBrief(id, paths, testPkg, testName, task string) string {
	return passingBrief("RESULT: "+id+" sha=000000000000 tier: pro\n"+
		"REPO: mas-bandwidth/nova-sprint\n"+
		"PATHS: "+paths+"\n"+
		"TEST: "+testPkg+" "+testName+"\n") +
		"\nTHE TASK. " + task
}

// An add that held a probable duplicate and wrote the rest exits with the
// write's code (0): the DUPLICATE line names the held card, the other card is
// on the table, and a caller that retries on a non-zero exit does not re-add
// what was written (docs/CLI.md, docs/SPEC-SPRINT.md section 11).
func TestAnAddThatHeldADuplicateAndWroteTheRestExitsZero(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	openDir := t.TempDir()
	open := writeBriefFile(t, openDir, "openwork", duplicateExitBrief(
		"openwork", "internal/dupwork/*.go,internal/dupwork/*_test.go",
		"internal/dupwork", "TestDupWork", "Fix the duplicate detection path in the add verb."))
	ta.ok("add --stream s1 --one --brief-file " + open)

	dir := t.TempDir()
	dup := writeBriefFile(t, dir, "dupwork", duplicateExitBrief(
		"dupwork", "internal/dupwork/*.go,internal/dupwork/*_test.go",
		"internal/dupwork", "TestDupWork", "Fix the duplicate detection path in the add verb."))
	other := writeBriefFile(t, dir, "otherwork", duplicateExitBrief(
		"otherwork", "internal/otherwork/*.go,internal/otherwork/*_test.go",
		"internal/otherwork", "TestOtherWork", "Change the unrelated reporting path in the add verb."))
	code, out, errs := ta.do("add --stream s1 --brief-file " + dup + " --brief-file " + other)
	require.Equal(t, 0, code, "exit %d\n%s%s", code, out, errs)
	require.Equal(t, 1, strings.Count(out, "DUPLICATE "), "one DUPLICATE line: %s", out)
	require.Contains(t, out, "DUPLICATE dupwork:", out)
	require.False(t, ta.placed("dupwork"), "the duplicate was held")
	require.True(t, ta.placed("otherwork"), "the other card was written")
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(2), w.All, "the open card and the one written: %s", out)
	ta.clean()
}

// An add whose every real card was held as a probable duplicate writes nothing
// and exits 1 (docs/CLI.md, docs/SPEC-SPRINT.md section 11).
func TestAnAddWhoseEveryCardIsADuplicateExitsOne(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	openDir := t.TempDir()
	open := writeBriefFile(t, openDir, "openwork", duplicateExitBrief(
		"openwork", "internal/dupone/*.go,internal/dupone/*_test.go,internal/duptwo/*.go,internal/duptwo/*_test.go",
		"internal/dupone", "TestDupOne", "Fix the duplicate detection path in the add verb."))
	ta.ok("add --stream s1 --one --brief-file " + open)

	dir := t.TempDir()
	one := writeBriefFile(t, dir, "dupone", duplicateExitBrief(
		"dupone", "internal/dupone/*.go,internal/dupone/*_test.go",
		"internal/dupone", "TestDupOne", "Fix the duplicate detection path in the add verb."))
	two := writeBriefFile(t, dir, "duptwo", duplicateExitBrief(
		"duptwo", "internal/duptwo/*.go,internal/duptwo/*_test.go",
		"internal/duptwo", "TestDupTwo", "Fix the duplicate detection path in the add verb."))
	code, out, errs := ta.do("add --stream s1 --brief-file " + one + " --brief-file " + two)
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Equal(t, 2, strings.Count(out, "DUPLICATE "), "one DUPLICATE line per held card: %s", out)
	require.False(t, ta.placed("dupone"), "nothing was written")
	require.False(t, ta.placed("duptwo"), "nothing was written")
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(1), w.All, "only the open card is on the table: %s", out)
	ta.clean()
}
