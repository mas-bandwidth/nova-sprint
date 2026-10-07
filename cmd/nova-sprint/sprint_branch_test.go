package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// writeBaseBrief writes a passing brief whose header block names its repository and,
// unless base is "", its BASE: line, as a card the coordinator cuts does, and returns
// its path.
func writeBaseBrief(t *testing.T, dir, id, base string) string {
	t.Helper()
	lead := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nREPO: mas-bandwidth/nova-tools\n"
	if base != "" {
		lead += "BASE: " + base + "\n"
	}
	lead += "PATHS: internal/" + id + ".go\n\nFix " + id + "."
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(lead)), 0o600))
	return path
}

// Every stream lands on the sprint branch, and promotion alone reaches dev
// (docs/SPEC-SPRINT.md section 7, the sprint branch). Found 2026-10-04: cards cut with
// BASE dev were merged by the lander straight onto dev, and the merge queue's promotion
// run restarted every time. add refuses a card whose BASE is dev in a stream that is not
// the promotion stream, the one-brief and the many-brief form alike, nothing written,
// with the sprint branch as the remedy; a card cut on the sprint branch, or naming no
// BASE (the lander's --base), is admitted, and quack, which cuts its own briefs, is held
// to the same rule. The lander refuses, before any git, land and its dry run alike, a
// batch whose effective base is dev in a stream that is not the promotion stream,
// nothing pushed or recorded, naming the sprint branch and the mark
// (sprint.ProtectedLandWhy); the same batch lands on the sprint branch, and dev does not
// move. Only the mark `stream set --land-protected` writes opens dev to a stream
// (TestLandRefusesAProtectedBaseUntilTheStreamIsMarked).
func TestEveryStreamLandsOnTheSprintBranchAndOnlyPromotionReachesDev(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	onDev := writeBaseBrief(t, dir, "d1", "dev")
	pinned := writeBaseBrief(t, dir, "d2", "dev@0123456789012345678901234567890123456789")
	onSprint := writeBaseBrief(t, dir, "k1", "sprint/mechanical-2026-10-02")
	noBase := writeBaseBrief(t, dir, "n1", "")

	for _, c := range []struct{ name, line, card string }{
		{"one brief", "add --stream s1 d1 --one --brief-file " + onDev, "d1"},
		{"a pinned dev base", "add --stream s1 d2 --one --brief-file " + pinned, "d2"},
		{"a count of cards", "add --stream s1 --count 2 --brief-file " + onDev, "s1-1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := ta.applies()
			code, out, errs := ta.do(c.line)
			assert.NotEqual(t, 0, code, "%s: %s%s", c.line, out, errs)
			assert.Contains(t, out+errs, "card "+c.card+" is cut on dev, and stream s1 is not the promotion stream: every stream lands on the sprint branch, and promotion alone reaches dev")
			assert.Contains(t, out+errs, "re-cut the card with BASE: <the sprint branch> (sprint/<name>, the branch its stream lands on)")
			assert.NotContains(t, out, "MOVED", "nothing written")
			assert.False(t, ta.placed(c.card), "nothing written")
			assert.Equal(t, before, ta.applies(), "no store write")
		})
	}

	many := t.TempDir()
	writeBaseBrief(t, many, "m1", "sprint/mechanical-2026-10-02")
	writeBaseBrief(t, many, "m2", "dev")
	code, out, errs := ta.do("add --stream s2 --brief-dir " + many)
	assert.NotEqual(t, 0, code, "many-brief add with a dev card: %s%s", out, errs)
	assert.Contains(t, out+errs, "card m2 is cut on dev, and stream s2 is not the promotion stream")
	assert.False(t, ta.placed("m1") || ta.placed("m2"), "nothing written, all or none")

	assert.Contains(t, ta.ok("add --stream s1 k1 --one --brief-file "+onSprint), "MOVED k1 -> ready", "a card cut on the sprint branch is admitted")
	assert.Contains(t, ta.ok("add --stream s1 n1 --one --brief-file "+noBase), "MOVED n1 -> ready", "a card naming no BASE lands on the lander's --base")

	code, out, errs = ta.do("quack --streams q --count 1 --repo https://example.com/quack.git --base dev")
	assert.NotEqual(t, 0, code, "quack on dev: %s%s", out, errs)
	assert.Contains(t, out+errs, "is cut on dev, and stream q is not the promotion stream", "quack is held to the rule as add is")
	assert.Contains(t, ta.ok("quack --streams q --count 1 --repo https://example.com/quack.git"), "MOVED", "quack's default base is the sprint branch")

}

