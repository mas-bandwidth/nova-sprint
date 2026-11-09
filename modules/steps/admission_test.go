package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnadmittedNeverDealt (item 4, 2026-11-08: 45 waiting cards failed
// admission after their stream's base moved, and only check run by hand
// showed it): a producer layer 6's re-proof refuses at its stream's base is
// never dealt, nor is a blocker preempted for, nor judged undealt; its
// stream has one admission judgement naming the cards and the first
// refusal's fix; once the cards pass again the judgement clears and the
// card is dealt.
func TestUnadmittedNeverDealt(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.unadmitted = map[string]string{
		"p1": "card p1: PATHS a/x: neither it nor its directory a/ is there: correct the path, then add the card again",
		"p2": "card p2: PATHS a/y: neither it nor its directory a/ is there: correct the path, then add the card again",
	}
	for range 4 {
		s.tick()
	}
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	s.requireState("p2", "ready")
	assert.NotContains(t, s.snap().Cards, "p1.w1", "a card failing admission is never dealt")
	assert.Empty(t, s.openJudgements("undealt"), "a card failing admission is not an engine defect")
	js := s.openJudgements("admission")
	require.Len(t, js, 1, "one judgement per stream")
	j := s.snap().Judgements[js[0]]
	assert.Equal(t, "s1", j.About)
	assert.Contains(t, j.Question, "p1, p2")
	assert.Contains(t, j.Question, "correct the path")

	s.unadmitted = nil
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	assert.Empty(t, s.openJudgements("admission"), "the cards pass again: cleared")
	s.requireState("p1.w1", "working")
}

// TestUnadmittedBlockerNotPreempted: a blocker failing admission preempts
// nothing (TestTickPreemption's world, the blocker refused).
func TestUnadmittedBlockerNotPreempted(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(2)
	s.take("p1.w1")
	s.take("p2.w1")
	s.producer("b", "s1", "pro", "a/z")
	s.must("priority", store.SetCardPriority{Card: "b", Priority: "blocker"})
	s.unadmitted = map[string]string{"b": "card b: PATHS a/z: gone: fix"}
	s.tick()
	s.requireState("b", "ready")
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	s.requireState("p2.w1", "working")
	s.requireState("b", "ready")
}
