package sprint

import (
	"math/rand"
)

// preferFirstWeighted selects a route based on its measured outcome share.
// Share falls as cost per landed card rises or ok rate falls, with a floor
// so the route is still sampled. Returns the selected route, steps taken,
// and whether selection succeeded.
// Model: tla/RouteIndex.tla, RouteFair; design section in route.go preferFirst.
func preferFirstWeighted(arr []string, served map[string]Route, skip []string, hold bool, at uint64, fleet *Table) (r Route, steps uint64, ok bool) {
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
	if pick, steps, ok := preferFirstWithShares(arr, served, servedFirst, skip, hold, at, fleet); ok {
		return pick, steps, true
	}

	// Second pass: pick from routes without First set
	return preferFirstWithShares(arr, served, servedNonFirst, skip, hold, at, fleet)
}

// preferFirstWithShares is like preferFirst but uses weighted selection based on shares.
// It counts array positions for index advancement, not filtered candidates.
func preferFirstWithShares(arr []string, served map[string]Route, wantFirst func(string) bool, skip []string, hold bool, at uint64, fleet *Table) (r Route, steps uint64, ok bool) {
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

	// Compute shares for candidates
	shares := computeRouteShares(candidates, fleet)

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
func computeRouteShares(candidates []string, fleet *Table) []float64 {
	if len(candidates) == 0 {
		return nil
	}

	// Use existing RouteStats function from route.go
	allStats := RouteStats(nil, fleet)

	shares := make([]float64, len(candidates))
	for i, name := range candidates {
		// Find stats for this route
		var routeStats RouteStat
		found := false
		for _, st := range allStats {
			if st.Route.Name == name {
				routeStats = st
				found = true
				break
			}
		}

		if !found {
			// No stats yet, give equal weight
			shares[i] = 1.0
		} else {
			shares[i] = routeShare(routeStats)
		}
	}

	return shares
}

// shareFloor is the minimum share a route can have (1%).
const shareFloor = 0.01

// routeShare computes the share weight for a route based on its stats.
// The share falls as cost per landed card rises or ok rate falls.
// A floor ensures the route is still sampled.
// Design: from docs/SPEC-SPRINT.md deal section, RouteFair.
func routeShare(stats RouteStat) float64 {
	// Compute ok rate (0 if no attempts)
	okRate := 0.0
	if stats.Attempts > 0 {
		okRate = float64(stats.OK) / float64(stats.Attempts)
	}

	// Compute cost per landed card from the stats
	// Cost per landed = total cost / landed count
	// We'll use the ratio of failed to ok as a proxy since we track ok/failed
	// A route with many failed takes and few ok has high cost per landed
	costFactor := 0.0
	if stats.OK == 0 && stats.Failed > 0 {
		// Zero landings means infinite cost per landed - penalize heavily
		costFactor = 100.0
	} else if stats.OK > 0 {
		// Higher ratio of failed/ok means higher cost per landed
		costFactor = float64(stats.Failed) / float64(stats.OK)
	}

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
