package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// writeBriefFile writes text under dir as name.md and returns its path.
func writeBriefFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	return path
}

// brief check runs every check add runs, in add's order, over every brief and
// collects every finding instead of stopping at the first: a directory of
// three briefs, one with an invalid PRIORITY line and one naming a TLA+ model
// with tier: pro, is one call that names both findings, exits 1, and writes
// nothing (its keys before and after are equal).
func TestBriefCheckNamesEveryFindingInOneCall(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	dir := t.TempDir()
	priority := writeBriefFile(t, dir, "priority-card", passingBrief(
		"RESULT: c sha=0123456789ab tier: pro\nPRIORITY: medium\nTHE TASK. Fix the empty case."))
	tla := writeBriefFile(t, dir, "tla-card", passingBrief(
		"RESULT: c sha=0123456789ab tier: frontier\nPATHS: tla/Foo.tla\nTHE TASK. Edit the model."))
	writeBriefFile(t, dir, "clean-card", passingBrief("RESULT: c sha=0123456789ab tier: pro\nTHE TASK. Fix the empty case."))

	before := ta.applies()
	code, out, errs := ta.do("brief check --stream s1 " + dir)
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "CHECK "+priority+" plan:", out)
	require.Contains(t, out, "CHECK "+tla+" tla-records:", out)
	require.Equal(t, 2, strings.Count(out, "\nCHECK ")+boolToInt(strings.HasPrefix(out, "CHECK ")), out)
	require.Contains(t, out, "BRIEF CHECK files=3 findings=2", out)
	require.Equal(t, before, ta.applies(), "brief check wrote")
}

// A clean brief alone is exit 0 and names no finding.
func TestBriefCheckCleanBriefExitsZero(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	dir := t.TempDir()
	writeBriefFile(t, dir, "clean-card", passingBrief("RESULT: c sha=0123456789ab tier: pro\nTHE TASK. Fix the empty case."))
	code, out, errs := ta.do("brief check --stream s1 " + dir)
	require.Equal(t, 0, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "BRIEF CHECK files=1 findings=0", out)
	require.False(t, strings.HasPrefix(out, "CHECK ") || strings.Contains(out, "\nCHECK "), out)
}

// A brief over the size bound and a lint finding in one file are both said, in
// one call, and the call exits 1.
func TestBriefCheckNamesASizeAndLintFindingTogether(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	dir := t.TempDir()
	over := writeBriefFile(t, dir, "over-card", "RESULT: c sha=0123456789ab\n"+strings.Repeat("x", store.MaxBriefBytes+100))
	code, out, errs := ta.do("brief check --stream s1 " + over)
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "CHECK "+over+" size:", out)
	require.Contains(t, out, "CHECK "+over+" rule-worktree:", out)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
