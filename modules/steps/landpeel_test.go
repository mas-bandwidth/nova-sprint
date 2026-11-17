package steps

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// landEdges is the plan's land requests.
func landEdges(p Plan) []EdgeRequest {
	var out []EdgeRequest
	for _, e := range p.Edges {
		if e.Kind == "land" {
			out = append(out, e)
		}
	}
	return out
}

// TestTickGroupRedPeels: a group whose own red the base does not share
// (land_on_shared_red on, the default since 2026-11-15) opens no
// judgement: the same tick asks its first card alone, with the group's
// other merging cards' branches to quiet, and the rest regroup after it
// (LANDING.md "One card's red never holds the others"; tla/LandEdge.tla
// Peel). A conflict that names no card peels the same way.
func TestTickGroupRedPeels(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"ci_red", "conflict", "ci_no_verdict"} {
		s, req := threeMerging(t, false)
		s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: outcome, Reason: "failing unit"})
		p := s.tick()
		require.Empty(t, s.rejected, outcome)
		assert.Empty(t, s.openJudgements("land"), "%s: no judgement for a group's own red", outcome)
		es := landEdges(p)
		require.Len(t, es, 1, outcome)
		assert.Equal(t, []string{"p1"}, es[0].Cards, "%s: the first card alone", outcome)
		assert.True(t, es[0].SharedRed, outcome)
		assert.True(t, es[0].Fresh, "%s: the card alone gates on a fresh group sha, its own runs cancelled", outcome)
		assert.ElementsMatch(t, []string{"sprint/s1/p2", "sprint/s1/p3"}, es[0].Quiet, outcome)
		for _, c := range []string{"p1", "p2", "p3"} {
			s.requireState(c, "merging")
		}
		s.run(2)
		solo := s.request("land", "s1")
		assert.Equal(t, []string{"p1"}, solo.Cards, outcome)
		assert.Empty(t, s.openJudgements("engine"), outcome)
		s.arrive(InboxItem{Kind: "land", Request: solo.ID, Outcome: "landed", Landed: solo.Heads, Merge: sha(40), Main: true})
		s.run(2)
		assert.NotEqual(t, "merging", s.card("p1").State, "%s: the solo card landed", outcome)
		assert.Equal(t, []string{"p2", "p3"}, s.request("land", "s1").Cards, "%s: the rest regroup", outcome)
	}
}

// TestTickOwnRedGoesToFix: a card asked alone whose landing is red on a
// check proven its own (InboxItem.OwnRed) goes straight back to fix with
// the failing checks as its finding, and no judgement opens. A red not
// proven the card's own (the base's checks still running, say) stays the
// coordinator's judgement: never fix for what may be the base's red.
func TestTickOwnRedGoesToFix(t *testing.T) {
	t.Parallel()
	u := newSim(t)
	unproven := toMerging(u)
	u.arrive(InboxItem{Kind: "land", Request: unproven.ID, Outcome: "ci_red", Reason: "failing unit; not landed on the base's reds: the base tip's checks were not read complete"})
	u.tick()
	u.requireState("p1", "merging")
	assert.Len(t, u.openJudgements("land"), 1, "an unproven red is judged")

	s := newSim(t)
	first := toMerging(s)
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "ci_red", OwnRed: true, Reason: "rung 1; failing unit; first failing lines: --- FAIL: TestX"})

	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	assert.True(t, s.card("p1").Red)
	assert.True(t, strings.HasPrefix(s.card("p1").Finding, "red: head "+first.Heads[0]+" of branch sprint/s1/p1 is red onto "), s.card("p1").Finding)
	assert.Contains(t, s.card("p1").Finding, "--- FAIL: TestX")
	assert.Empty(t, s.openJudgements("land"))
}

// TestTickGroupNeverPastGroupMax: backed up with more merging cards than
// landing(stream).group_max, the group is the first group_max of them in
// landing order, and the others' branches are its quiet ones; the rest
// land in the next groups (Glenn 2026-11-15: "merge cards as they
// arrive").
func TestTickGroupNeverPastGroupMax(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 8, "pro")
	s.row("r2", 8, "pro", "heavy")
	var ps []string
	for i := 1; i <= 6; i++ {
		ps = append(ps, fmt.Sprintf("p%d", i))
		s.producer(ps[i-1], "s1", "pro", "a/"+ps[i-1])
	}
	s.run(2)
	for i, p := range ps {
		s.take(p + ".w1")
		s.report(p+".w1", "ok", sha(i+1), "done")
	}
	s.tick()
	for _, p := range ps {
		s.take(p + ".r1.1")
		s.report(p+".r1.1", "ok", s.card(p).Head, "fine")
	}
	s.run(2)
	req := s.request("land", "s1")
	require.Len(t, req.Cards, Engine.Landing.GroupMax)
	assert.Equal(t, ps[:4], req.Cards)
	assert.ElementsMatch(t, []string{"sprint/s1/p5", "sprint/s1/p6"}, req.Quiet)
}

