package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threeMerging is stream s1 with p1, p2 and p3 merging at sha(1..3), and
// the one group request (rung 2) that lands them together; held, with
// land_on_shared_red off (holdShared).
func threeMerging(t *testing.T, held bool) (*sim, EdgeRequest) {
	t.Helper()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 6, "pro")
	s.row("r2", 6, "pro", "heavy")
	ps := []string{"p1", "p2", "p3"}
	for _, p := range ps {
		s.producer(p, "s1", "pro", "a/"+p)
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
	require.Equal(t, ps, req.Cards, "backed up: one group request")
	if held {
		holdShared(s)
	}
	return s, req
}

// A group merge that conflicts on one card's head (live 2026-11-11: 42
// cards, one conflicting file, 42 judgements), with land_on_shared_red
// off: that card alone is the coordinator's, named with its file, and the
// rest of the group is asked again without it, with no answer from the
// coordinator.
func TestTickGroupConflictJudgesOnlyTheCardThatDoesNotMerge(t *testing.T) {
	t.Parallel()
	s, req := threeMerging(t, true)
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "conflict", Head: req.Heads[1],
		Reason: "rung 2; conflicts: test/x_test.go; reason: merge conflict on " + req.Heads[1] + ": test/x_test.go"})
	s.tick()
	require.Empty(t, s.rejected)
	assert.Equal(t, []string{"land.p2.1"}, s.openJudgements("land"), "only the card whose head does not merge")
	q := s.snap().Judgements["land.p2.1"].Question
	assert.Contains(t, q, "card p2 ")
	assert.Contains(t, q, "test/x_test.go")
	assert.Contains(t, q, "p1, p3", "names the cards asked again without it")
	s.run(2)
	again := s.request("land", "s1")
	assert.Equal(t, []string{"p1", "p3"}, again.Cards, "the rest of the group lands without it")
	assert.Empty(t, s.openJudgements("engine"))
}

// A conflict that names no head (GitHub's mergeable state on the group's
// PR), with land_on_shared_red off, is still one judgement per card: the
// edge could not say which (on, the group peels: TestTickGroupRedPeels).
func TestTickConflictWithNoHeadJudgesTheGroup(t *testing.T) {
	t.Parallel()
	s, req := threeMerging(t, true)

	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "conflict", Reason: "GitHub says #1 conflicts with v2"})
	s.tick()
	assert.Equal(t, []string{"land.p1.1", "land.p2.1", "land.p3.1"}, s.openJudgements("land"))
}

// With land_on_shared_red on (the default, 2026-11-15), the card whose
// head does not merge goes back to fix with the conflict named as its
// finding, no judgement opens, and the rest of the group is asked again
// without it.
func TestTickConflictGoesToFix(t *testing.T) {
	t.Parallel()
	s, req := threeMerging(t, false)
	require.True(t, req.SharedRed, "the switch rides on the request")
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "conflict", Head: req.Heads[1],
		Reason: "rung 2; conflicts: test/x_test.go; reason: merge conflict on " + req.Heads[1] + ": test/x_test.go"})
	s.tick()
	require.Empty(t, s.rejected)
	assert.Empty(t, s.openJudgements("land"), "no judgement for a conflict")
	s.requireState("p2", "ready")
	p2 := s.card("p2")
	assert.True(t, p2.Red)
	assert.Contains(t, p2.Finding, "conflict: head "+req.Heads[1])
	assert.Contains(t, p2.Finding, "test/x_test.go")
	s.run(2)
	again := s.request("land", "s1")
	assert.Equal(t, []string{"p1", "p3"}, again.Cards, "the rest of the group lands without it")
	assert.Empty(t, s.openJudgements("engine"))
}

// A one-card landing that GitHub says conflicts names no head: the one
// card is still the one that does not merge, so it goes to fix.
func TestTickOneCardConflictGoesToFix(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	req := toMerging(s)
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "conflict", Reason: "GitHub says #1 conflicts with main"})
	s.tick()
	require.Empty(t, s.rejected)
	assert.Empty(t, s.openJudgements("land"))
	s.requireState("p1", "ready")
	assert.Contains(t, s.card("p1").Finding, "GitHub says #1 conflicts with main")
}

// The policy's land_on_shared_red is what the land request carries: on by
// default (the engine's answer), off when landing(stream) says False.
func TestTickLandRequestCarriesTheSwitch(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	req := toMerging(s)
	assert.True(t, req.SharedRed, "the default lands on the base's own reds")
	o := newOracle(Answers{Version: 1, Landing: map[string]LandingAnswer{"s1": {BackedUp: 2, Flowing: 1, CIWait: 600, HoldSharedRed: true}}}, Engine)
	assert.True(t, o.landing(StreamFacts{Name: "s1"}).HoldSharedRed, "False in the policy holds it")
}