// One base (docs/SPEC-SPRINT.md section 7, the sprint branch). Found 2026-10-04: cards cut
// on a temporary branch and on personal ones were admitted, landed there, and were folded
// back onto the base by hand through 17 conflicts; the owner, 2026-10-05: "Prevention is
// better than cure". Once set --base records the sprint's base, add refuses, exit 2,
// nothing written, a card cut on any other branch outside the promotion stream, naming
// the card, its BASE, the sprint's base and the remedy, a batch all or none; a card on the
// sprint's base is admitted, and the promotion stream admits a card on dev. brief holds a
// new BASE to the same rule.
func TestAddRefusesACardCutOnAnyBranchButTheSprintBase(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	for _, bad := range []string{"dev", "main"} {
		code, out, errs := ta.do("set --base " + bad)
		assert.Equal(t, 1, code, "set --base %s is refused as set refuses any setting: %s%s", bad, out, errs)
		assert.Contains(t, out+errs, "--base wants the sprint's base branch")
	}
	ta.ok("set --base sprint/one")
	dir := t.TempDir()
	side := writeBaseBrief(t, dir, "x1", "rowan/friend-health")
	onBase := writeBaseBrief(t, dir, "k1", "sprint/one")
	pinned := writeBaseBrief(t, dir, "k2", "sprint/one@0123456789012345678901234567890123456789")
	onDev := writeBaseBrief(t, dir, "p1", "dev")

	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 x1 --one --brief-file " + side)
	assert.Equal(t, 2, code, "a card cut on a side branch: %s%s", out, errs)
	assert.Contains(t, out+errs, "card x1 is cut on rowan/friend-health, not the sprint's base sprint/one, and stream s1 is not the promotion stream")
	assert.Contains(t, out+errs, "nothing was written; re-cut the card with BASE: sprint/one, or, for the promotion stream, run: nova-sprint stream set s1 --land-protected <owner/name,...|any>")
	assert.NotContains(t, out, "MOVED", "nothing written")
	assert.False(t, ta.placed("x1"), "nothing written")
	assert.Equal(t, before, ta.applies(), "no store write")

	many := t.TempDir()
	writeBaseBrief(t, many, "m1", "sprint/one")
	writeBaseBrief(t, many, "m2", "sprint/mechanical-2026-10-02")
	code, out, errs = ta.do("add --stream s2 --brief-dir " + many)
	assert.Equal(t, 2, code, "a batch with one card off the base: %s%s", out, errs)
	assert.Contains(t, out+errs, "card m2 is cut on sprint/mechanical-2026-10-02, not the sprint's base sprint/one, and stream s2 is not the promotion stream")
	assert.False(t, ta.placed("m1") || ta.placed("m2"), "nothing written, all or none")

	assert.Contains(t, ta.ok("add --stream s1 k1 --one --brief-file "+onBase), "MOVED k1 -> ready", "a card cut on the sprint's base is admitted")
	assert.Contains(t, ta.ok("add --stream s1 k2 --one --brief-file "+pinned), "MOVED k2 -> ready", "a pinned sprint base is the sprint's base")

	// the promotion stream: marked by the coordinator, it admits a card cut on dev
	ta.ok("add --stream p --count 2")
	ta.ok("stream set p --land-protected any")
	assert.Contains(t, ta.ok("add --stream p p1 --one --brief-file "+onDev), "MOVED p1 -> ready", "the promotion stream keeps its mark")

	// brief: a new BASE off the sprint's base is refused, the brief kept; one on it is taken
	before = ta.applies()
	code, out, errs = ta.do("brief k1 --brief-file " + side)
	assert.Equal(t, 2, code, "brief moving k1 off the base: %s%s", out, errs)
	assert.Contains(t, out+errs, "card k1 is cut on rowan/friend-health, not the sprint's base sprint/one, and stream s1 is not the promotion stream")
	assert.Equal(t, before, ta.applies(), "no store write")
	assert.Contains(t, ta.ok("brief k1 --brief-file "+pinned), "k1 brief replaced", "a brief on the sprint's base is taken")
	assert.Contains(t, ta.ok("brief p1 --brief-file "+side), "p1 brief replaced", "the promotion stream takes any base")
}

// placed says the work table holds a primary of this id.
func (ta *testApp) placed(id string) bool {
	ta.t.Helper()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	s, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	require.NoError(ta.t, err)
	return s.Work.Placed(id) != nil
}
