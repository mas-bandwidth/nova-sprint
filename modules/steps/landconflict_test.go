package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threeMerging is stream s1 with p1, p2 and p3 merging at sha(1..3), and
// the one group request (rung 2) that lands them together.
func threeMerging(t *testing.T) (*sim, EdgeRequest) {
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
	return s, req
}

// A group merge that conflicts on one card's head (live 2026-11-11: 42
// cards, one conflicting file, 42 judgements): that card alone is the
// coordinator's, named with its file, and the rest of the group is asked
// again without it, with no answer from the coordinator.
func TestTickGroupConflictJudgesOnlyTheCardThatDoesNotMerge(t *testing.T) {
	t.Parallel()
	s, req := threeMerging(t)
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
// PR) is still one judgement per card: the edge could not say which.
func TestTickConflictWithNoHeadJudgesTheGroup(t *testing.T) {
	t.Parallel()
	s, req := threeMerging(t)
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "conflict", Reason: "GitHub says #1 conflicts with v2"})
	s.tick()
	assert.Equal(t, []string{"land.p1.1", "land.p2.1", "land.p3.1"}, s.openJudgements("land"))
}
