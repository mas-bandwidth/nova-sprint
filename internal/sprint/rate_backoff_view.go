package sprint

import "time"

// RateBackoffsAt is each route's rate-limit backoff that holds at now: the backoffs
// rateBackoffs computes from the fleet table's rate-limited take records alone, kept only
// while now is before their fixed end (Resting), so a caller that only reads the fleet
// table (the `routes` verb: cmd/nova-sprint/reads.go, cmdRoutes) sees a route backing off
// from a rate limit instead of one that serves while the deal draws nothing on it. It is
// the deal's path unchanged: RestsDue and the steps still read rateBackoffs, and this adds
// no write and no rest (docs/SPEC-SPRINT.md, the routes verb; tla/RateBackoff.tla,
// BackoffEnds).
func RateBackoffsAt(fleet *Table, now time.Time) map[string]RouteRest {
	out := map[string]RouteRest{}
	for route, r := range rateBackoffs(fleet) {
		if r.Resting(now) {
			out[route] = r
		}
	}
	return out
}
