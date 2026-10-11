package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// add --partial runs the brief check's collector over every brief and admits
// what passes in one write: five briefs where b2 names an invalid PRIORITY and
// b4 needs b2 — b1, b3 and b5 are admitted, b2 and b4 are held with one HELD
// line each, the call exits 1, and the table holds exactly the three admitted
// cards (docs/CLI.md; docs/SPEC-SPRINT.md section 7, the add).
func TestAddPartialAdmitsWhatPassesAndHoldsTheRest(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeBriefFile(t, dir, "b1", passingBrief("Fix b1."))
	writeBriefFile(t, dir, "b2", passingBrief("RESULT: c sha=0123456789ab tier: pro\nPRIORITY: medium\nThe task. Fix b2."))
	writeBriefFile(t, dir, "b3", passingBrief("Fix b3."))
	writeBriefFile(t, dir, "b4", "Needs: b2\n"+passingBrief("RESULT: c sha=0123456789ab tier: pro\nThe task. Fix b4."))
	writeBriefFile(t, dir, "b5", passingBrief("Fix b5."))
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial")
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "HELD b2: ", out)
	require.Contains(t, out, "HELD b4: needs b2, held back", out)
	require.Contains(t, resultLine(out, errs, "ADD"), "ADD OK stream=s1 cards=3 before=- moved=3 ", out)
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(3), w.All, "the table holds only the admitted cards")
	ta.clean()
}

// A card the add step's own plan refuses at the write (an id that exists
// already) is held back with its refusal, the others land, and the call exits 1.
func TestAddPartialHoldsACardThePlanRefuses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	first := t.TempDir()
	writeBriefFile(t, first, "dup", passingBrief("Fix dup."))
	ta.ok("add --stream s1 --brief-dir " + first)
	second := t.TempDir()
	writeBriefFile(t, second, "dup", passingBrief("Fix dup again."))
	writeBriefFile(t, second, "fresh", passingBrief("Fix fresh."))
	code, out, errs := ta.do("add --stream s1 --brief-dir " + second + " --partial")
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "HELD dup: ", out)
	require.Contains(t, resultLine(out, errs, "ADD"), "ADD OK stream=s1 cards=1 before=- moved=1 ", out)
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(2), w.All, "dup exists and fresh landed")
	ta.clean()
}

// With every brief clean, add --partial is exit 0 and admits every card, the
// same as add without the flag.
func TestAddPartialAllCleanExitsZero(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	for _, n := range []string{"c1", "c2", "c3"} {
		writeBriefFile(t, dir, n, passingBrief("Fix "+n+"."))
	}
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial")
	require.Equal(t, 0, code, "exit %d\n%s%s", code, out, errs)
	require.NotContains(t, out, "HELD ", out)
	require.Contains(t, resultLine(out, errs, "ADD"), "ADD OK stream=s1 cards=3 before=- moved=3 ", out)
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(3), w.All)
	ta.clean()
}

// --partial with an id, --count, --sentinel or --land-protected is a usage
// refusal naming why (a marked first add is one step).
func TestAddPartialRefusesTheOneStepForms(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeBriefFile(t, dir, "c1", passingBrief("Fix c1."))
	for _, line := range []string{
		"add --stream s1 c1 --partial",
		"add --stream s1 --count 2 --partial",
		"add --stream s1 --brief-dir " + dir + " --sentinel stop --partial",
		"add --stream s1 --brief-dir " + dir + " --land-protected any --partial",
	} {
		code, out, errs := ta.do(line)
		require.Equal(t, 2, code, "%s: exit %d\n%s%s", line, code, out, errs)
		require.True(t, strings.Contains(out+errs, "--partial"), "%s: %s%s", line, out, errs)
	}
	ta.clean()
}
