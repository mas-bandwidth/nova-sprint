package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noRouteWorld is a world of two members up, the routes given, and one card s1-1 the
// tick's deal holds under "no route serves the tier" for tier.
func noRouteWorld(t *testing.T, tier string, routes []Route, fields map[string]string) *world {
	t.Helper()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2", Width: 2}))
	w.s.Routes = routes
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 1}))
	for k, v := range fields {
		w.s.Work.Card("s1-1").Fields[k] = v
	}
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	require.Nil(t, w.s.Fleet.Card(WorkCardID("s1-1", 1)), "no route serves its tier: dealt nowhere")
	var judged bool
	for _, o := range w.s.Open {
		if o.Note.Type == NNoRoute && o.Note.Stream == TierSubject(tier) {
			judged = true
		}
	}
	require.True(t, judged, "the tier's one judgment holds s1-1")
	return w
}

// answerRules runs the tick's rule part named, with the rules on, as the run loop does.
func (w *world) answerRules(name string) {
	w.t.Helper()
	for _, part := range TickRules {
		if part.Name == name {
			w.part(part.Fn, TickReq{AnswerRules: true})
		}
	}
}

// A card whose tier no route serves is raised to the nearest tier above it that a route
// serves once the tier's judgment is past its deadline, never left to the seat (the owner,
// 2026-10-12: "no route serves the tier" held its cards 31 minutes unanswered): pinned
// there, dealt on it at the next deal, and the seat told the change in a note.
func TestATierNoRouteServesIsRaisedToTheNearestServedTierAtItsDeadline(t *testing.T) {
	t.Parallel()
	w := noRouteWorld(t, cardhdr.RouteFlash, []Route{
		{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true, Deadline: 600},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true, Deadline: 600},
	}, nil)

	// within the deadline: the seat has time
	w.tick(DeadlineJudgment / 2)
	w.answerRules("rule no-route")
	assert.Empty(t, w.s.Work.Card("s1-1").F(FieldTier), "within its deadline the card is not re-tiered")

	// past it: raised to pro, the nearest served tier above flash (not heavy)
	w.tick(DeadlineJudgment/2 + time.Second)
	w.answerRules("rule no-route")
	assert.Equal(t, cardhdr.RoutePro, w.s.Work.Card("s1-1").F(FieldTier), "raised to the nearest served tier above")
	var said []string
	for _, n := range w.notesOf(NRuleAnswered) {
		said = append(said, n.What)
	}
	require.Len(t, said, 1, "the change is named to the seat")
	assert.Contains(t, said[0], "s1-1 re-tiered from flash to pro")
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	wc := w.s.Fleet.Card(WorkCardID("s1-1", 1))
	require.NotNil(t, wc, "dealt at the next deal")
	assert.Equal(t, "pro-a", wc.F(FieldRoute), "on a route of the tier it was raised to")
}

// TestANoRouteServeCardStaysOnItsTier pins that the no-route rule never moves a card down
// a tier: a heavy card no route serves is never re-tiered to flash though a flash route
// serves, and a card the coordinator pinned to a tier stays on that tier though a route
// serves above it. Either way the card stays held for the seat (the alarm names it).
func TestANoRouteServeCardStaysOnItsTier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tier   string
		routes []Route
		fields map[string]string
	}{
		{"TestAHeavyCardNoRouteServesIsNeverReTieredToFlash", cardhdr.RouteHeavy,
			[]Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600}},
			map[string]string{FieldTier: cardhdr.RouteHeavy}},
		{"TestAPinnedCardNoRouteServesStaysOnItsPinnedTier", cardhdr.RouteFlash,
			[]Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true, Deadline: 600}},
			map[string]string{FieldTier: cardhdr.RouteFlash}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := noRouteWorld(t, tc.tier, tc.routes, tc.fields)
			w.tick(DeadlineJudgment + time.Minute)
			w.answerRules("rule no-route")
			assert.Equal(t, tc.tier, w.s.Work.Card("s1-1").F(FieldTier), "the tier never moves down")
			assert.Empty(t, w.notesOf(NRuleAnswered), "left to the seat")
			p, _ := TickDeal(w.s, TickReq{})
			w.must(p)
			assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-1", 1)), "never dealt on a lower tier")
		})
	}
}
