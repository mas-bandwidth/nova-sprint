package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnsweredJudgementStaysQuiet (2026-11-10, seen live: a stall answered
// retry reopened seconds later, again and again, waking the coordinator
// every couple of minutes). A watch judgement (its one choice is retry)
// answered while its cause still holds does not reopen until the judgement
// timeout has passed since the answer; then it reopens saying it still
// holds after the answer, as base-red does.
func TestAnsweredJudgementStaysQuiet(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 8, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a.go")
	s.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Needs: []string{"p1"}})
	s.run(4)
	require.Equal(t, []string{"idle.fleet.1"}, s.openJudgements("idle"))

	s.must("judge", store.AnswerJudgement{ID: "idle.fleet.1", Answer: "retry", Actor: "rowan"})
	answered := s.snap().Judgements["idle.fleet.1"]
	s.run(3)
	require.Empty(t, s.openJudgements("idle"), "answered with its cause still there: quiet for the judgement timeout, not reopened at once")

	s.now = answered.ClosedAt.Add(2 * time.Hour)
	s.tick()
	require.Equal(t, []string{"idle.fleet.2"}, s.openJudgements("idle"))
	q := s.snap().Judgements["idle.fleet.2"].Question
	assert.True(t, strings.HasPrefix(q, "STILL HOLDS 2h0m"), q)
}

// admissionSim is one stream s1 whose producer p1 fails admission (the
// cause is Inputs.Unadmitted, so a test turns it on and off).
func admissionSim(t *testing.T) *sim {
	s := newSim(t)
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a.go")
	s.unadmitted = map[string]string{"p1": "it touches a path the base no longer has"}
	return s
}

// TestAnsweredQuietEndsWhenTheCauseClears (the second cold read of #793):
// the quiet belongs to one unbroken stretch of the cause. Answered, then a
// tick that sees the cause not holding ends it, so the cause coming back
// inside the judgement timeout opens at once, as a new judgement with no
// STILL HOLDS: it is news, not the same thing still there.
func TestAnsweredQuietEndsWhenTheCauseClears(t *testing.T) {
	t.Parallel()
	s := admissionSim(t)
	s.run(2)
	require.Equal(t, []string{"admission.s1.1"}, s.openJudgements("admission"))
	s.must("judge", store.AnswerJudgement{ID: "admission.s1.1", Answer: "retry", Actor: "rowan"})
	s.run(2)
	require.Empty(t, s.openJudgements("admission"), "answered, the cause unbroken: quiet")

	why := s.unadmitted
	s.unadmitted = nil
	s.run(1)
	s.unadmitted = why
	s.run(1)
	require.Equal(t, []string{"admission.s1.2"}, s.openJudgements("admission"), "the cause cleared and came back inside the quiet: pushed at once")
	q := s.snap().Judgements["admission.s1.2"].Question
	assert.True(t, strings.HasPrefix(q, "stream s1: "), "a new stretch of the cause says nothing about the answer: %s", q)
}

// TestStillHoldsOnlyWhenTheCauseNeverBroke: STILL HOLDS after the answer is
// said only when the cause held at every tick since the answer. Answered,
// cleared, and back much later is a plain judgement.
func TestStillHoldsOnlyWhenTheCauseNeverBroke(t *testing.T) {
	t.Parallel()
	s := admissionSim(t)
	s.run(2)
	s.must("judge", store.AnswerJudgement{ID: "admission.s1.1", Answer: "retry", Actor: "rowan"})
	answered := s.snap().Judgements["admission.s1.1"]
	why := s.unadmitted
	s.unadmitted = nil
	s.run(2)
	s.now = answered.ClosedAt.Add(3 * time.Hour)
	s.unadmitted = why
	s.run(1)
	require.Equal(t, []string{"admission.s1.2"}, s.openJudgements("admission"))
	q := s.snap().Judgements["admission.s1.2"].Question
	assert.False(t, strings.HasPrefix(q, "STILL HOLDS"), q)
}
