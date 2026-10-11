package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief edit records the needs its DEPENDS-ON names, and a ready card takes
// no need that has not landed: it moves ready -> waiting in the same unit. The
// deal refuses a card with an unlanded need however it was placed, and the
// tick's invariant names it (docs/SPEC-SPRINT.md, needs and the deal).

// needsWorld is stream a with a-1 ready and no needs, ready to be named.
func needsWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	return w
}

// (i) a ready card whose brief is replaced by one whose DEPENDS-ON names a card
// not landed ends with needs recorded and in waiting, in the same unit: here the
// DEPENDS-ON is not the only change, so the ordinary path takes it.
func TestABriefEditRecordsANeedNotLandedAndReadyWaits(t *testing.T) {
	t.Parallel()
	w := needsWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Brief: "two", Who: "coordinator"}))
	require.Equal(t, Ready, w.s.Work.Placed("a-2").Col, "a-2 starts ready")

	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "two, reworded\nDEPENDS-ON: a-1", Needs: []string{"a-1"}}}, Who: "coordinator"}))

	c := w.s.Work.Placed("a-2")
	require.Equal(t, Waiting, c.Col, "a ready card that gains a need not landed waits")
	assert.Equal(t, "a-1", c.F("needs"), "the edit recorded the needs its DEPENDS-ON names")
	assert.Equal(t, Ready, w.s.Work.Placed("a-1").Col, "a-1 has not landed")
}

// (ii) a card in waiting or ready whose brief edit names needs all landed stays
// or becomes ready, with the needs recorded.
func TestABriefEditNamingLandedNeedsLeavesTheCardReady(t *testing.T) {
	t.Parallel()
	w := needsWorld(t)
	w.place(w.s.Work, "a-1", "a", Landed)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-3"}, Brief: "three", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Needs: []string{"a-3"}, Brief: "two", Who: "coordinator"}))
	require.Equal(t, Waiting, w.s.Work.Placed("a-2").Col, "a-2 starts waiting on a-3")

	// the edit re-points a-2 at a-1, which has landed: the resolve makes it ready
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "two\nDEPENDS-ON: a-1", Needs: []string{"a-1"}}}, Who: "coordinator"}))
	w.must(Resolve(w.s, ResolveReq{Who: "coordinator"}))
	c := w.s.Work.Placed("a-2")
	assert.Equal(t, Ready, c.Col, "a waiting card whose needs are all landed becomes ready")
	assert.Equal(t, "a-1", c.F("needs"))

	// a ready card whose edit names a landed need stays ready
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-4"}, Brief: "four", Who: "coordinator"}))
	require.Equal(t, Ready, w.s.Work.Placed("a-4").Col)
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-4", Brief: "four\nDEPENDS-ON: a-1", Needs: []string{"a-1"}}}, Who: "coordinator"}))
	c = w.s.Work.Placed("a-4")
	assert.Equal(t, Ready, c.Col, "a ready card whose new needs have landed stays ready")
	assert.Equal(t, "a-1", c.F("needs"))
}

// (iii) a ready card with a need not landed, built directly in the store as a
// corrupt row would be, is never dealt by dealPlan, and the tick raises one
// judgment naming the card and its unlanded needs.
func TestAReadyCardWithAnUnlandedNeedIsNeverDealt(t *testing.T) {
	t.Parallel()
	w := needsWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 4}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Needs: []string{"a-1"}, Brief: "two", Who: "coordinator"}))
	// the corrupt row: ready with a recorded need that has not landed
	w.place(w.s.Work, "a-2", "a", Ready)
	require.Equal(t, Ready, w.s.Work.Placed("a-2").Col)
	require.Equal(t, "a-1", w.s.Work.Placed("a-2").F("needs"))

	p := Deal(w.s, DealReq{Sel: Sel{Only: []string{"a-2"}}, Who: "coordinator"})
	require.Empty(t, p.Units, "a ready card with an unlanded need is not dealt")
	require.Len(t, p.Refused, 1, "the deal refuses it")
	assert.Contains(t, p.Refused[0].Why, "not landed")

	tp, _ := TickCheck(w.s, TickReq{})
	found := false
	for _, n := range tp.Notes {
		if n.Type == NInvariant && contains(n.Primaries, "a-2") && strings.Contains(n.What, "a-1") {
			found = true
		}
	}
	assert.True(t, found, "the tick raises one judgment naming a-2 and its unlanded need a-1: %+v", tp.Notes)
}

// (v) a brief edit that drops its DEPENDS-ON clears the card's needs, rather
// than keeping the stale ones: the ordinary path records the needs its brief
// names, and a brief that names none leaves the card with none.
func TestABriefEditDroppingANeedClearsIt(t *testing.T) {
	t.Parallel()
	w := needsWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Needs: []string{"a-1"}, Brief: "two", Who: "coordinator"}))
	require.Equal(t, Waiting, w.s.Work.Placed("a-2").Col, "a-2 starts waiting on a-1")
	require.Equal(t, "a-1", w.s.Work.Placed("a-2").F("needs"))

	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "two, reworded", Needs: nil}}, Who: "coordinator"}))

	assert.Equal(t, "", w.s.Work.Placed("a-2").F("needs"), "the edit dropped its DEPENDS-ON: the needs are cleared, not kept stale")
	w.place(w.s.Work, "a-1", "a", Landed)
	w.must(Resolve(w.s, ResolveReq{Who: "coordinator"}))
	assert.Equal(t, Ready, w.s.Work.Placed("a-2").Col, "with no needs left, the card becomes ready")
}

// (iv) the same edit with no DEPENDS-ON change leaves needs as they were.
func TestABriefEditWithoutANeedsChangeLeavesThem(t *testing.T) {
	t.Parallel()
	w := needsWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Needs: []string{"a-1"}, Brief: "two", Who: "coordinator"}))
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "two, reworded\nDEPENDS-ON: a-1", Needs: []string{"a-1"}}}, Who: "coordinator"}))
	c := w.s.Work.Placed("a-2")
	assert.Equal(t, "a-1", c.F("needs"), "the needs did not change")
	assert.Equal(t, Waiting, c.Col, "the card did not move")
}
