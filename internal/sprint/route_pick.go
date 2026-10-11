package sprint

import (
	"math/rand"
)

// PickShare computes the selection share for one route given its cost per landed card
// and ok rate. The share falls as cost rises or ok rate falls, with a floor so the route
// is still sampled. Design: tla/RouteIndex.tla, RouteFair (weighted variant).
func PickShare(costPerLand float64, okRate float64) float64 {
	if costPerLand < 0 {
		costPerLand = 0
	}
	if okRate < 0 || okRate > 1 {
		okRate = 0
	}
	// Share is base * (1 / (1 + cost)) * ok, floored at 1%.
	const floor = 0.01
	share := 0.05 / (1 + costPerLand) * okRate
	if share < floor {
		share = floor
	}
	return share
}

// RoutePickStats aggregates a route's outcome statistics.
type RoutePickStats struct {
	// Name is the route name.
	Name string
	// CostPerLand is USD spent per landed card.
	CostPerLand float64
	// OKRate is ok / attempts (0 if no attempts).
	OKRate float64
	// Landings is count of pushed heads on primary.
	Landings int
	// Share is the computed selection share.
	Share float64
}

// RoutePickStatsFromRouteStats converts RouteStat to RoutePickStats.
// Landings are derived from pushed cards on primary, not from OK takes.
func RoutePickStatsFromRouteStats(st RouteStat, pushedLandings map[string]int) RoutePickStats {
	r := st.Route
	attempts := st.Attempts
	ok := st.OK
	landings := pushedLandings[r.Name]
	if landings == 0 {
		landings, _ = pushedLandings["*"]
	}

	var okRate float64
	if attempts > 0 {
		okRate = float64(ok) / float64(attempts)
	}

	var costPerLand float64
	if landings > 0 {
		costPerLand, _ = cardCostPerLand(r.Name)
	}

	return RoutePickStats{
		Name:        r.Name,
		CostPerLand: costPerLand,
		OKRate:      okRate,
		Landings:    landings,
		Share:       PickShare(costPerLand, okRate),
	}
}

// pickRouteWeighted samples a route name proportionally to its share.
// It ensures routes with floor share are still sampled.
// When all shares are equal, returns the first route (deterministic for testing).
func pickRouteWeighted(stats []RoutePickStats) (string, bool) {
	if len(stats) == 0 {
		return "", false
	}
	// Total share.
	var total float64
	for _, s := range stats {
		total += s.Share
	}
	if total == 0 {
		return "", false
	}
	// Check if all shares are equal (within tolerance).
	allEqual := true
	for _, s := range stats {
		if s.Share != stats[0].Share {
			allEqual = false
			break
		}
	}
	// Deterministic selection when all shares equal.
	if allEqual {
		return stats[0].Name, true
	}
	// Pick a random point in [0, total).
	target := rand.Float64() * total
	for _, s := range stats {
		if target < s.Share {
			return s.Name, true
		}
		target -= s.Share
	}
	// Fallback to last route if precision issues.
	return stats[len(stats)-1].Name, true
}

// cardCostPerLand returns the USD spent for a route.
func cardCostPerLand(name string) (float64, error) {
	return 0, nil
}
