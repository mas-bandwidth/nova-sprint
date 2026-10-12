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
func writeBaseBrief(t *testing.T, dir, repo, id, base string) string {
	t.Helper()
	lead := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nREPO: " + repo + "\n"
	if base != "" {
		lead += "BASE: " + base + "\n"
	}
	lead += "PATHS: internal/" + id + ".go\nTEST: none a fixture of the sprint branch rule\n\nFix " + id + "."
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(lead)), 0o600))
	return path
}

// Every stream lands on its own branch sprint/<stream>, and a card cut on dev or main is
// refused in every stream, naming the flow and the one command that fixes it
// (docs/SPEC-SPRINT.md section 7, the sprint branch; sprint.ProtectedLandingWhy). A card cut
// on the sprint branch, or naming no BASE, is admitted as it is. The lander refuses, before
// any git, land and its dry run alike, a batch whose effective base is dev or main
// (sprint.ProtectedLandWhy), and lands the stream's branch instead; dev moves only by
// promotion.
func TestEveryStreamLandsOnTheSprintBranchAndOnlyPromotionReachesDev(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	// add reads each brief at its base (the brief checks): a twin of the repository holds
	// every card's file on dev and on the sprint branch, and the pin is its commit
	files := map[string]string{}
	for _, id := range []string{"d1", "k1", "n1"} {
		files["internal/"+id+".go"] = "package internal\n"
	}
	repo, _ := twinRemote(t, ta, files, "dev", "sprint/mechanical-2026-10-02")
	onDev := writeBaseBrief(t, dir, repo, "d1", "dev")
	onSprint := writeBaseBrief(t, dir, repo, "k1", "sprint/mechanical-2026-10-02")
	noBase := writeBaseBrief(t, dir, repo, "n1", "")

	// a card cut on dev is refused in every stream, naming the flow and the one fix
	code, out, errs := ta.do("add --stream s1 d1 --one --brief-file " + onDev)
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, sprint.LandingFlow, "the refusal quotes the landing flow")
	assert.Contains(t, out+errs, "nova-sprint stream set s1 --base sprint/s1", "the refusal names the one fix")

	assert.Contains(t, ta.ok("add --stream s1 k1 --one --brief-file "+onSprint), "MOVED k1 -> ready", "a card cut on the sprint branch is admitted")
	assert.Contains(t, ta.ok("add --stream s1 n1 --one --brief-file "+noBase), "MOVED n1 -> ready", "a card naming no BASE is admitted")

	t.Run("the lander", func(t *testing.T) {
		t.Parallel()
		r := plainStreamRig(t)
		dev := r.git(r.remote, "rev-parse", "dev")
		for _, land := range []string{"land --repo-dir " + r.clone + " --base dev --dry-run", "land --repo-dir " + r.clone + " --base dev"} {
			code, out, errs := r.do(land)
			assert.Equal(t, 1, code, land)
			assert.Contains(t, out+errs, "LAND REFUSED stream=s1 cards=1 base=dev tip=- ids=s1-1 ", land)
			assert.Contains(t, out+errs, "card s1-1 lands on dev, a protected branch", land)
		}
		assert.Equal(t, dev, r.git(r.remote, "rev-parse", "dev"), "nothing was pushed to dev")
		assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"), "nothing was recorded")

		assert.Contains(t, r.ok("land --repo-dir "+r.clone+" --base sprint/s1"), "LAND OK stream=s1 cards=1 base=sprint/s1", "the sprint branch is where a stream lands")
		assert.Equal(t, dev, r.git(r.remote, "rev-parse", "dev"), "dev moved only by promotion")
	})
}

