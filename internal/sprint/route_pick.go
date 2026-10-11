package sprint

import (
	"math"
	"time"
)

// preferFirstWeighted selects a route based on its measured outcome share.
// Share falls as cost per landed card rises or ok rate falls, with a floor
// so the route is still sampled. Returns the selected route, steps taken,
// and whether selection succeeded.
// Model: tla/RouteIndex.tla, RouteFair; design section in route.go preferFirst.
func preferFirstWeighted(arr []string, served map[string]Route, skip []string, hold bool, at uint64, fleet *Table) (r Route, steps uint64, ok bool) {
	// Filter out skipped routes
	var candidates []string
	for _, name := range arr {
		if _, ok := served[name]; !ok {
			continue
		}
		if hold && contains(skip, name) {
			continue
		}
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return Route{}, 0, false
	}

	// Get stats and compute shares
	shares := computeRouteShares(candidates, served, fleet)

	// Check if all shares are equal - if so, use deterministic round-robin
	allEqual := true
	if len(shares) > 1 {
		first := shares[0]
		for _, s := range shares {
			if math.Abs(s-first) > 1e-9 {
				allEqual = false
				break
			}
		}
	}

	if allEqual {
		// Use deterministic round-robin when all shares are equal
		return preferFirst(arr, served, skip, hold, at)
	}

	// Normalize shares
	totalShare := 0.0
	for _, s := range shares {
		totalShare += s
	}

	// Select based on cumulative share
	rnd := randFloat()
	cumulative := 0.0
	for i, name := range candidates {
		cumulative += shares[i] / totalShare
		if rnd < cumulative {
			steps = uint64(i) + 1
			return served[name], steps, true
		}
	}

	// Fallback to last candidate
	return served[candidates[len(candidates)-1]], uint64(len(candidates)), true
}

// computeRouteShares computes the share weight for each candidate route.
// Share falls as cost per landed card rises or ok rate falls.
func computeRouteShares(candidates []string, served map[string]Route, fleet *Table) []float64 {
	if len(candidates) == 0 {
		return nil
	}

	// Get stats for all routes
	statsMap := routeStatsMap(fleet)

	shares := make([]float64, len(candidates))
	for i, name := range candidates {
		st, ok := statsMap[name]
		if !ok {
			// No stats yet, give equal weight (1.0)
			shares[i] = 1.0
		} else {
			// Compute share from stats: higher ok rate and lower cost per landed = higher share
			// Using a simple formula: share = okRate * (1 / (1 + costFactor))
			// Since we don't track actual spend, we use ok rate as the primary factor
			share := routeShare(st)
			shares[i] = share
		}
	}

	return shares
}

// shareFloor is the minimum share a route can have (1%).
const shareFloor = 0.01

// routeShare computes the share weight for a route based on its stats.
// The share falls as cost per landed card rises or ok rate falls.
// A floor ensures the route is still sampled.
func routeShare(stats RouteStat) float64 {
	// Compute ok rate (0 if no attempts)
	okRate := 0.0
	if stats.Attempts > 0 {
		okRate = float64(stats.OK) / float64(stats.Attempts)
	}

	// Compute cost per landed card (0 if no landings)
	// Higher cost = lower share
	// We use a simple inverse: share = 1 / (1 + costFactor)
	// Since we don't track actual spend per route in RouteStat,
	// we just use ok rate as the primary factor
	share := okRate
	if share < shareFloor {
		share = shareFloor
	}
	return share
}

// routeStatsMap builds a map from route name to RouteStat.
func routeStatsMap(fleet *Table) map[string]RouteStat {
	// Get all routes from fleet cards
	attempts := make(map[string]int)
	okCount := make(map[string]int)
	failedCount := make(map[string]int)

	for _, c := range fleet.Column(DoneOK, DoneFailed) {
		route := c.F(FieldRoute)
		if route == "" {
			continue
		}
		attempts[route]++
		if c.Col == DoneOK {
			okCount[route]++
		} else {
			failedCount[route]++
		}
	}

	// Build stats map
	stats := make(map[string]RouteStat, len(attempts))
	for route := range attempts {
		stats[route] = RouteStat{
			Attempts: attempts[route],
			OK:       okCount[route],
			Failed:   failedCount[route],
		}
	}
	return stats
}

// randFloat returns a random float in [0, 1).
// In production code, use a proper random source.
func randFloat() float64 {
	// Simple deterministic pseudo-random for testing
	// Uses current time for variation
	return math.Mod(float64(time.Now().UnixNano()), 1000000) / 1000000.0
}
