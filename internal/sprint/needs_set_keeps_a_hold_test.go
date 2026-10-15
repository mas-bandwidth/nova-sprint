package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A card added held waits until the coordinator's release, whatever happens to its
// needs (docs/SPEC-SPRINT.md, needs and holds; tla/Holds.tla HeldMovesOnlyByRelease).
// The state below is the one the five port-fixed cards were in: a held waiting card
// needing a card that has landed (spike) and one that has not (chain).

// heldWorld is a fleet with one member up, a spike landed, a chain waiting, and a card
// held on both.
func heldWorld(t *testing.T) *world {
	t.Helper()
	w := fleetWorld(t, 0, 2, "m1")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"spike"}, Brief: "the spike", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"chain"}, Needs: []string{"spike"}, Brief: "the chain", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"held"}, Needs: []string{"spike", "chain"}, Held: true, Brief: "the held card", Who: "coordinator"}))
	w.place(w.s.Work, "spike", "s1", Landed)
	require.Equal(t, Waiting, w.state("held"))
	require.True(t, IsHeld(w.s.Work.Card("held")))
	require.Equal(t, Waiting, w.state("chain"))
	return w
}

// A needs change never releases a hold: needs set with every new need landed leaves the
// held card waiting, hold stamp, who and why intact, its needs as asked, and the MOVED
// line names no column change. tla/Holds.tla NeedsSetH and its reversed witness
// "needssetheld".
func TestNeedsSetKeepsAHeldCardWaiting(t *testing.T) {
	t.Parallel()
	w := heldWorld(t)
	before := w.s.Work.Card("held")
	stamped := before.F(FieldHeld)

	p := NeedsSet(w.s, NeedsSetReq{ID: "held", Needs: []string{"spike"}, Who: "coordinator"})
	require.Empty(t, p.Refused)
	for _, u := range p.Units {
		if u.Key == "held" {
			assert.NotContains(t, u.Moved, "->", "no column change is named: %q", u.Moved)
		}
	}
	w.must(p)

	got := w.s.Work.Card("held")
	assert.Equal(t, Waiting, got.Col, "a held card stays waiting")
	assert.True(t, IsHeld(got))
	assert.Equal(t, stamped, got.F(FieldHeld), "the hold stamp stays")
	assert.Equal(t, "spike", got.F("needs"), "the needs are written as asked")
	assert.Contains(t, got.F("brief"), "DEPENDS-ON: spike")

	// the tick's resolve leaves it too
	w.must(Resolve(w.s, ResolveReq{Who: "coordinator"}))
	assert.Equal(t, Waiting, w.state("held"))

	// clearing the needs is a needs change as well
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "held", Clear: true, Who: "coordinator"}))
	assert.Equal(t, Waiting, w.state("held"))
	assert.True(t, IsHeld(w.s.Work.Card("held")))

	// the release is the way out, and it moves it to ready
	w.must(Release(w.s, ReleaseReq{IDs: []string{"held"}, Reason: "test", Coordinator: "coordinator", Who: "coordinator"}))
	assert.Equal(t, Ready, w.state("held"))
	assert.False(t, IsHeld(w.s.Work.Card("held")))
}

// A held card whose needs change keeps waiting when the need set names a need not landed
// too, and a card that is not held still follows the lifecycle (waiting -> ready).
func TestNeedsSetStillMovesACardThatIsNotHeld(t *testing.T) {
	t.Parallel()
	w := heldWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"plain"}, Needs: []string{"spike", "chain"}, Brief: "plain", Who: "coordinator"}))
	require.Equal(t, Waiting, w.state("plain"))
	p := w.must(NeedsSet(w.s, NeedsSetReq{ID: "plain", Needs: []string{"spike"}, Who: "coordinator"}))
	assert.Equal(t, Ready, w.state("plain"))
	assert.Contains(t, p.Units[0].Moved, "waiting -> ready")
}

// A brief edit whose DEPENDS-ON line alone changes (taken whole, --needs-from-brief) is a
// needs change too: the held card keeps its column and its hold. The edit keeps a held
// waiting card where it is by the DEPENDS-ON path (briefDepends writes the needs and
// moves nothing); this pins it.
func TestABriefEditKeepsAHeldCardWaiting(t *testing.T) {
	t.Parallel()
	w := heldWorld(t)
	stamped := w.s.Work.Card("held").F(FieldHeld)
	old := w.s.Work.Card("held").F("brief")
	brief := old
	if i := strings.Index(old, "DEPENDS-ON:"); i >= 0 {
		j := strings.Index(old[i:], "\n")
		if j < 0 {
			j = len(old) - i
		}
		brief = old[:i] + "DEPENDS-ON: spike" + old[i+j:]
	} else {
		brief = "DEPENDS-ON: spike\n" + old
	}
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "held", Brief: brief, Needs: []string{"spike"}}}, NeedsFromBrief: true, Who: "coordinator"})
	require.Empty(t, p.Refused)
	for _, u := range p.Units {
		if u.Key == "held" {
			assert.NotContains(t, u.Moved, "-> ready", "%q", u.Moved)
		}
	}
	w.must(p)
	got := w.s.Work.Card("held")
	assert.Equal(t, Waiting, got.Col)
	assert.Equal(t, stamped, got.F(FieldHeld))
	assert.Equal(t, "spike", got.F("needs"))
}

// The deal never hands out a card that carries a hold, so a held card that reached ready
// by any path still waits there: the ready closure of dealPlan refuses it, naming the
// release verb, and after the release it is dealt. tla/Holds.tla DealH and its reversed
// witness "dealheld".
func TestTheDealNeverHandsOutAHeldCard(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 2, "m1")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"held"}, Brief: "the held card", Who: "coordinator"}))
	require.Equal(t, Ready, w.state("held"))
	// the state the bug left: ready, hold stamp still on it
	c := w.s.Work.Card("held")
	c.Fields[FieldHeld] = stamp(t0)
	c.Fields["held_by"] = "coordinator"
	c.Fields["held_reason"] = "test"
	c.Rev++
	require.True(t, IsHeld(c))

	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"held"}}})
	require.Empty(t, p.Units, "a held card is not dealt: %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "nova-sprint release held", "the reason names the release verb")

	// the same by a tick
	tp := Deal(w.s, DealReq{Sel: Sel{Limit: 5}})
	assert.Empty(t, tp.Units, "%+v", tp)
	assert.Equal(t, Ready, w.state("held"))

	// the release clears the hold, and the deal then hands it out
	w.must(Release(w.s, ReleaseReq{IDs: []string{"held"}, Reason: "test", Coordinator: "coordinator", Who: "coordinator"}))
	require.False(t, IsHeld(w.s.Work.Card("held")))
	p = Deal(w.s, DealReq{Sel: Sel{IDs: []string{"held"}}})
	require.Empty(t, p.Refused, "%+v", p)
	require.NotEmpty(t, p.Units)
	w.must(p)
	assert.Equal(t, Working, w.state("held"))
}
