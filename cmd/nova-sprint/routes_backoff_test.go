package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

const backoffReport = "provider failure: provider: class=rate-limited status=429 msg=Too many requests"

// routesRestView is what `routes --json` carries for each route's rest, as the repository's
// lowercase field names spell it.
type routesRestView struct {
	Routes []struct {
		Route struct {
			Name string `json:"name"`
		} `json:"route"`
		RestedUntil string `json:"rested_until"`
		RestedFor   string `json:"rested_for"`
	} `json:"routes"`
}

// routesBackoffApp is one flash route on its own provider, a card dealt, taken and finished
// with a rate-limited 429: the withdrawn work card holds one FieldRateTake record (Finished
// at the store's now), the record RateRests drops and the backoff rateBackoffs computes from
// it (route_rest.go). With no tick the backoff's line is not written; `routes` must read the
// record itself (docs/SPEC-SPRINT.md, the routes verb; tla/RateBackoff.tla).
func routesBackoffApp(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --members m1 --readers reader-a,reader-b")
	ta.m.SetRoutes([]sprint.Route{{Name: "pro-mercury3", Tier: "flash", Provider: "inception", Model: "mercury-3-preview-1002", Enabled: true}})
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --failed --report '" + backoffReport + "'")
	return ta
}

// A route backing off from a rate limit does not look like it serves: `routes` prints the
// route's ROUTE line rested_until=<the backoff's end, RFC3339> and why, and `routes --json`
// gives the same rested_until and a rested_for that begins "rate-backoff: "; once the clock
// passes the end both show "-" again, the backoff ended by itself (docs/SPEC-SPRINT.md, the
// routes verb; tla/RateBackoff.tla, BackoffEnds).
func TestRoutesShowsARateLimitBackoff(t *testing.T) {
	t.Parallel()
	ta := routesBackoffApp(t)

	// one 429 with no ok end between: 60s from the take's own record
	end := t0.Add(time.Minute).UTC().Format(time.RFC3339)
	out := ta.ok("routes")
	assert.Contains(t, out, "ROUTE pro-mercury3 model=inception/mercury-3-preview-1002")
	assert.Contains(t, out, " rested_until="+end+" ", "the backoff's end, RFC3339:\n%s", out)

	var v routesRestView
	ta.json("routes", &v)
	require.Len(t, v.Routes, 1)
	assert.Equal(t, "pro-mercury3", v.Routes[0].Route.Name)
	assert.Equal(t, end, v.Routes[0].RestedUntil, "--json carries the same end")
	assert.True(t, len(v.Routes[0].RestedFor) >= len("rate-backoff: ") && v.Routes[0].RestedFor[:len("rate-backoff: ")] == "rate-backoff: ", "rested_for begins rate-backoff: : %q", v.Routes[0].RestedFor)

	// the clock passes the end: the record stays, the backoff does not hold, the route serves
	ta.a.sleep(61 * time.Second)
	out = ta.ok("routes")
	assert.Contains(t, out, " rested_until=- ", "past the end the route shows no rest:\n%s", out)
	v = routesRestView{}
	ta.json("routes", &v)
	require.Len(t, v.Routes, 1)
	assert.Empty(t, v.Routes[0].RestedUntil, "and --json carries none")
	assert.Empty(t, v.Routes[0].RestedFor)
}

// restAt picks a backoff over a rest that does not hold or ends earlier, and a holding rest
// that ends later over a backoff: a route resting for its 5xxs or its provider's credit
// prints as today, and when such a rest and a backoff both hold the one that ends later is
// shown, as RouteRests already picks between a route's rest and its provider's
// (docs/SPEC-SPRINT.md, the routes verb; tla/RateBackoff.tla).
func TestRestAtShowsTheRestThatEndsLater(t *testing.T) {
	t.Parallel()
	now := t0
	short := sprint.RouteRest{Route: "a", Cause: sprint.RestRateBackoff, At: now, Until: now.Add(time.Minute)}
	long := sprint.RouteRest{Route: "a", Cause: sprint.RestProvider, At: now, Until: now.Add(30 * time.Minute)}
	retired := sprint.RouteRest{Route: "a", Cause: sprint.RestNoResult, At: now, Until: now.Add(24 * time.Hour)}

	// a backoff with no rest at all shows
	got, ok := restAt(nil, map[string]sprint.RouteRest{"a": short}, "a", now)
	require.True(t, ok)
	assert.Equal(t, short, got)

	// a backoff beside a 30-minute 5xx rest: the rest ends later, so it is shown
	got, ok = restAt(map[string]sprint.RouteRest{"a": long}, map[string]sprint.RouteRest{"a": short}, "a", now)
	require.True(t, ok)
	assert.Equal(t, long, got, "the longer rest wins")

	// a backoff that ends later than a resting rest wins
	got, ok = restAt(map[string]sprint.RouteRest{"a": short}, map[string]sprint.RouteRest{"a": long}, "a", now)
	require.True(t, ok)
	assert.Equal(t, long, got)

	// a rest that does not hold (a retired one) never hides a backoff
	got, ok = restAt(map[string]sprint.RouteRest{"a": retired}, map[string]sprint.RouteRest{"a": short}, "a", now)
	require.True(t, ok)
	assert.Equal(t, short, got, "a retired rest holds nothing, the backoff shows")
}
