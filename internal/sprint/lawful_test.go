package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// A landing the lifecycle refuses satisfies no need: the needs rule is judged
// against the landings of the units the lifecycle kept.
func TestLawfulRefusedLandingSatisfiesNoNeed(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.admit(AddReq{Stream: "s2", IDs: []string{"b"}, Needs: []string{"s1-1"}})
	s11, b := w.s.Work.Card("s1-1"), w.s.Work.Card("b") // s1-1 is ready
	p := Plan{Units: []Unit{
		{Key: "s1-1", Changes: []Change{change(Work, moveEntry(s11, s11.Row, Landed, nil))}},
		{Key: "b", Changes: []Change{change(Work, moveEntry(b, b.Row, Ready, nil))}},
	}}
	p.on(w.s)
	p = Lawful(p)
	require.Empty(t, p.Units, "a refused landing lent to a waiter: %+v", p)
	require.Len(t, p.Refused, 2, "a refused landing lent to a waiter: %+v", p)
	require.Contains(t, p.Refused[1].Why, "b needs s1-1", "a refused landing lent to a waiter: %+v", p)
}

// ready -> waiting carries its cause (docs/SPEC-SPRINT.md section 3): a
// sentinel inserted, or a named need not landed. Needs that have all landed
// move nothing back to waiting (tla/SprintTables.tla
// ReadyToWaitingOnlyByInsertOrNeeds, witness "needsany").
func TestLawfulReadyToWaitingNeedsANeedNotLanded(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1 and s1-2 ready
	w.place(w.s.Work, "s1-2", "s1", Landed)
	s11 := w.s.Work.Card("s1-1")
	back := func(needs string) Plan {
		p := Plan{Units: []Unit{{Key: "s1-1", Changes: []Change{change(Work, moveEntry(s11, s11.Row, Waiting, map[string]string{"needs": needs}))}}}}
		p.on(w.s)
		return Lawful(p)
	}
	p := back("s1-2")
	require.Empty(t, p.Units, "a need landed moved a ready card back: %+v", p)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "ready -> waiting only")
	w.admit(AddReq{Stream: "s2", IDs: []string{"n"}})
	p = back("s1-2,n")
	require.Len(t, p.Units, 1, "a need not landed moves it back: %+v", p)
	require.Empty(t, p.Refused)
}

// working -> waiting (PR #96's move) is judged by the card's own named needs in
// the pre-state, never by the needs the entry writes: a manifest that writes a
// need not landed cannot move a working card whose needs have all landed
// (tla/SprintTables.tla WorkingToWaitingOnlyOnNeeds, witness "withdrawmet").
// With a named need not landed the move is kept.
func TestLawfulWorkingToWaitingNeedsTheCardsOwnNeedNotLanded(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.admit(AddReq{Stream: "s2", IDs: []string{"n"}}) // n is not landed
	w.place(w.s.Work, "s1-2", "s1", Landed)
	w.place(w.s.Work, "s1-1", "s1", Working)
	s11 := w.s.Work.Card("s1-1")
	back := func() Plan {
		p := Plan{Units: []Unit{{Key: "s1-1", Changes: []Change{change(Work, moveEntry(s11, s11.Row, Waiting, map[string]string{"needs": "n"}))}}}}
		p.on(w.s)
		return Lawful(p)
	}
	s11.Fields["needs"] = "s1-2" // its own need has landed
	p := back()
	require.Empty(t, p.Units, "a written need moved a working card whose needs landed: %+v", p)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "working -> waiting only when the card's named needs have not landed")
	s11.Fields["needs"] = "s1-2,n" // its own need n has not landed
	p = back()
	require.Empty(t, p.Refused, "a working card whose own need has not landed waits again: %+v", p)
	require.Len(t, p.Units, 1)
}

// A work-table move whose expectation names no place is judged from the
// pre-state; one the pre-state does not hold is refused.
func TestLawfulMoveWithoutPlace(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.admit(AddReq{Stream: "s2", IDs: []string{"b"}, Needs: []string{"s1-1"}})
	bare := func(id, row, col string) Unit {
		return Unit{Key: id, Changes: []Change{change(Work, ntable.BatchMemberEntry{ID: id,
			Expect: &ntable.MemberExpect{Revision: "1"}, Move: &ntable.MemberMoveOp{Row: row, Col: col}})}}
	}
	for _, c := range []struct {
		u   Unit
		pre bool
		why string
	}{
		{bare("b", "s2", Ready), true, "b needs s1-1, not landed"},
		{bare("b", "s2", Merging), true, "no move waiting -> merging"},
		{bare("s1-1", "s1", Landed), true, "no move ready -> landed"},
		{bare("ghost", "s2", Ready), true, "names no place"},
		{bare("b", "s2", Ready), false, "names no place"},
		{Unit{Key: "b", Changes: []Change{change(Work, ntable.BatchMemberEntry{ID: "b", Remove: true})}}, false, "names no place"},
	} {
		p := Plan{Units: []Unit{c.u}}
		if c.pre {
			p.on(w.s)
		}
		p = Lawful(p)
		require.Empty(t, p.Units, "%s (pre %v, want %q): %+v", c.u.Key, c.pre, c.why, p)
		require.Len(t, p.Refused, 1, "%s (pre %v, want %q): %+v", c.u.Key, c.pre, c.why, p)
		require.Contains(t, p.Refused[0].Why, c.why, "%s (pre %v, want %q): %+v", c.u.Key, c.pre, c.why, p)
	}
	// A move from a place the pre-state holds, by the lifecycle, is kept.
	p := Plan{Units: []Unit{bare("s1-1", "s1", Working)}}
	p.on(w.s)
	p = Lawful(p)
	require.Len(t, p.Units, 1, "ready -> working without a place: %+v", p)
}
