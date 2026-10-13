package main

import (
	"bytes"
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

// A store that opens but does not answer the plan's one read-only snapshot load
// is the store not answering: the store-free checks alone run and it says so,
// exit 3 (not 0: half the checks did not run), never a false clean. The brief
// here names a PRIORITY add refuses, so against a store that answered the call
// would find it.
func TestBriefCheckSaysWhenTheStoreDidNotAnswer(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	dir := t.TempDir()
	writeBriefFile(t, dir, "priority-card", passingBrief(
		"RESULT: c sha=0123456789ab tier: pro\nPRIORITY: medium\nTHE TASK. Fix the empty case."))
	code, out, errs := ta.bare("brief check --stream s1 " + dir)
	require.Equal(t, 3, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "CHECK store: not reached", out)
	require.Contains(t, out, "the store's checks (names, dropped cards, needs, the plan) were not run", out)
	require.NotContains(t, out, "findings=1", "a store that did not answer is not a clean brief: %s", out)
	require.Contains(t, out, "BRIEF CHECK files=1 findings=0", out)
}

// A model-line fault in one brief never hides another brief's lint findings:
// the card lint runs once per brief, so a directory with a bad model line and a
// brief with lint drift names both findings in one call.
func TestBriefCheckKeepsEveryBriefsLintFindings(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	dir := t.TempDir()
	bad := writeBriefFile(t, dir, "bad-model", passingBrief("tier: bogus\nTHE TASK. Fix the empty case."))
	drift := writeBriefFile(t, dir, "drift-card", "RESULT: c sha=0123456789ab tier: pro\nTHE TASK. Fix the empty case.")
	code, out, errs := ta.do("brief check --stream s1 " + dir)
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "CHECK "+bad+" lint:", out)
	require.Contains(t, out, "CHECK "+drift+" rule-worktree:", out)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// The tla-records rule reads the card's base tree, as the lander does (internal/sprint/
// land_records.go, which reads tlc.CasesFile): a repository with no tla/CASES.tsv holds
// no models, so a model card with no tlacheck STEP checks clean; one whose base holds the
// cases table is refused as today; and a base no tree was read for is never a clean pass.
func TestBriefCheckTlaRecordsSkipsARepoWithNoCases(t *testing.T) {
	t.Parallel()
	const modelCard = "RESULT: c sha=0123456789ab tier: frontier\nPATHS: tla/Foo.tla\nTHE TASK. Edit the model.\n"
	for _, tc := range []struct {
		name     string
		tree     []string
		haveTree bool
		refused  bool
	}{
		{"a base tree with no cases table skips the rule", []string{"README.md", "tla/Foo.tla"}, true, false},
		{"a base tree that holds the cases table is refused", []string{"tla/CASES.tsv", "tla/Foo.tla", "tla/MCFoo.cfg"}, true, true},
		{"a base no tree was read for keeps the rule", nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			code := briefTlaRecordsHold(&stderr, briefCheck{id: "c", brief: modelCard}, tc.tree, tc.haveTree)
			if !tc.refused {
				require.Equal(t, 0, code, "the base holds no cases table, so the rule does not apply: %s", stderr.String())
				require.Empty(t, stderr.String(), "the base holds no cases table, so the rule prints nothing")
				return
			}
			require.Equal(t, 2, code, stderr.String())
			require.Contains(t, stderr.String(), "tla/Foo.tla")
			require.Contains(t, stderr.String(), "does not cover tla/RUNS.tsv")
			require.Contains(t, stderr.String(), "names no STEP")
		})
	}
}
