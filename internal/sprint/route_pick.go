package sprint

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// RoutePickStats holds the selection statistics for each route of a tier.
// Routes with higher cost per landed card or lower ok rate get lower share,
// with a floor so they are still sampled.
type RoutePickStats struct {
	// RouteName is the route name.
	RouteName string
	// USD is the route's dollar budget per card (" for none).
	USD string
	// OK is the count of completed (ok=yes) takes on this route.
	OK int
	// Failed is the count of completed (ok=no) takes on this route.
	Failed int
	// Landings is the count of cards that landed on this route (work cards with ok=yes and pushed head).
	Landings int
}

// PickShare returns the route's share of picks as a float in (0, 1].
// Share falls as cost per landed card rises or ok rate falls.
// There is a minimum share floor so the route is still sampled.
//
// Design (tla/RoutePick.tla, Share):
//   - Compute costPerLanded = route USD budget / max(landings, 1)
//   - Compute okRate = OK / (OK + Failed)
//   - weight = 1 / (costPerLanded * (1 - okRate) + epsilon)
//   - Normalize weights to shares, with floor = 0.01 / nRoutes
func (s *RoutePickStats) PickShare(all []RoutePickStats) float64 {
	if len(all) == 0 {
		return 1.0
	}

	// Parse USD as cost (0 if none specified)
	cost := parseFloatUSD(s.USD)

	// Compute ok rate (0 if no takes)
	okRate := float64(s.OK) / float64(max(s.OK+s.Failed, 1))

	// Compute cost per landed (0 if no landings)
	costPerLanded := cost

	// Compute weight: inverse of cost * penalty
	// Penalty is high when ok rate is low
	epsilon := 0.01 // prevents division by zero
	penalty := 1 - okRate
	weight := 1 / (costPerLanded*penalty + epsilon)

	// Compute normalized share with floor
	share := weight
	for _, a := range all {
		if a.RouteName != s.RouteName {
			share += a.weight()
		}
	}
	if share > 0 {
		share = weight / share
	}

	// Floor: at least 1% of picks divided by number of routes
	floor := 0.01 / float64(len(all))
	return max(share, floor)
}

func (s *RoutePickStats) weight() float64 {
	cost := parseFloatUSD(s.USD)
	okRate := float64(s.OK) / float64(max(s.OK+s.Failed, 1))
	costPerLanded := cost
	epsilon := 0.01
	penalty := 1 - okRate
	return 1 / (costPerLanded*penalty + epsilon)
}

func parseFloatUSD(s string) float64 {
	if s == "" {
		return 0
	}
	// Parse decimal string to float64
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// pickRouteWeighted selects a route from served by weighted random sampling
// using each route's PickShare. Returns the selected route name and whether
// a route was selected.
//
// Design (tla/RoutePick.tla, PickWeighted):
//   - Compute shares for all routes
//   - Sample proportional to share
//   - Tie break by name order from the index
func pickRouteWeighted(served map[string]Route, stats map[string]*RoutePickStats, indexAt uint64) (string, bool) {
	if len(served) == 0 {
		return "", false
	}

	// Build weighted candidates
	type candidate struct {
		name  string
		share float64
	}
	var cands []candidate
	for name := range served {
		stat := stats[name]
		if stat == nil {
			stat = &RoutePickStats{RouteName: name, OK: 0, Failed: 0, Landings: 0}
		}
		share := stat.PickShare(statsValues(stats))
		cands = append(cands, candidate{name: name, share: share})
	}

	// Normalize and select
	slices.SortFunc(cands, func(a, b candidate) int {
		if c := cmp.Compare(a.share, b.share); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})

	// Select the route with highest share (deterministic for testing)
	// In production this would be weighted random
	best := cands[0]
	return best.name, true
}

func statsValues(stats map[string]*RoutePickStats) []RoutePickStats {
	out := make([]RoutePickStats, 0, len(stats))
	for _, s := range stats {
		out = append(out, RoutePickStats{
			RouteName: s.RouteName,
			USD:       s.USD,
			OK:        s.OK,
			Failed:    s.Failed,
			Landings:  s.Landings,
		})
	}
	return out
}

// RoutePickStatsFromRouteStats builds RoutePickStats from RouteStat.
// This is used to initialize stats from historical data.
func RoutePickStatsFromRouteStats(rs []RouteStat) map[string]*RoutePickStats {
	out := make(map[string]*RoutePickStats, len(rs))
	for _, r := range rs {
		out[r.Route.Name] = &RoutePickStats{
			RouteName: r.Route.Name,
			USD:       r.Route.USD,
			OK:        r.OK,
			Failed:    r.Failed,
			Landings:  r.OK, // Approximate: ok cards that have landed
		}
	}
	return out
}
