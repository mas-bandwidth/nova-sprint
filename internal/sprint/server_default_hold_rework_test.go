package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldAtTheEnvCap is the 2026-10-17 shape of fuzz-viewrowswire-prefix-oracle (judgment
// w-c2d3004dd8fac384, alias j25715, 15 cards): attempt 1's child ended on the member's
// environment three times (exit 137), the bound judgment went unanswered, and the
// server-default answer held the ready primary in place. The clock is 1000 s.
func heldAtTheEnvCap(t *testing.T) *world {
	t.Helper()
	w := friendWorld(t, "c: a machine's card\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
	w.s.Now = time.Unix(1000, 0).UTC()
	pr := w.s.Work.Placed("s1-1")
	pr.Fields["attempt"] = "1"
	w.s.Fleet.Put(&Card{ID: "s1-1.w1", Row: "m1", Col: Withdrawn, Score: pr.Score, Rev: 1, Fields: map[string]string{
		"kind": "work", "primary": "s1-1", "stream": "s1", "attempt": "1", "gen": "6", "member": "m1",
		FieldEnvExitHistory: "m1=137,m2=137,m1=137"}})
	for k, v := range map[string]string{FieldHeld: stamp(w.s.Now), "held_by": ServerDefaultActor,
		"held_reason": "j-env: s1-1.w1: attempt 1's child ended on the member's environment 3 times", "held_judgment": "j-env",
		"decision_held_from": Ready} {
		pr.Fields[k] = v
	}
	pr.Rev++
	w.s.Work.Put(pr)
	require.NotNil(t, AtEnvExitBound(w.s, pr), "the fixture: s1-1 is at its environment-exit cap")
	require.True(t, IsHeld(pr), "the fixture: s1-1 is held by server-default")
	return w
}

// The deadlock: rework (the answer the bound judgment asks for) dealt attempt 2 and
// left the server-default hold on the primary, so every finish of attempt 2 was
// refused "release the server-default hold before moving its primary" (38 times on
// .w2), and release refused the working primary ("is working, not a sentinel
// waiting to be released"). The rework is the hold's answer: it clears the hold, and
// a primary already working under a hold is released in place.
func TestAReworkedServerDefaultHoldNeverBlocksItsFinish(t *testing.T) {
	t.Parallel()
	t.Run("the rework clears the hold and the finish moves", func(t *testing.T) {
		w := heldAtTheEnvCap(t)
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "run only the named tests", Who: w.s.Coordinator}))
		wc := WorkCardID("s1-1", 2)
		dealt := w.s.Fleet.Placed(wc)
		require.NotNil(t, dealt, "the rework deals attempt 2")
		assert.False(t, IsHeld(w.s.Work.Placed("s1-1")), "the rework answered the hold: it is cleared")
		w.must(Take(w.s, TakeReq{As: dealt.Row, Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc)}))
		p := Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, As: dealt.Row, Gens: gensOf(w.s, wc), Head: "abc"})
		assert.Empty(t, p.Refused, "the finish of attempt 2 moves its primary")
	})
	t.Run("release clears a hold on a working primary in place", func(t *testing.T) {
		w := heldAtTheEnvCap(t)
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "run only the named tests", Who: w.s.Coordinator}))
		pr := w.s.Work.Placed("s1-1")
		if !IsHeld(pr) { // after the fix the rework cleared it: put the stuck state back by hand
			for k, v := range map[string]string{FieldHeld: stamp(w.s.Now), "held_by": ServerDefaultActor, "held_reason": "j-env", "held_judgment": "j-env", "decision_held_from": Ready} {
				pr.Fields[k] = v
			}
			pr.Rev++
			w.s.Work.Put(pr)
		}
		require.Equal(t, Working, pr.Col)
		p := Release(w.s, ReleaseReq{IDs: []string{"s1-1"}, Reason: "the rework answered it", Coordinator: w.s.Coordinator, Who: w.s.Coordinator})
		require.Empty(t, p.Refused, "release applies to a working primary held by server-default")
		w.must(p)
		pr = w.s.Work.Placed("s1-1")
		assert.False(t, IsHeld(pr), "the hold is cleared")
		assert.Equal(t, Working, pr.Col, "in place: the work goes on")
	})
}