// One base (docs/SPEC-SPRINT.md section 7, the sprint branch). Found 2026-10-04: cards cut
// on a temporary branch and on personal ones were admitted, landed there, and were folded
// back onto the base by hand through 17 conflicts; the owner, 2026-10-05: "Prevention is
// better than cure". Once set --base records the sprint's base, add refuses, exit 2,
// nothing written, a card cut on any other branch, dev and main included, naming the card,
// its BASE, the sprint's base and the remedy, a batch all or none; a card on the sprint's
// base is admitted. brief holds a new BASE to the same rule.
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
	files := map[string]string{}
	for _, id := range []string{"x1", "k1", "k2", "m1", "m2", "p1"} {
		files["internal/"+id+".go"] = "package internal\n"
	}
	repo, sha := twinRemote(t, ta, files, "dev", "sprint/one", "sprint/mechanical-2026-10-02", "rowan/friend-health")
	side := writeBaseBrief(t, dir, repo, "x1", "rowan/friend-health")
	onBase := writeBaseBrief(t, dir, repo, "k1", "sprint/one")
	pinned := writeBaseBrief(t, dir, repo, "k2", "sprint/one@"+sha)
	onDev := writeBaseBrief(t, dir, repo, "p1", "dev")

	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 x1 --one --brief-file " + side)
	assert.Equal(t, 2, code, "a card cut on a side branch: %s%s", out, errs)
	assert.Contains(t, out+errs, "card x1 is cut on rowan/friend-health, not the sprint's base sprint/one")
	assert.Contains(t, out+errs, "nothing was written; re-cut the card with BASE: sprint/one")
	assert.NotContains(t, out, "MOVED", "nothing written")
	assert.False(t, ta.placed("x1"), "nothing written")
	assert.Equal(t, before, ta.applies(), "no store write")

	many := t.TempDir()
	writeBaseBrief(t, many, repo, "m1", "sprint/one")
	writeBaseBrief(t, many, repo, "m2", "sprint/mechanical-2026-10-02")
	code, out, errs = ta.do("add --stream s2 --brief-dir " + many)
	assert.Equal(t, 2, code, "a batch with one card off the base: %s%s", out, errs)
	assert.Contains(t, out+errs, "card m2 is cut on sprint/mechanical-2026-10-02, not the sprint's base sprint/one")
	assert.False(t, ta.placed("m1") || ta.placed("m2"), "nothing written, all or none")

	assert.Contains(t, ta.ok("add --stream s1 k1 --one --brief-file "+onBase), "MOVED k1 -> ready", "a card cut on the sprint's base is admitted")
	assert.Contains(t, ta.ok("add --stream s1 k2 --one --brief-file "+pinned), "MOVED k2 -> ready", "a pinned sprint base is the sprint's base")

	// a card cut on dev is refused in every stream, a marked one included: the mark is no
	// landing permission, and the one fix re-points the stream's base to its sprint branch
	ta.promotionStream("p")
	code, out, errs = ta.do("add --stream p p1 --one --brief-file " + onDev)
	assert.Equal(t, 2, code, "a dev card into a marked stream: %s%s", out, errs)
	assert.Contains(t, out+errs, "card p1 is cut on dev, a protected branch")
	assert.Contains(t, out+errs, sprint.LandingFlow)
	assert.Contains(t, out+errs, "nova-sprint stream set p --base sprint/p")
	assert.False(t, ta.placed("p1"), "nothing written")

	// brief: a new BASE off the sprint's base is refused, the brief kept; one on it is taken
	before = ta.applies()
	code, out, errs = ta.do("brief k1 --brief-file " + side)
	assert.Equal(t, 2, code, "brief moving k1 off the base: %s%s", out, errs)
	assert.Contains(t, out+errs, "card k1 is cut on rowan/friend-health, not the sprint's base sprint/one")
	assert.Equal(t, before, ta.applies(), "no store write")
	assert.Contains(t, ta.ok("brief k1 --brief-file "+pinned), "k1 brief replaced", "a brief on the sprint's base is taken")
}

// placed says the work table holds a primary of this id.
func (ta *testApp) placed(id string) bool {
	ta.t.Helper()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	s, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	require.NoError(ta.t, err)
	return s.Work.Placed(id) != nil
}
