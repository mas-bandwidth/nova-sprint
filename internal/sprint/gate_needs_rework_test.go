package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reworkByRule is the machine's rule rework (TickRuleRework calls Rework with a
// PerCard carrying the rule): the same function a coordinator's rework uses.
func reworkByRule(w *world, id string) {
	w.t.Helper()
	w.must(Rework(w.s, ReworkReq{
		Sel:     Sel{Only: []string{id}},
		PerCard: map[string]ReworkCard{id: {Fix: "make the test pass", Rule: "failed", Said: "attempt"}},
		Who:     "tick",
	}))
}

// TestARuleReworkWaitsWhenOnlyANonMatchingMemberHasRoom: the rule rework of a
// GATE-NEEDS linux card narrows its pool by the gate needs as the deal does. The
// linux member is up but has no room; the darwin member has room but does not carry
// linux. The card is not dealt to the darwin member: it waits ready, and the darwin
// member holds nothing.
// tla/GateFacts.tla Rework.
func TestARuleReworkWaitsWhenOnlyANonMatchingMemberHasRoom(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1", "m4")
	w.s.MemberCtl("m1").Fields[FieldFacts] = "linux,go"
	w.s.MemberCtl("m4").Fields[FieldFacts] = "darwin,go"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, "m1", wc.Row)
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: "red"}))
	// m1 at its room: two ready cards fill its width-1 room (DealAhead times it).
	putWorkCard(w, "x1", "m1", Ready, 100, nil)
	putWorkCard(w, "x2", "m1", Ready, 101, nil)
	reworkByRule(w, "s1-1")
	require.Nil(t, w.s.Fleet.Card("s1-1.w2"), "no matching member has room: attempt 2 is not dealt")
	assert.Equal(t, 0, w.s.Fleet.Count("m4", Ready), "the darwin member holds nothing")
	assert.Zero(t, w.s.Fleet.Count("m4", Working))
	assert.Empty(t, w.notesOf(NGateNeeds), "a matching member is up: no gate-host judgment")
}

// TestARuleReworkDealsToAMemberWhoseFactsMeetTheGateNeeds: with room on the linux
// member, the rule rework's attempt 2 is dealt to it, never to the darwin member.
// tla/GateFacts.tla Rework.
func TestARuleReworkDealsToAMemberWhoseFactsMeetTheGateNeeds(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1", "m4")
	w.s.MemberCtl("m1").Fields[FieldFacts] = "linux,go"
	w.s.MemberCtl("m4").Fields[FieldFacts] = "darwin,go"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, "m1", wc.Row)
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: "red"}))
	reworkByRule(w, "s1-1")
	wc2 := w.s.Fleet.Card("s1-1.w2")
	require.NotNil(t, wc2, "attempt 2 is dealt")
	assert.Equal(t, "m1", wc2.Row, "attempt 2 is dealt to the member whose facts meet the gate needs, never the darwin member")
}

// TestARuleReworkWithNoMatchingMemberRaisesTheGateNeedsJudgment: no linux member is
// up to take a GATE-NEEDS linux card after its failed finish. The rule rework does
// not deal it to the darwin member; it raises the one gate-host judgment, naming the
// need, once over two ticks, never a silent wait.
// tla/GateFacts.tla Rework.
func TestARuleReworkWithNoMatchingMemberRaisesTheGateNeedsJudgment(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1", "m4")
	w.s.MemberCtl("m1").Fields[FieldFacts] = "linux,go"
	w.s.MemberCtl("m4").Fields[FieldFacts] = "darwin,go"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: "red"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	reworkByRule(w, "s1-1")
	require.Nil(t, w.s.Fleet.Card("s1-1.w2"), "no matching member is up: attempt 2 is not dealt")
	notes := w.notesOf(NGateNeeds)
	require.Len(t, notes, 1, "one gate-host judgment: %+v", w.notes)
	assert.Contains(t, notes[0].What, "GATE-NEEDS linux")
	assert.Equal(t, []string{"s1-1"}, notes[0].Primaries)
	// a second tick's rule rework (the card is ready now, not reworked again) raises no
	// second judgment: the standing cause is judged once, never every tick
	Rework(w.s, ReworkReq{
		Sel:     Sel{Only: []string{"s1-1"}},
		PerCard: map[string]ReworkCard{"s1-1": {Fix: "make the test pass", Rule: "failed", Said: "attempt"}},
		Who:     "tick",
	})
	require.Len(t, w.notesOf(NGateNeeds), 1, "a standing cause is judged once, never every tick")
}

// TestRedoDealsToAMemberWhoseFactsMeetTheGateNeeds holds the conflict redo to
// the same GateHostPool path as an ordinary deal and a review rework.
func TestRedoDealsToAMemberWhoseFactsMeetTheGateNeeds(t *testing.T) {
	t.Parallel()
	w := stoppedForConflict(t)
	w.s.MemberCtl("m1").Fields[FieldFacts] = "darwin,go"
	w.s.MemberCtl("m2").Fields[FieldFacts] = "linux,go"
	w.s.Work.Card("s1-2").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"

	w.must(Redo(w.s, RedoReq{Sel: Sel{IDs: []string{"s1-2"}}, Who: "coordinator"}))
	wc := w.s.Fleet.Card("s1-2.w2")
	require.NotNil(t, wc, "redo deals attempt 2")
	assert.Equal(t, "m2", wc.Row, "redo chooses the member whose facts meet GATE-NEEDS")
}

// TestRedoWithNoMatchingMemberRaisesTheGateNeedsJudgment refuses to deal a
// conflict redo to an incapable host and makes the wait visible once.
func TestRedoWithNoMatchingMemberRaisesTheGateNeedsJudgment(t *testing.T) {
	t.Parallel()
	w := stoppedForConflict(t)
	w.s.MemberCtl("m1").Fields[FieldFacts] = "darwin,go"
	w.s.MemberCtl("m2").Fields[FieldFacts] = "darwin,go"
	w.s.Work.Card("s1-2").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"

	p := w.must(Redo(w.s, RedoReq{Sel: Sel{IDs: []string{"s1-2"}}, Who: "coordinator"}))
	require.Nil(t, w.s.Fleet.Card("s1-2.w2"), "no matching member: redo waits ready")
	require.Equal(t, Ready, w.state("s1-2"))
	require.Len(t, p.Notes, 1)
	assert.Equal(t, NGateNeeds, p.Notes[0].Kind)
	assert.Contains(t, p.Notes[0].What, "GATE-NEEDS linux")
}
