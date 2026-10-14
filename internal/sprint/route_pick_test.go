package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// TestAZeroLandingRouteGetsAFloorShareOfTheDeal drives the deal, never the
// helper: two flash routes at equal price, route A with 20 ended takes and $20
// spent but zero landings, route B with 20 takes and 10 landings. A deal of 100
// ready cards must give A fewer than B and more than zero (the floor keeps it
// sampled), and a rebalance draw must see the same shares. The draws are
// weighted by routeShare (route_pick.go) with the draw's source seeded
// (routeRand, a seedable *rand.Rand; the process-global rand.Seed is a no-op on
// this tree's Go), so the counts are pinned.
func TestAZeroLandingRouteGetsAFloorShareOfTheDeal(t *testing.T) {
	routeRand.Seed(1)

	w := newWorld(t, "reader-a")
	s := w.s
	s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "a", Enabled: true},
		{Name: "flash-b", Tier: cardhdr.RouteFlash, Provider: "q", Model: "b", Enabled: true},
	}
	s.Tiers = map[string][]string{cardhdr.RouteFlash: {"flash-a", "flash-b"}}
	w.setWidth("m1", 500)
	w.must(FleetStep(s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.setWidth("m2", 500)
	w.must(FleetStep(s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))

	seedOutcomes(s)

	cards := make([]CardAdd, 100)
	for i := range cards {
		cards[i] = CardAdd{ID: fmt.Sprintf("c%03d", i), Brief: fmt.Sprintf("c: the work tier: flash\nThe task of card %d.\n", i)}
	}
	w.must(Add(s, AddReq{Stream: "s1", Cards: cards}))
	require.Equal(t, 100, len(s.Work.Column(Ready)), "100 ready flash primaries")

	plan, _ := TickDeal(s, TickReq{})
	require.Empty(t, plan.Refused, "every ready card is dealt")
	w.do(plan)

	a, b := routeCounts(s.Fleet)
	t.Logf("DEAL-ROUTE-SHARE flash-a=%d flash-b=%d", a, b)
	require.Equal(t, 100, a+b, "all 100 ready cards are dealt")
	require.Greater(t, b, a, "the landing route B gets more of the deal than the zero-landing route A")
	require.Greater(t, a, 0, "the zero-landing route A keeps a floor share of the deal")

	// a rebalance draw (a different caller of routeOf/drawRoute) reads the same
	// precomputed shares: with a full friend and an idle member, the rebalance
	// moves the friend's excess ready cards to the member and draws their routes
	// by the same weights.
	w = newWorld(t)
	s = w.s
	s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "a", Enabled: true},
		{Name: "flash-b", Tier: cardhdr.RouteFlash, Provider: "q", Model: "b", Enabled: true},
	}
	s.Tiers = map[string][]string{cardhdr.RouteFlash: {"flash-a", "flash-b"}}
	w.setWidth("m1", 100)
	w.must(FleetStep(s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	seedOutcomes(s)
	s.Work.SetRows([]string{"s1"})
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}
	rbPlace(w, "s1-w1", cardhdr.RouteFlash, FriendRow("amy"), Working) // amy's one lane works: no free half
	for i := 0; i < 40; i++ {
		rbPlace(w, fmt.Sprintf("s1-q%02d", i), cardhdr.RouteFlash, FriendRow("amy"), Ready)
	}
	w.must(Rebalance(s, []FriendSeat{amy}, "machine"))

	a, b = routeCounts(s.Fleet)
	t.Logf("REBALANCE-ROUTE-SHARE flash-a=%d flash-b=%d", a, b)
	require.Greater(t, b, a, "the rebalance draw weights the landing route B above the zero-landing route A")
	require.Greater(t, a, 0, "the zero-landing route A keeps a floor share of the rebalance draw")
}

// seedOutcomes puts the two routes' ended takes on the fleet: route A 20 takes,
// $20 charged, no landing; route B 20 takes, $20 charged, 10 landings.
func seedOutcomes(s *Snapshot) {
	for i := 0; i < 20; i++ {
		s.Fleet.Put(&Card{ID: fmt.Sprintf("a-fail-%02d", i), Row: "m1", Col: DoneFailed, Fields: map[string]string{
			FieldRoute: "flash-a", "ok": "no", "taken": stamp(t0), "finished": stamp(t0),
			FieldCostTotal: "records=1 charged_usd=1.00",
		}})
	}
	for i := 0; i < 10; i++ {
		s.Fleet.Put(&Card{ID: fmt.Sprintf("b-ok-%02d", i), Row: "m1", Col: DoneOK, Fields: map[string]string{
			FieldRoute: "flash-b", "ok": "yes", "taken": stamp(t0), "finished": stamp(t0),
			FieldCostTotal: "records=1 charged_usd=1.00",
		}})
	}
	for i := 0; i < 10; i++ {
		s.Fleet.Put(&Card{ID: fmt.Sprintf("b-fail-%02d", i), Row: "m1", Col: DoneFailed, Fields: map[string]string{
			FieldRoute: "flash-b", "ok": "no", "taken": stamp(t0), "finished": stamp(t0),
			FieldCostTotal: "records=1 charged_usd=1.00",
		}})
	}
}

// routeCounts counts the work cards dealt on each flash route.
func routeCounts(fleet *Table) (a, b int) {
	for _, c := range fleet.Cards() {
		if c.F("kind") != "work" {
			continue
		}
		switch c.F(FieldRoute) {
		case "flash-a":
			a++
		case "flash-b":
			b++
		}
	}
	return a, b
}
