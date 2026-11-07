package steps

import (
	"context"
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toMerging takes p1 through work and one ok read to merging at sha(1).
func toMerging(t *testing.T) *sim {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(2)
	s.requireState("p1", "merging")
	return s
}

// answer applies a coordinator's answer to judgement id (with ev, if any).
func (s *sim) answer(id, choice string, ev card.Event, cardID string) {
	s.t.Helper()
	s.fold()
	op, why := Verb(s.v, s.inputs(), Request{Actor: "coord", Coordinators: []string{"coord"}, Judgement: id, Answer: choice, Card: cardID, Event: ev})
	require.Nil(s.t, why)
	rc, err := s.f.Apply(context.Background(), op)
	require.NoError(s.t, err)
	require.True(s.t, rc.Accepted, rc.Reason)
}

// TestLandRung1IsOneCardWithItsBranch: flowing, one merging producer is
// requested alone, carrying its work's branch (the PR's head).
func TestLandRung1IsOneCardWithItsBranch(t *testing.T) {
	s := toMerging(t)
	req := s.request("land", "s1")
	assert.Equal(t, []string{"p1"}, req.Cards)
	assert.Equal(t, []string{sha(1)}, req.Heads)
	require.NotEmpty(t, req.Branch)
	assert.Equal(t, s.card("p1.w1").Branch, req.Branch)
}

// TestLandRestartRerequests: a restart forgets what was in flight; the next
// tick requests the merging producer again, by the same id (INTENT 16; the
// night's defect 51).
func TestLandRestartRerequests(t *testing.T) {
	s := toMerging(t)
	first := s.request("land", "s1")
	s.inflight = nil // the run restarted
	s.tick()
	again := s.request("land", "s1")
	assert.Equal(t, first.ID, again.ID)
}

// TestLandEscalateOpensOneJudgementAndStops: an escalate opens one land
// judgement on the card with the edge's facts, the card stays merging, and
// nothing is requested for it until the coordinator answers; retry asks
// again.
func TestLandEscalateOpensOneJudgementAndStops(t *testing.T) {
	s := toMerging(t)
	req := s.request("land", "s1")
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "escalate", Facts: "PR x is red at h: failing unit; first failing lines: --- FAIL: TestBudgets"})
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "merging")
	open := s.openJudgements("land")
	require.Equal(t, []string{"land.p1.1"}, open)
	j := s.snap().Judgements["land.p1.1"]
	assert.Contains(t, j.Question, "--- FAIL: TestBudgets")
	assert.Contains(t, j.Command, "land | fix | retry")
	assert.True(t, j.Deadline.IsZero(), "a land judgement waits for the coordinator")
	s.run(3)
	assert.False(t, slices.ContainsFunc(s.inflight, func(r EdgeRequest) bool { return r.Kind == "land" }), "no landing while the judgement is open")
	assert.Equal(t, []string{"land.p1.1"}, s.openJudgements("land"), "one judgement, not one per tick")
	s.answer("land.p1.1", "retry", nil, "")
	s.tick()
	assert.Equal(t, req.ID, s.request("land", "s1").ID)
}

// TestLandFixAnswerSendsTheCardBack: fix on a land judgement is LandFix:
// back to ready with the finding, a bounce, and the next attempt continues
// from the head.
func TestLandFixAnswerSendsTheCardBack(t *testing.T) {
	s := toMerging(t)
	req := s.request("land", "s1")
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "escalate", Facts: "red"})
	s.tick()
	s.answer("land.p1.1", "fix", card.LandFix{Head: sha(1), Finding: "bus is over its line budget"}, "p1")
	s.fold()
	s.requireState("p1", "ready")
	assert.Equal(t, "bus is over its line budget", s.card("p1").Finding)
	assert.Equal(t, 1, s.card("p1").Bounces)
	assert.Empty(t, s.openJudgements("land"))
	s.tick()
	s.requireState("p1", "working")
	assert.Equal(t, sha(1), s.card("p1.w2").Prior)
}

// TestLandRung2GroupRedIsOneStreamJudgement: backed up, the stream's
// eligible producers are one request; an escalate on it is one judgement
// about the stream (no bisection, no blame) and stops the stream.
func TestLandRung2GroupRedIsOneStreamJudgement(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 4, "pro")
	s.row("r2", 2, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "b/y")
	s.run(2)
	s.take("p1.w1")
	s.take("p2.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.report("p2.w1", "ok", sha(2), "done")
	s.tick()
	s.take("p1.r1.1")
	s.take("p2.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.report("p2.r1.1", "ok", sha(2), "fine")
	s.inflight = nil
	s.run(2)
	req := s.request("land", "s1")
	require.Len(t, req.Cards, 2, "backed up: one group")
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "escalate", Facts: "group red"})
	s.tick()
	assert.Equal(t, []string{"land.s1.1"}, s.openJudgements("land"))
	s.requireState("p1", "merging")
	s.requireState("p2", "merging")
	s.run(2)
	assert.False(t, slices.ContainsFunc(s.inflight, func(r EdgeRequest) bool { return r.Kind == "land" }))
}

// TestLandedAndFixAreTheCoordinators: a row that is not a coordinator may
// not raise Landed or LandFix through a verb.
func TestLandedAndFixAreTheCoordinators(t *testing.T) {
	s := toMerging(t)
	for _, ev := range []card.Event{card.Landed{Head: sha(1)}, card.LandFix{Head: sha(1), Finding: "x"}} {
		_, why := Verb(s.v, s.inputs(), Request{Actor: "r1", Coordinators: []string{"coord"}, Card: "p1", Event: ev})
		require.NotNil(t, why)
		assert.Contains(t, why.Reason, "coordinator")
	}
}
