package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A finish whose report names BLOCKED-BY: is the card saying what it waits for
// (docs/SPEC-CARD-CONTRACT.md, the BLOCKED-BY line; tla/WaitFor.tla Finish/Split).
// The machine reads it: a card that does not need the one it names gains it as a
// need and waits; one that does need it, or whose need would close a cycle, is the
// gate-fix judgment; a name that is no open card is the ordinary HOLD.

func blockedWork(t *testing.T, w *world, id string) *Card {
	t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{id}}}))
	wc := w.s.Fleet.Card(WorkCardID(id, 1))
	require.NotNil(t, wc)
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	return wc
}

func TestHoldBlockedByAnOpenCardBecomesANeed(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	wc := blockedWork(t, w, "s1-1")
	head := strings.Repeat("a", 40)
	report := "Verdict: HOLD\nBLOCKED-BY: s1-2\nthe API s1-2 adds is missing"
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Head: head, Report: report}))

	pr := w.s.Work.Card("s1-1")
	assert.Equal(t, Waiting, pr.Col, "the card waits for the need it named")
	assert.Equal(t, "s1-2", pr.F("needs"), "the named open card is written as a need")
	assert.Equal(t, "1", pr.F(FieldDecidedAttempt), "the attempt counted toward no bound: the decision is at it")
	assert.Equal(t, 1, pr.Int("attempt"), "the attempt itself is untouched")
	assert.Empty(t, pr.F(FieldFailure), "not the brief's failure")
	assert.Equal(t, head, pr.F("head"), "the head is kept for its next attempt's carry")
	assert.Empty(t, openOf(w, NWorkFailed, "s1-1"), "not the worker's failed work")
	assert.Empty(t, openOf(w, NGateFixCycle, "s1-1"))

	notes := w.notesOf(NBlockedBy)
	require.Len(t, notes, 1, "one happened note to the coordinator")
	assert.Equal(t, Happened, notes[0].Kind)
	assert.Contains(t, notes[0].What, "s1-1")
	assert.Contains(t, notes[0].What, "s1-2")
	assert.Contains(t, notes[0].What, "BLOCKED-BY: s1-2")
	w.clean("a BLOCKED-BY need")
}

func TestHoldBlockedByItsDependentIsAGateFixJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	wc := blockedWork(t, w, "s1-1")
	setNeeds(w.s, "s1-2", "s1-1") // s1-2 needs s1-1: finishing s1-1 is the gate fix
	w.s.Work.Card("s1-2").Fields["brief"] = "RESULT: s1-2 sha=abc tier: pro\nDONE-WHEN: go test ./sprint -run TestTheGate"
	w.s.Work.Card("s1-1").Fields["brief"] = "RESULT: s1-1 sha=abc tier: pro\nDONE-WHEN: go test ./sprint -run TestTheGate"
	report := "Verdict: HOLD\nBLOCKED-BY: s1-2"
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: report}))

	pr := w.s.Work.Card("s1-1")
	assert.Empty(t, pr.F("needs"), "no need is written for a card that needs it")
	assert.Equal(t, "1", pr.F(FieldDecidedAttempt), "the attempt counted toward no bound")
	assert.Equal(t, 1, pr.Int("attempt"))
	assert.Empty(t, openOf(w, NWorkFailed, "s1-1"))

	open := openOf(w, NGateFixCycle, "s1-1")
	require.Len(t, open, 1, "one gate-fix judgment")
	n := open[0].Note
	assert.Contains(t, n.Primaries, "s1-1")
	assert.Contains(t, n.Primaries, "s1-2")
	assert.Contains(t, n.What, "split")
	assert.Contains(t, n.What, "TestTheGate", "the shared DONE-WHEN command is named")
	assert.Contains(t, n.Decisions, "rework with a fix")
	assert.Contains(t, n.Decisions, "drop")
	assert.Contains(t, strings.Join(n.Decisions, "\n"), "twin ", "the split command is offered")
}

func TestBlockedByANonCardIsAnOrdinaryHold(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	wc := blockedWork(t, w, "s1-1")
	report := "Verdict: HOLD\nBLOCKED-BY: no-such-card\nthe API is missing"
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: report}))

	pr := w.s.Work.Card("s1-1")
	assert.Equal(t, Review, pr.Col, "today's failed path")
	assert.Equal(t, "failed", pr.F("result"))
	assert.Empty(t, pr.F("needs"))
	require.Len(t, openOf(w, NWorkFailed, "s1-1"), 1, "the ordinary failed-work judgment")
	assert.Empty(t, w.notesOf(NBlockedBy))
	assert.Empty(t, openOf(w, NGateFixCycle, "s1-1"))
}

func TestBlockedByThatWouldCycleIsTheJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	wc := blockedWork(t, w, "s1-1")
	// s1-2 does not need s1-1, but LAND-AFTER s1-1: the need would close a loop.
	w.s.Work.Card("s1-2").Fields[FieldLandAfter] = "s1-1"
	w.s.Work.Card("s1-2").Fields[FieldLandAfterSet] = "true"
	report := "Verdict: HOLD\nBLOCKED-BY: s1-2"
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: report}))

	pr := w.s.Work.Card("s1-1")
	assert.Empty(t, pr.F("needs"), "a need that would close a loop is never written")
	require.Len(t, openOf(w, NGateFixCycle, "s1-1"), 1, "the cycle is the judgment")
	assert.Empty(t, w.notesOf(NBlockedBy))
}
