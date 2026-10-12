package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A need a working card gained after it was dealt is legal until that attempt ends:
// rule 11 holds only for a card dealt after the need was recorded, so it leaves the
// needs gained while working (FieldWaitsAfter, the brief verb's record of them) out
// of its count; the finish moves the card working -> waiting when the attempt ends
// (docs/SPEC-SPRINT.md section 9, rule 11; steps_edit.go briefInPlace, steps_work.go
// finishPlan). The brief verb names the state on the card ("working, waits for
// <need> after this attempt"), so the two agree.
func TestARule11LeavesAWorkingCardsGainedNeedOut(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1 and s1-2, both ready
	w.must(Deal(w.s, DealReq{Sel: Sel{Only: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the fixture: s1-1 is dealt")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row}))
	require.Equal(t, Working, wc.Col, "the fixture: taken")
	// the taken card gains a need not landed (s1-2 is ready, not landed)
	setNeeds(w.s, "s1-1", "s1-2")
	// without the marker, rule 11 flags the state: the need was recorded after the deal
	v := Check(w.s, nil)
	require.Len(t, v, 1, "check: %v", v)
	require.Equal(t, 11, v[0].Rule, "check: %v", v)
	// the brief verb records the need it gained while working; rule 11 leaves it out
	w.s.Work.Card("s1-1").Fields[FieldWaitsAfter] = "s1-2"
	w.clean("a need gained while working is legal until the attempt ends")

	// its attempt ends: the finish moves it working -> waiting for the need
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: wc.Row, Gens: gensOf(w.s, "s1-1.w1")}))
	require.Equal(t, Waiting, w.s.StateOf("s1-1"), "the card waits for the need once its attempt ends")
	w.clean("after the finish, working -> waiting")
}
