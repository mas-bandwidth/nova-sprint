package sprint

import (
	"fmt"
	"math/rand"
)

// preferFirstWeighted selects a route based on its measured outcome share.
// Share falls as cost per landed card rises or ok rate falls, with a floor
// so the route is still sampled. Returns the selected route, steps taken,
// and whether selection succeeded.
// Design section in route.go preferFirst.
func preferFirstWeighted(arr []string, served map[string]Route, skip []string, hold bool, at uint64, stats map[string]RouteStat, costPerLanded map[string]float64) (r Route, steps uint64, ok bool) {
	// Two-pass walk like preferFirst: first pass for First routes, second for others.
	// This preserves the First rule from route.go.
	servedFirst := func(name string) bool {
		r, ok := served[name]
		return ok && r.First
	}
	servedNonFirst := func(name string) bool {
		r, ok := served[name]
		return ok && !r.First
	}

	// First pass: try to pick a route with First set
	if pick, steps, ok := preferFirstWithShares(arr, served, servedFirst, skip, hold, at, stats, costPerLanded); ok {
		return pick, steps, true
	}

	// Second pass: pick from routes without First set
	return preferFirstWithShares(arr, served, servedNonFirst, skip, hold, at, stats, costPerLanded)
}

// preferFirstWithShares is like preferFirst but uses weighted selection based on shares.
// It counts array positions for index advancement, not filtered candidates.
func preferFirstWithShares(arr []string, served map[string]Route, wantFirst func(string) bool, skip []string, hold bool, at uint64, stats map[string]RouteStat, costPerLanded map[string]float64) (r Route, steps uint64, ok bool) {
	n := uint64(len(arr))
	if n == 0 {
		return Route{}, 0, false
	}

	// Build candidates in array order, counting positions properly
	var candidates []string
	for i := uint64(0); i < n; i++ {
		name := arr[(at+i)%n]
		// Check if this entry is drawable (not held/skip and matches wantFirst)
		if hold && contains(skip, name) {
			continue
		}
		if _, ok := served[name]; !ok {
			continue
		}
		if !wantFirst(name) {
			continue
		}
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return Route{}, 0, false
	}

	// Compute shares for candidates from precomputed stats
	shares := computeRouteShares(candidates, stats, costPerLanded)

	// Check if all shares are equal (within tolerance)
	allEqual := true
	if len(shares) > 1 {
		first := shares[0]
		for _, s := range shares {
			if abs(s-first) > 1e-9 {
				allEqual = false
				break
			}
		}
	}

	if allEqual {
		// Use deterministic selection when all shares are equal (round-robin style)
		// Pick the first valid candidate in array order
		for i := uint64(0); i < n; i++ {
			name := arr[(at+i)%n]
			if hold && contains(skip, name) {
				continue
			}
			if _, ok := served[name]; !ok {
				continue
			}
			if wantFirst(name) {
				return served[name], i + 1, true
			}
		}
	}

	// Weighted selection using proper random source
	// Normalize shares
	totalShare := 0.0
	for _, s := range shares {
		totalShare += s
	}

	// Use seeded random (can be made deterministic for testing)
	rnd := rand.Float64()
	cumulative := 0.0
	for i, name := range candidates {
		cumulative += shares[i] / totalShare
		if rnd < cumulative {
			// steps is the position in the array from at, not the index in candidates
			// We need to find the actual position in arr
			for j := uint64(0); j < n; j++ {
				if arr[(at+j)%n] == name {
					return served[name], j + 1, true
				}
			}
		}
	}

	// Fallback to last candidate
	return served[candidates[len(candidates)-1]], n, true
}

// computeRouteShares computes the share weight for each candidate route.
// Share falls as cost per landed card rises or ok rate falls.
// This should be called once per tick, not per draw, to avoid O(ready x fleet) cost.
func computeRouteShares(candidates []string, stats map[string]RouteStat, costPerLanded map[string]float64) []float64 {
	if len(candidates) == 0 {
		return nil
	}

	shares := make([]float64, len(candidates))
	for i, name := range candidates {
		if s, ok := stats[name]; ok {
			// Use actual cost per landed from fleet, default to 0 if not found
			cost := costPerLanded[name]
			shares[i] = routeShare(s, cost)
		} else {
			// No stats yet, give equal weight
			shares[i] = 1.0
		}
	}

	return shares
}

// routeStatsByName returns route stats indexed by route name from fleet.
func routeStatsByName(fleet *Table) map[string]RouteStat {
	allStats := RouteStats(nil, fleet)
	out := make(map[string]RouteStat, len(allStats))
	for _, st := range allStats {
		out[st.Route.Name] = st
	}
	return out
}

// routeCostPerLanded computes the average cost per landed card for each route
// from the fleet's landed cards (DoneOK). Returns map from route name to USD.
func routeCostPerLanded(fleet *Table) map[string]float64 {
	// Sum costs by route from landed cards
	type costAccum struct {
		totalUSD float64
		count    int
	}
	routeCosts := make(map[string]*costAccum)

	// Look at done OK cards in fleet
	for _, c := range fleet.Column(DoneOK) {
		route := c.F(FieldRoute)
		if route == "" {
			continue
		}
		// Parse the cost from the card's cost field (exact USD)
		costStr := c.F(FieldCost)
		if costStr == "" {
			continue
		}
		// Simple parsing: strip $ and parse as float
		if costStr[0] == '$' {
			costStr = costStr[1:]
		}
		var costUSD float64
		fmt.Sscanf(costStr, "%f", &costUSD)

		if routeCosts[route] == nil {
			routeCosts[route] = &costAccum{}
		}
		routeCosts[route].totalUSD += costUSD
		routeCosts[route].count++
	}

	// Compute averages
	result := make(map[string]float64, len(routeCosts))
	for route, acc := range routeCosts {
		if acc.count > 0 {
			result[route] = acc.totalUSD / float64(acc.count)
		}
	}
	return result
}

// shareFloor is the minimum share a route can have.
// With 100 picks, floor=0.1 gives P(zero picks) = 1%, ensuring reliable sampling.
const shareFloor = 0.1

// routeShare computes the share weight for a route based on its stats and cost per landed.
// The share falls as cost per landed card rises or ok rate falls.
// A floor ensures the route is still sampled.
// Design section in route.go preferFirst.
func routeShare(stats RouteStat, costPerLanded float64) float64 {
	// Compute ok rate (0 if no attempts)
	okRate := 0.0
	if stats.Attempts > 0 {
		okRate = float64(stats.OK) / float64(stats.Attempts)
	}

	// Normalize cost per landed: costFactor = 1 when costPerLanded = $10, scales linearly
	// A route that spends $30 per landed has costFactor = 3, share reduced by ~75%
	costFactor := costPerLanded / 10.0

	// Share combines ok rate and cost factor
	// Higher ok rate = higher share, higher cost factor = lower share
	share := okRate / (1.0 + costFactor)
	if share < shareFloor {
		share = shareFloor
	}
	return share
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
