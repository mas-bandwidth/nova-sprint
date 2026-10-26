package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAHeldDuplicateHoldsTheCardsThatNeedItAndAdmitsTheRest(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	open := t.TempDir()
	writeBriefFile(t, open, "open", passingBrief(
		"RESULT: open sha=000000000000 tier: pro\nREPO: owner/repo\nPATHS: internal/x/x.go\n\nTHE TASK. Fix the empty case in internal/x/x.go."))
	ta.ok("add --stream s1 --brief-dir " + open)
	dir := t.TempDir()
	writeBriefFile(t, dir, "a", passingBrief(
		"RESULT: a sha=000000000000 tier: pro\nREPO: owner/repo\nPATHS: internal/x/x.go\n\nTHE TASK. Fix the empty case in internal/x/x.go."))
	writeBriefFile(t, dir, "b", passingBrief(
		"RESULT: b sha=000000000000 tier: pro\nDEPENDS-ON: a\n\nTHE TASK. Finish b."))
	writeBriefFile(t, dir, "c", passingBrief(
		"RESULT: c sha=000000000000 tier: pro\n\nTHE TASK. Finish c."))
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir)
	require.Equal(t, 0, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "DUPLICATE a: probable duplicate of open", out)
	require.Contains(t, out, "HELD b: needs a, held as a probable duplicate", out)
	require.Contains(t, out, "ADD OK stream=s1 cards=1", out)
	require.True(t, ta.placed("c"), "the independent card is admitted")
	require.False(t, ta.placed("a"), "the duplicate is held")
	require.False(t, ta.placed("b"), "the card that needs the duplicate is held")
	ta.clean()
}
