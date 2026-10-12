package main

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// No stream lands a card on a protected branch, dev or main (docs/SPEC-SPRINT.md section 7,
// the protected branches): add refuses a card cut on either, stream set --promotion is
// refused, and the lander refuses a batch whose base is dev or main before any git, naming
// the stream, the flow and the one command `nova-sprint stream set <s> --base sprint/<s>`.
// --promotion=false and --land-protected default take an old mark off.
func TestAddAndLandRefuseDevAndMainInEveryStream(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	// add reads each brief at its base (the brief checks): a twin of the repository holds
	// every card's file on main, dev and the sprint branch, and the pin is its commit
	files := map[string]string{}
	for _, id := range []string{"p1", "p2", "d1", "n1", "m1", "m2"} {
		files["internal/"+id+".go"] = "package internal\n"
	}
	repo, sha := twinRemote(t, ta, files, "main", "dev", "sprint/mechanical-2026-10-02")
	onMain := writeBaseBrief(t, dir, repo, "p1", "main")
	onMain2 := writeBaseBrief(t, dir, repo, "p2", "main@"+sha)
	onDev := writeBaseBrief(t, dir, repo, "d1", "dev")

	for _, c := range []struct{ name, line, card string }{
		{"one brief on main", "add --stream s1 p1 --one --brief-file " + onMain, "p1"},
		{"a pinned main base", "add --stream s1 p2 --one --brief-file " + onMain2, "p2"},
		{"a count of cards on main", "add --stream s1 --count 2 --brief-file " + onMain, "s1-1"},
		{"one brief on dev", "add --stream s1 d1 --one --brief-file " + onDev, "d1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := ta.applies()
			code, out, errs := ta.do(c.line)
			assert.NotEqual(t, 0, code, "%s: %s%s", c.line, out, errs)
			assert.Contains(t, out+errs, "card "+c.card+" is cut on ", c.line)
			assert.Contains(t, out+errs, "a protected branch: "+sprint.LandingFlow, c.line)
			assert.Contains(t, out+errs, "nova-sprint stream set s1 --base sprint/s1", c.line)
			assert.NotContains(t, out, "MOVED", "nothing written")
			assert.False(t, ta.placed(c.card), "nothing written")
			assert.Equal(t, before, ta.applies(), "no store write")
		})
	}

	many := t.TempDir()
	writeBaseBrief(t, many, repo, "m1", "sprint/mechanical-2026-10-02")
	writeBaseBrief(t, many, repo, "m2", "main")
	code, out, errs := ta.do("add --stream s2 --brief-dir " + many)
	assert.NotEqual(t, 0, code, "many-brief add with a main card: %s%s", out, errs)
	assert.Contains(t, out+errs, "card m2 is cut on main, a protected branch")
	assert.False(t, ta.placed("m1") || ta.placed("m2"), "nothing written, all or none")

	// a card naming no BASE is admitted (the lander's --base lands it)
	assert.Contains(t, ta.ok("add --stream s1 n1 --one --brief-file "+writeBaseBrief(t, dir, repo, "n1", "")), "MOVED n1 -> ready")

	// stream set --promotion is refused; --promotion=false takes an old mark off
	code, out, errs = ta.do("stream set s1 --promotion")
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, sprint.LandingFlow)
	assert.Contains(t, ta.ok("stream set s1 --promotion=false"), "land-protected none")

	t.Run("the lander", func(t *testing.T) {
		t.Parallel()
		for _, base := range []string{"dev", "main"} {
			t.Run(base, func(t *testing.T) {
				t.Parallel()
				r := plainStreamRig(t)
				tip := r.git(r.remote, "rev-parse", base)
				for _, land := range []string{"land --repo-dir " + r.clone + " --base " + base + " --dry-run", "land --repo-dir " + r.clone + " --base " + base} {
					code, out, errs := r.do(land)
					assert.Equal(t, 1, code, land)
					assert.Contains(t, out+errs, "LAND REFUSED stream=s1 cards=1 base="+base+" tip=- ids=s1-1 ", land)
					assert.Contains(t, out+errs, "card s1-1 lands on "+base+", a protected branch", land)
					assert.Contains(t, out+errs, sprint.LandingFlow, land)
					assert.Contains(t, out+errs, "nova-sprint stream set s1 --base sprint/s1\n", land)
				}
				assert.Equal(t, tip, r.git(r.remote, "rev-parse", base), "nothing was pushed to "+base)
				assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"), "nothing was recorded")
			})
		}
	})
}

// plainStreamRig is a land rig with origin's dev and sprint/s1 pushed, one queued card
// (s1-1) cut on sprint/s1, and s1 a plain stream: queued marks every stream land-protected
// any, and the protected-base lander tests start from a stream that is not, so it queues the
// card itself without the mark.
func plainStreamRig(t *testing.T) *landRig {
	t.Helper()
	r := newLandRig(t)
	for _, b := range []string{"dev", "sprint/s1"} {
		r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/"+b)
	}
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 1 --one")
	plainQueued(r, map[string]string{"s1-1": r.head("s1-1", "sprint/s1", "a.txt", "a\n")}, "s1-1")
	return r
}

// plainQueued pushes the cards' heads and takes them through the sprint to the merge queue,
// each finished at its head, without marking the stream (queued marks every stream).
func plainQueued(r *landRig, heads map[string]string, order ...string) {
	r.t.Helper()
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(len(order))
	r.ok("take --as m1 --limit 100")
	for _, id := range order {
		r.ok("finish --as m1 " + id + ".w1@1 --head " + heads[id])
	}
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
}

// promotionStream marks stream for a protected branch before a card cut on dev or main is
// added to it (docs/SPEC-SPRINT.md section 7, the protected branches): the verb that wrote
// the mark (`stream set --promotion`) is refused now, so the mark is written on the store
// directly, as an old store carries it, and the founding card is dropped.
func (ta *testApp) promotionStream(stream string) {
	ta.t.Helper()
	ta.ok("add --stream " + stream + " " + stream + "-founder --one")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(ta.t, err)
	step := store.Step{
		Verb: "raw mark",
		Load: []string{sprint.Merge},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			ctl := s.StreamCtl(stream)
			return sprint.Plan{Units: []sprint.Unit{{Key: ctl.ID, Stream: stream,
				Changes: []sprint.Change{{Table: sprint.Merge, Entry: ntable.BatchMemberEntry{
					ID:     ctl.ID,
					Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(ctl.Rev, 10), Place: &ntable.PlaceExpect{Row: ctl.Row, Col: ctl.Col}},
					Set:    map[string]string{sprint.FieldLandProtected: sprint.LandProtectedAny},
				}}}}}}
		},
	}
	_, err = st.Run(context.Background(), step)
	require.NoError(ta.t, err)
	ta.ok("drop " + stream + "-founder --reason 'it founded the promotion stream'")
}
