package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Rule (b) of every-need-names-a-live-card-checked-every-second: the run loop
// checks every unlanded card's needs, in every state, and a need naming a card
// that is absent or off the table raises one NNeedsGone judgment at once,
// naming the card, its state, the missing need and the two answers.
func TestTickNeedsJudgesAGoneNeedInEveryUnlandedState(t *testing.T) {
	t.Parallel()
	// waiting is the resolve part's own judgment (NBlocked/NMissingNeed), so the
	// needs part judges every other unlanded state and never doubles waiting.
	for _, st := range []State{Ready, Working, Review, Merging} {
		t.Run(string(st), func(t *testing.T) {
			t.Parallel()
			w := setup(t, 0)
			w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"need"}}))
			w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Needs: []string{"need"}}))
			w.seedDroppedNeed("need")
			if st != Waiting {
				c := w.s.Work.Card("dep")
				c.Col = st
				w.s.Work.cells = nil
			}
			p, _ := TickNeeds(w.s, TickReq{})
			require.Len(t, p.Notes, 1, "notes: %+v", p.Notes)
			n := p.Notes[0]
			require.Equal(t, NNeedsGone, n.Type, "note: %+v", n)
			require.Contains(t, n.Primaries, "dep", "note: %+v", n)
			require.Contains(t, n.What, "dep", "note names the card: %+v", n)
			require.Contains(t, n.What, string(st), "note names the state: %+v", n)
			require.Contains(t, n.What, "need", "note names the missing need: %+v", n)
			require.ElementsMatch(t, []string{"drop"}, n.Decisions, "note offers the drop answer: %+v", n)
		})
	}
}

// Rule (a): drop of a card an unlanded card needs is refused naming the
// dependent, and nothing moves; --cascade is the way through.
func TestDropRefusesACardAnUnlandedCardNeeds(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"need"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"dep"}, Needs: []string{"need"}}))
	p := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"need"}}, Reason: "replaced"})
	require.Empty(t, p.Units, "nothing moves: %+v", p)
	require.Len(t, p.Refused, 1, "refused: %+v", p)
	require.Contains(t, p.Refused[0].Why, "need", "names the dependent")
	require.Contains(t, p.Refused[0].Why, "dep", "names the dependent")
}

// Rule (a): --waive removes the need from each dependent.
func TestDropWithWaiveLeavesNoStrandedNeed(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"need"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"dep"}, Needs: []string{"need"}}))
	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"need"}}, Reason: "replaced", Waive: true}))
	require.Nil(t, w.s.Work.Placed("need"), "the card is off the table")
	require.Empty(t, Split(w.s.Work.Card("dep").F("needs")), "no stranded need on dep")
}

// Rule (c): accept refuses a card whose need has not landed.
func TestAcceptRefusesACardWhoseNeedHasNotLanded(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"need"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"dep"}, Needs: []string{"need"}}))
	c := w.s.Work.Card("dep")
	c.Col = Review
	w.s.Work.cells = nil
	p := Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"dep"}}})
	require.Empty(t, p.Units, "nothing moves: %+v", p)
	require.Len(t, p.Refused, 1, "refused: %+v", p)
	require.Contains(t, p.Refused[0].Why, "not landed", "refused for the unlanded need: %+v", p.Refused)
}

func TestTickNeedsHoldsAndClears(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"need"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Needs: []string{"need"}}))
	w.seedDroppedNeed("need")
	c := w.s.Work.Card("dep")
	c.Col = Review
	w.s.Work.cells = nil
	p, _ := TickNeeds(w.s, TickReq{})
	require.Len(t, p.Notes, 1, "the first judgment: %+v", p.Notes)
	w.note(p.Notes...)
	p, _ = TickNeeds(w.s, TickReq{})
	require.Empty(t, p.Notes, "no second judgment while one is open: %+v", p.Notes)
	// the coordinator sets the need away: the judgment closes
	dep := w.s.Work.Placed("dep")
	dep.Fields["needs"] = ""
	delete(dep.Fields, "needs")
	delete(dep.Fields, "outcome")
	w.s.Work.cells = nil
	need := w.s.Work.Card("need")
	need.Row, need.Col = "s1", Landed
	need.Rev++
	w.s.Work.Put(need)
	w.s.Work.cells = nil
	p, _ = TickNeeds(w.s, TickReq{})
	require.Empty(t, p.Notes, "no new judgment: %+v", p.Notes)
	require.Len(t, p.Closes, 1, "the judgment closes: %+v", p.Closes)
}
