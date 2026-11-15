package steps

import (
	"fmt"
	"strings"
	"testing"

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
	for _, outcome := range []string{"ci_red", "conflict"} {
		s, req := threeMerging(t, false)
		s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: outcome, Reason: "failing unit"})
		p := s.tick()
		require.Empty(t, s.rejected, outcome)
		assert.Empty(t, s.openJudgements("land"), "%s: no judgement for a group's own red", outcome)
		es := landEdges(p)
		require.Len(t, es, 1, outcome)
		assert.Equal(t, []string{"p1"}, es[0].Cards, "%s: the first card alone", outcome)
		assert.True(t, es[0].SharedRed, outcome)
		assert.ElementsMatch(t, []string{"sprint/s1/p2", "sprint/s1/p3"}, es[0].Quiet, outcome)
		for _, c := range []string{"p1", "p2", "p3"} {
			s.requireState(c, "merging")
		}
		s.run(2)
		assert.Equal(t, []string{"p1"}, s.request("land", "s1").Cards, outcome)
		assert.Empty(t, s.openJudgements("engine"), outcome)
	}
}

// TestTickOwnRedGoesToFix: a card asked alone whose landing is red on
// checks the base does not share goes straight back to fix with the
// failing checks as its finding, and no judgement opens.
func TestTickOwnRedGoesToFix(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	first := toMerging(s)
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "ci_red", Reason: "rung 1; failing unit; first failing lines: --- FAIL: TestX"})
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
