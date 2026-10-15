package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// An unserved tier falls to the strongest served tier (docs/SPEC-SPRINT.md section 5,
// "An unserved tier falls to the strongest served tier"; unserved_tier.go): the sprint's
// setting nova-sprint set --unserved-tier-falls-to gives every card whose brief tier no
// route and no up friend serves the strongest served tier at or below the setting, written
// as a field of its own, so the deal can run it where something can take it; when the brief
// tier is served again the fallback is cleared on every card not yet taken, and a card the
// seat pinned is never touched.
func TestAnUnservedTierFallsToTheStrongestServedTier(t *testing.T) {
	t.Parallel()
	frontierCard := func(id string, fields map[string]string) *Card {
		f := map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "head": "head",
			"brief": "c: the work (s1) tier: frontier\n\nThe task.\n"}
		for k, v := range fields {
			f[k] = v
		}
		return &Card{ID: id, Row: "s1", Col: Ready, Score: 1, Rev: 1, Fields: f}
	}
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.s.Work.SetRows([]string{"s1"})
	w.s.Work.Put(frontierCard("s1-1", nil))
	w.s.Work.Put(frontierCard("s1-2", nil))
	// a card the seat pinned by hand: tier --set / brief --tier (FieldTier): never touched
	w.s.Work.Put(frontierCard("s1-3", map[string]string{FieldTier: cardhdr.RouteFrontier}))
	// a flash route and a heavy route, no frontier route: frontier is unserved, heavy is the
	// strongest served tier at or below the setting
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true},
		{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true},
	}
	w.must(Set(w.s, SetReq{UnservedTierFallsTo: "heavy", Who: Coordinator}))

	tick := func(seats []FriendSeat) {
		t.Helper()
		p, _ := TickUnservedFallback(w.s, TickReq{Friends: seats})
		w.must(p)
		q, _ := TickDeal(w.s, TickReq{Friends: seats})
		w.must(q)
	}

	// phase 1: frontier is unserved and nothing serves it, so both frontier cards fall to
	// heavy and are dealt on the heavy route, with the note naming the unserved tier
	tick(nil)
	for _, id := range []string{"s1-1", "s1-2"} {
		wc := w.s.Fleet.Card(w.s.Work.Card(id).F("work"))
		require.NotNil(t, wc, "%s is dealt", id)
		assert.Equal(t, "heavy-a", wc.F(FieldRoute), "%s falls to the heavy route", id)
		assert.Equal(t, "heavy", w.s.Work.Card(id).F(FieldFallbackTier), "%s records its fallback tier", id)
		assert.Equal(t, "heavy", w.s.Work.Card(id).F(FieldTierNow), "%s is dealt on heavy", id)
	}
	require.Len(t, w.notesOf(NUnservedFallback), 2, "one note per unserved card")
	for _, n := range w.notesOf(NUnservedFallback) {
		assert.Contains(t, n.What, "tier frontier is unserved", "the note names the unserved tier: %s", n.What)
		assert.Contains(t, n.What, "falls to heavy", "the note names the fallback: %s", n.What)
	}

	// the seat's pin is untouched: no fallback, no heavy deal, still frontier
	pinned := w.s.Work.Card("s1-3")
	assert.Empty(t, pinned.F(FieldFallbackTier), "a seat-pinned card gets no fallback")
	assert.Equal(t, cardhdr.RouteFrontier, pinned.F(FieldTier), "its pin stands")
	assert.Equal(t, Ready, pinned.Col, "the pinned card is not dealt on the fallback")
	assert.Nil(t, w.s.Fleet.Card(w.s.Work.Card("s1-3").F("work")), "no work card for the pinned card")

	// phase 2: a friend up serving frontier comes up, so the brief tier is served again: the
	// next tick clears the fallback on the untaken cards and they go back to frontier
	tick([]FriendSeat{frontierFay(Up)})
	for _, id := range []string{"s1-1", "s1-2"} {
		c := w.s.Work.Card(id)
		assert.Empty(t, c.F(FieldFallbackTier), "%s's fallback is cleared", id)
		assert.Equal(t, cardhdr.RouteFrontier, w.s.DealTier(c), "%s is back on its own tier", id)
		wc := w.s.Fleet.Card(w.s.Work.Card(id).F("work"))
		require.NotNil(t, wc, "%s is dealt again", id)
		assert.Equal(t, FriendRow("fay"), wc.Row, "%s goes to the frontier friend", id)
		assert.Equal(t, cardhdr.RouteFrontier, DealtTier(wc, c), "%s's packet carries frontier", id)
	}
	assert.Empty(t, w.s.Work.Card("s1-3").F(FieldFallbackTier), "the seat-pinned card is still untouched")
	assert.Equal(t, cardhdr.RouteFrontier, w.s.Work.Card("s1-3").F(FieldTier), "its pin stands")
}
