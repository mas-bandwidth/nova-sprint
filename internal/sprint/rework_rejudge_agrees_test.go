package sprint

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reworkWorkEntries is the live rework of the held primary s1-1 (heldAtTheEnvCap, the
// clock at 1000 s) and its work-table changes: the entries a repair replays through
// the store's rejudge (store/engine.go) as sprint.Rejudge(pre, "rework", changes).
func reworkWorkEntries(t *testing.T) (*world, Plan, []Change) {
	t.Helper()
	w := heldAtTheEnvCap(t)
	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "run only the named tests", Who: w.s.Coordinator})
	require.Empty(t, p.Refused, "the live rework of the held primary is applied")
	var changes []Change
	for _, u := range p.Units {
		for _, c := range u.Changes {
			if c.Table == Work {
				changes = append(changes, c)
			}
		}
	}
	require.Len(t, changes, 1, "the rework writes one work-table entry, the primary's")
	e := changes[0].Entry
	require.Equal(t, "s1-1", e.ID)
	require.NotNil(t, e.Move, "the fixture: the rework deals the next attempt at once")
	require.Equal(t, Working, e.Move.Col, "the fixture: the rework moves the primary to working")
	return w, p, changes
}

// serverDefaultHoldFields are the five fields of a server-default hold.
var serverDefaultHoldFields = []string{FieldHeld, "held_by", "held_reason", "held_judgment", "decision_held_from"}

// A rework of a primary the server default holds in place clears the hold
// (v17-rework-clears-the-server-default-hold). The repair path replays the same
// entries through Rejudge with the verb "rework", against the pre-state: it must
// admit exactly what the live rework applied, and the live rework's plan must be
// judged by the same hold guard (tla/SprintTables.tla ReworkHeldInPlace,
// DecisionReleaseStep; reversed witness "reworkkeepshold").
func TestAReworkAndItsRepairAgreeOnAServerDefaultHold(t *testing.T) {
	t.Parallel()
	t.Run("a live rework and a repair reach the same state", func(t *testing.T) {
		t.Parallel()
		w, p, changes := reworkWorkEntries(t)
		assert.Empty(t, Rejudge(w.s, "rework", changes), "the repair of the rework's entries refuses none of them")
		w.must(p)
		pr := w.s.Work.Placed("s1-1")
		require.NotNil(t, pr)
		assert.Equal(t, Working, pr.Col, "the rework dealt the next attempt")
		for _, f := range serverDefaultHoldFields {
			assert.Empty(t, pr.F(f), "the rework cleared %s", f)
		}
		assert.False(t, IsHeld(pr), "the rework answered the hold")
	})
	t.Run("a repair of a rework refused nothing the live rework allowed", func(t *testing.T) {
		t.Parallel()
		live, p, changes := reworkWorkEntries(t)
		live.must(p)
		// the repair reads the same pre-state, built the same way, and applies the
		// entries one by one, each its own unit, as Rejudge judged them
		repair := heldAtTheEnvCap(t)
		require.Empty(t, Rejudge(repair.s, "rework", changes), "the repair refuses none of the rework's entries")
		var replay Plan
		for _, c := range changes {
			replay.Units = append(replay.Units, Unit{Key: c.Entry.ID, Changes: []Change{c}})
		}
		repair.must(replay)
		want, got := live.s.Work.Placed("s1-1"), repair.s.Work.Placed("s1-1")
		require.NotNil(t, want)
		require.NotNil(t, got)
		assert.Equal(t, want.Col, got.Col, "the repair places the primary where the live rework did")
		for _, f := range append(slices.Clone(serverDefaultHoldFields), "released", "released_by", "release_reason", "attempt") {
			assert.Equal(t, want.F(f), got.F(f), "the repair and the live rework agree on %s", f)
		}
		assert.Equal(t, IsHeld(want), IsHeld(got))
	})
	t.Run("the guard is only for a rework", func(t *testing.T) {
		t.Parallel()
		w, _, changes := reworkWorkEntries(t)
		for _, verb := range []string{"finish", ""} {
			refused := Rejudge(w.s, verb, changes)
			require.Len(t, refused, 1, "verb %q: the same entry is refused", verb)
			assert.Equal(t, "s1-1", refused[0].Key)
			assert.Equal(t, "release the server-default hold before moving its primary", refused[0].Why, "verb %q", verb)
		}
	})
	t.Run("a rework that keeps the hold is refused", func(t *testing.T) {
		t.Parallel()
		w, _, changes := reworkWorkEntries(t)
		c := changes[0]
		e := c.Entry
		require.Contains(t, e.Unset, "held_by", "the fixture: the rework unsets held_by")
		e.Unset = slices.DeleteFunc(slices.Clone(e.Unset), func(f string) bool { return f == "held_by" })
		e.Set = maps.Clone(e.Set)
		c.Entry = e
		refused := Rejudge(w.s, "rework", []Change{c})
		require.Len(t, refused, 1, "a rework entry that moves the primary and keeps held_by is refused")
		assert.Equal(t, "s1-1", refused[0].Key)
		assert.Equal(t, "release the server-default hold before moving its primary", refused[0].Why)
		// and one that rewrites a hold field to another value, kept in place
		e = changes[0].Entry
		e.Move = nil
		e.Unset = slices.DeleteFunc(slices.Clone(e.Unset), func(f string) bool { return f == "held_reason" })
		e.Set = maps.Clone(e.Set)
		e.Set["held_reason"] = "another reason"
		c.Entry = e
		refused = Rejudge(w.s, "rework", []Change{c})
		require.Len(t, refused, 1, "a rework entry that changes a hold field to another value is refused")
		assert.Equal(t, "only release may change server-default hold provenance", refused[0].Why)
	})
}