// A solo persistent timeout (ci_no_verdict with OwnRed after bounded reruns)
// bounces back to fix up to limits.bounces times, then opens a land judgement;
// an unproven ci_no_verdict (no-ci or infra) opens a land judgement at once.
func TestTickSoloNoVerdictBouncesBoundedByLimit(t *testing.T) {
	t.Parallel()
	u := newSim(t)
	unproven := toMerging(u)
	u.arrive(InboxItem{Kind: "land", Request: unproven.ID, Outcome: "ci_no_verdict", Reason: "no verdict after 45m"})
	u.tick()
	assert.Len(t, u.openJudgements("land"), 1, "unproven ci_no_verdict opens a land judgement")

	s := newSim(t)
	req := toMerging(s)
	for b := 1; b <= Engine.Limits.Bounces; b++ {
		s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "ci_no_verdict", OwnRed: true, Reason: "stopped: unit timed out"})
		s.tick()
		require.Empty(t, s.rejected)
		s.requireState("p1", "ready")
		assert.Empty(t, s.openJudgements("land"), "bounce %d goes to fix", b)
		s.run(2)
		wID := fmt.Sprintf("p1.w%d", b+1)
		s.take(wID)
		s.report(wID, "ok", sha(b+1), "fixed")
		s.tick()
		rID := fmt.Sprintf("p1.r%d.1", s.card("p1").Review)
		s.take(rID)
		s.report(rID, "ok", sha(b+1), "fine")
		s.run(2)
		req = s.request("land", "s1")
	}
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "ci_no_verdict", OwnRed: true, Reason: "stopped: unit timed out"})
	s.tick()
	s.requireState("p1", "merging")
	assert.Equal(t, []string{"land.p1.1"}, s.openJudgements("land"), "past bounce limit opens land judgement")
}

// Multiple streams sharing the same (repo, base) lane dispatch at most one
// land request at a time in fair MergingAt FIFO order, quieting merging card
// branches across all streams on that repo without firing engine judgements.
func TestTickSerializesSharedRepoBaseLaneFIFO(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.must("streams",
		store.PutStream{Name: "s-main", Repo: "mas-bandwidth/schema", Base: "main", Forge: "github", Pos: 1},
		store.PutStream{Name: "s-py", Repo: "https://github.com/mas-bandwidth/schema.git", Base: "main", Forge: "github", Pos: 2},
		store.PutStream{Name: "s-unity", Repo: "git@github.com:mas-bandwidth/schema.git", Base: "main", Forge: "github", Pos: 3},
	)
	s.row("r1", 6, "pro")
	s.row("r2", 6, "pro", "heavy")
	s.producer("pm", "s-main", "pro", "a/m")
	s.producer("pp", "s-py", "pro", "a/p")
	s.producer("pu", "s-unity", "pro", "a/u")
	s.run(2)
	for i, id := range []string{"pu", "pp", "pm"} {
		s.take(id + ".w1")
		s.report(id+".w1", "ok", sha(i+1), "done")
		s.tick()
		s.take(id + ".r1.1")
		s.report(id+".r1.1", "ok", sha(i+1), "fine")
		s.tick()
	}
	s.run(2)
	assert.Empty(t, s.openJudgements("engine"))
	var active []EdgeRequest
	for _, r := range s.inflight {
		if r.Kind == "land" {
			active = append(active, r)
		}
	}
	require.Len(t, active, 1, "at most one land request on shared mas-bandwidth/schema@main lane")
	assert.Equal(t, "s-unity", active[0].Stream)

	s.arrive(InboxItem{Kind: "land", Request: active[0].ID, Outcome: "landed", Landed: active[0].Heads, Merge: sha(50), Main: true})
	s.run(2)
	assert.Empty(t, s.openJudgements("engine"))
	reqPy := s.request("land", "s-py")
	assert.Equal(t, []string{"pp"}, reqPy.Cards)
	assert.Contains(t, reqPy.Quiet, "sprint/s-main/pm")
}
