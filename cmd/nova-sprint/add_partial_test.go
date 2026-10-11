package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// add --partial holds back every brief of the call the checks find a fault in,
// and every card that needs a held card through its Needs: or DEPENDS-ON: line,
// and admits the rest in one store write (docs/SPEC-SPRINT.md section 7, the
// add; docs/CLI.md). Five briefs, b2 with a PRIORITY add refuses and b4
// DEPENDS-ON b2: b1, b3 and b5 are admitted, b2 and b4 are held with their two
// lines, exit 1, and the table holds exactly the three.
func TestAddPartialHoldsWhatFailsAndWhatNeedsIt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeNeedsBrief(t, dir, "b1", "Fix b1.", "")
	writeBriefFile(t, dir, "b2", passingBrief("RESULT: b2 sha=0123456789ab tier: pro\nPRIORITY: medium\nTHE TASK. Fix b2."))
	writeNeedsBrief(t, dir, "b3", "Fix b3.", "")
	writeBriefFile(t, dir, "b4", passingBrief("RESULT: b4 sha=000000000000 tier: pro\nDEPENDS-ON: b2\n\nFix b4."))
	writeNeedsBrief(t, dir, "b5", "Fix b5.", "")
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial")
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "HELD b2: ", out)
	require.Contains(t, out, "PRIORITY", out)
	require.Contains(t, out, "HELD b4: needs b2, held back", out)
	require.Contains(t, out, "ADD OK stream=s1 cards=3 before=- ", out)
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(3), w.All, "the table holds only the admitted cards: %s", out)
	ta.clean()
}

// A card the add step's own plan refuses at the write (an id that exists
// already) is held with its refusal, and the others land (docs/CLI.md).
func TestAddPartialHoldsACardThePlanRefuses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	first := t.TempDir()
	writeNeedsBrief(t, first, "dup", "Fix dup.", "")
	ta.ok("add --stream s1 --brief-dir " + first)
	dir := t.TempDir()
	writeNeedsBrief(t, dir, "dup", "Fix dup.", "")
	writeNeedsBrief(t, dir, "c1", "Fix c1.", "")
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial")
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "HELD dup: ", out)
	require.Contains(t, out, "exists already", out)
	require.Contains(t, out, "ADD OK stream=s1 cards=1 ", out)
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(2), w.All, "the table holds the first card and c1: %s", out)
	ta.clean()
}

// With every brief clean, add --partial admits every card, exit 0, the same as
// add without the flag (docs/CLI.md).
func TestAddPartialAllCleanExitsZero(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeNeedsBrief(t, dir, "a", "Fix a.", "")
	writeNeedsBrief(t, dir, "b", "Fix b.", "")
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial")
	require.Equal(t, 0, code, "exit %d\n%s%s", code, out, errs)
	require.NotContains(t, out, "HELD ", out)
	require.Contains(t, out, "ADD OK stream=s1 cards=2 ", out)
	ta.clean()
}

// --partial names its cards by file and admits them in one write: ids, --count,
// --sentinel and a marked first add (--land-protected) are refused, exit 2
// (docs/CLI.md).
func TestAddPartialRefusesCountSentinelAndAMarkedFirstAdd(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeNeedsBrief(t, dir, "a", "Fix a.", "")
	for _, tc := range []struct{ line, why string }{
		{"add --stream s1 --brief-dir " + dir + " --partial --count 2", "--count"},
		{"add --stream s1 --brief-dir " + dir + " --partial --sentinel stop", "one step"},
		{"add --stream s1 --brief-dir " + dir + " --partial --land-protected any", "one step"},
	} {
		code, out, errs := ta.do(tc.line)
		require.Equal(t, 2, code, "%s: exit %d\n%s%s", tc.line, code, out, errs)
		require.Contains(t, errs, tc.why, "%s: %s", tc.line, errs)
	}
	ta.clean()
}
