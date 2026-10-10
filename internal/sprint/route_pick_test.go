package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoutePickStatsFromRouteStats checks that stats are built from route stats.
func TestRoutePickStatsFromRouteStats(t *testing.T) {
	t.Parallel()
	rs := []RouteStat{
		{Route: Route{Name: "a", USD: "0.10"}, OK: 10, Failed: 2},
		{Route: Route{Name: "b", USD: "0.05"}, OK: 5, Failed: 10},
	}
	stats := RoutePickStatsFromRouteStats(rs)
	require.Len(t, stats, 2)
	assert.Equal(t, "0.10", stats["a"].USD)
	assert.Equal(t, 10, stats["a"].OK)
	assert.Equal(t, 2, stats["a"].Failed)
	assert.Equal(t, "0.05", stats["b"].USD)
	assert.Equal(t, 5, stats["b"].OK)
	assert.Equal(t, 10, stats["b"].Failed)
}

// TestRoutePickStatsPicksShareTwoRoutesEqualPrice checks that two routes with
// equal price but different ok rates get different shares.
// The route with zero landings (and thus lower ok rate) gets smaller share but not zero.
func TestRoutePickStatsPicksShareTwoRoutesEqualPrice(t *testing.T) {
	t.Parallel()
	// Route a: 20 takes, 20 ok, 10 landings (good)
	// Route b: 20 takes, 0 ok, 0 landings (bad)
	// Both with same USD = 0.10
	stats := map[string]*RoutePickStats{
		"a": {RouteName: "a", USD: "0.10", OK: 20, Failed: 0, Landings: 10},
		"b": {RouteName: "b", USD: "0.10", OK: 0, Failed: 20, Landings: 0},
	}
	all := statsValues(stats)
	shareA := stats["a"].PickShare(all)
	shareB := stats["b"].PickShare(all)
	// Share of a should be greater than share of b
	assert.Greater(t, shareA, shareB, "route a with better ok rate should have larger share")
	// Share of b should not be zero (floor)
	assert.Greater(t, shareB, 0.0, "route b should still have non-zero share due to floor")
	// Shares should sum to approximately 1
	sum := shareA + shareB
	assert.InDelta(t, 1.0, sum, 0.01, "shares should sum to 1")
}

// TestRoutePickStatsPicksShareZeroLandings checks that a route with zero landings
// over 20 takes gets smaller share of next picks but not zero.
func TestRoutePickStatsPicksShareZeroLandings(t *testing.T) {
	t.Parallel()
	// Two routes with equal price
	// Route a: 20 takes, all ok (good)
	// Route b: 20 takes, all failed (bad)
	stats := map[string]*RoutePickStats{
		"a": {RouteName: "a", USD: "0.10", OK: 20, Failed: 0},
		"b": {RouteName: "b", USD: "0.10", OK: 0, Failed: 20},
	}
	all := statsValues(stats)
	shareA := stats["a"].PickShare(all)
	shareB := stats["b"].PickShare(all)
	// Route with zero ok (and thus effectively zero landings) gets smaller share
	assert.Greater(t, shareA, shareB)
	// But not zero due to floor
	assert.Greater(t, shareB, 0.0)
	// Floor should be at least 0.01 / nRoutes = 0.01 / 2 = 0.005
	assert.GreaterOrEqual(t, shareB, 0.005, "share should respect floor")
}

// TestRoutePickStatsPicksShareCostPerLanded checks that route share falls as cost
// per landed card rises.
func TestRoutePickStatsPicksShareCostPerLanded(t *testing.T) {
	t.Parallel()
	// Route a: cheap route 0.05, 90% ok rate
	// Route b: expensive route 0.20, 50% ok rate
	stats := map[string]*RoutePickStats{
		"a": {RouteName: "a", USD: "0.05", OK: 90, Failed: 10},
		"b": {RouteName: "b", USD: "0.20", OK: 50, Failed: 50},
	}
	all := statsValues(stats)
	shareA := stats["a"].PickShare(all)
	shareB := stats["b"].PickShare(all)
	// Cheaper route with better ok rate should have larger share
	assert.Greater(t, shareA, shareB)
}

// TestRoutePickStatsPicksShareFloor checks that all routes get at least the floor share.
func TestRoutePickStatsPicksShareFloor(t *testing.T) {
	t.Parallel()
	// Create many routes with very different stats
	stats := map[string]*RoutePickStats{
		"a": {RouteName: "a", USD: "0.01", OK: 100, Failed: 0},
		"b": {RouteName: "b", USD: "0.50", OK: 1, Failed: 100},
		"c": {RouteName: "c", USD: "0.30", OK: 0, Failed: 50},
	}
	all := statsValues(stats)
	for name, s := range stats {
		share := s.PickShare(all)
		// Floor is 0.01 / nRoutes = 0.01 / 3 ≈ 0.0033
		floor := 0.01 / float64(len(stats))
		assert.GreaterOrEqual(t, share, floor, "route %s should have at least floor share", name)
	}
}

// TestPickRouteWeighted checks that pickRouteWeighted selects a route.
func TestPickRouteWeighted(t *testing.T) {
	t.Parallel()
	served := map[string]Route{
		"a": {Name: "a", Enabled: true},
		"b": {Name: "b", Enabled: true},
	}
	stats := map[string]*RoutePickStats{
		"a": {RouteName: "a", OK: 10, Failed: 0},
		"b": {RouteName: "b", OK: 5, Failed: 5},
	}
	name, ok := pickRouteWeighted(served, stats, 0)
	assert.True(t, ok)
	assert.NotEmpty(t, name)
}

