package sprint

import (
	"testing"
)

func TestPickShare(t *testing.T) {
	// Two routes with equal price, one with zero landings over 20 takes.
	// The one with zero landings should get a smaller share but not zero.

	// Route A: 20 attempts, 0 OK, 0 landings, $0 cost
	statsA := RouteStat{
		Route:    Route{Name: "route-a", Enabled: true},
		Attempts: 20,
		OK:       0,
	}
	// Route B: 20 attempts, 10 OK, 5 landings, $100 cost
	statsB := RouteStat{
		Route:    Route{Name: "route-b", Enabled: true},
		Attempts: 20,
		OK:       10,
	}

	// Both have zero cost per land (no landings)
	pushedLandings := map[string]int{
		"route-a": 0,
		"route-b": 5,
	}

	statsA2 := RoutePickStatsFromRouteStats(statsA, pushedLandings)
	statsB2 := RoutePickStatsFromRouteStats(statsB, pushedLandings)

	// Route A should have lower share due to zero OK rate
	if statsA2.Share >= statsB2.Share {
		t.Errorf("routeA share %f should be < routeB share %f", statsA2.Share, statsB2.Share)
	}

	// Both should have floor share at minimum
	const floor = 0.01
	if statsA2.Share < floor {
		t.Errorf("routeA share %f should be >= floor %f", statsA2.Share, floor)
	}
	if statsB2.Share < floor {
		t.Errorf("routeB share %f should be >= floor %f", statsB2.Share, floor)
	}
}

func TestPickShareHighCost(t *testing.T) {
	// High cost per land should reduce share
	share := PickShare(10, 0.8) // $10 per land, 80% ok rate
	const floor = 0.01
	if share < floor {
		t.Errorf("share %f should be >= floor %f", share, floor)
	}
	shareLow := PickShare(0, 0.8) // $0 per land, same ok rate
	if share >= shareLow {
		t.Errorf("high cost share %f should be < low cost share %f", share, shareLow)
	}
}

func TestPickShareZeroOK(t *testing.T) {
	// Zero OK rate should result in floor share
	share := PickShare(0, 0)
	const floor = 0.01
	if share != floor {
		t.Errorf("share with zero OK rate %f should be floor %f", share, floor)
	}
}

func TestPickRouteWeighted(t *testing.T) {
	stats := []RoutePickStats{
		{Name: "a", Share: 0.5},
		{Name: "b", Share: 0.3},
		{Name: "c", Share: 0.2},
	}

	name, ok := pickRouteWeighted(stats)
	if !ok {
		t.Fatal("expected true")
	}
	if name == "" {
		t.Fatal("expected non-empty name")
	}
}

func TestPickRouteWeightedEmpty(t *testing.T) {
	name, ok := pickRouteWeighted(nil)
	if ok {
		t.Error("expected false")
	}
	if name != "" {
		t.Error("expected empty name")
	}
}

func TestPickRouteWeightedFloorSampled(t *testing.T) {
	// Routes with floor share should still be sampled
	stats := []RoutePickStats{
		{Name: "a", Share: 0.5},
		{Name: "b", Share: 0.01}, // floor
		{Name: "c", Share: 0.01}, // floor
	}

	// Run multiple times to check floor routes are sampled
	sampled := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		name, ok := pickRouteWeighted(stats)
		if ok {
			sampled[name] = true
		}
	}

	if !sampled["b"] || !sampled["c"] {
		t.Errorf("floor routes should be sampled: a=%v b=%v c=%v",
			sampled["a"], sampled["b"], sampled["c"])
	}
}
