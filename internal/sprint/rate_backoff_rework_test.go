package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rateLimitedTake is one rate-limited take record on route, as the fleet table holds it
// (internal/sprint/rate_backoff_wait_test.go builds one the same way).
func rateLimitedTake(now time.Time, route string) ProviderTake {
	return ProviderTake{Route: route, Finished: stamp(now), Error: "provider: class=rate-limited status=429 msg=Too many requests"}
}

// backoffOnRoute puts a rate-limited take record on route in the fleet table, so the route
// backs off (tla/RateBackoff.tla).
func backoffOnRoute(w *world, route string) {
	w.s.Fleet.Put(&Card{ID: "bk-" + route, Row: "m1", Col: Withdrawn, Fields: map[string]string{
		FieldRoute:        route,
		FieldRateTake + "1": rateLimitedTake(w.s.Now, route).String(),
	}})
}

// oneBackedOffProRoute is a pro world with the single route pro-mercury3 backing off from a
// rate limit: the soft draw (a coordinator's rework or redo) has no route of the tier to
// deal the card on.
func oneBackedOffProRoute(t *testing.T) *world {
	t.Helper()
	w := setup(t, 1)
	proMercury(w)
	// a provider budget that is not full: the tier's draw fails only because its one route
	// backs off, and the rework's or redo's draw is soft (withSoftBudgets)
	w.s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "5"})
	backoffOnRoute(w, "pro-mercury3")
	return w
}

// A coordinator's rework whose tier's every route backs off is refused, and the refusal
// never tells the coordinator the card waits ready: no deal resumes by itself, so the words
// say the rework was not dealt, name the backoff's end, and say to give it again after that
// time (tla/RateBackoff.tla, BackoffEnds; docs/SPEC-SPRINT.md section 5, the deal).
func TestAReworkOnABackedOffRouteIsRefusedNotToldItWaits(t *testing.T) {
	t.Parallel()
	w := oneBackedOffProRoute(t)
	putReview(w, "s1-1", proBrief, 1, 1, "head")
	rest, ok := rateBackoffs(w.s.Fleet)["pro-mercury3"]
	require.True(t, ok, "the route backs off from a rate limit")

	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "the fix"})
	require.Len(t, p.Refused, 1, "the rework is refused: %+v", p)
	why := p.Refused[0].Why
	assert.Equal(t, "s1-1", p.Refused[0].Key)
	assert.NotContains(t, why, "waits ready", "a refused rework is not told it waits ready")
	assert.Contains(t, why, "every route of tier pro", why)
	assert.Contains(t, why, "backing off from a rate limit", why)
	assert.Contains(t, why, "was not dealt", why)
	assert.Contains(t, why, rest.UntilSaid(), "the refusal names the backoff's end")
	assert.Contains(t, why, "give it again", why)
}

// The same for a Redo: a conflicted card's redo whose tier's every route backs off is
// refused with the rework's words, never the waiting ones (tla/RateBackoff.tla, BackoffEnds;
// docs/SPEC-SPRINT.md section 5, the deal).
func TestARedoOnABackedOffRouteIsRefusedNotToldItWaits(t *testing.T) {
	t.Parallel()
	w := stoppedForConflict(t)
	proMercury(w)
	w.s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "5"})
	backoffOnRoute(w, "pro-mercury3")
	rest, ok := rateBackoffs(w.s.Fleet)["pro-mercury3"]
	require.True(t, ok, "the route backs off from a rate limit")

	p := Redo(w.s, RedoReq{Sel: Sel{IDs: []string{"s1-2"}}, Who: "coordinator"})
	require.Len(t, p.Refused, 1, "the redo is refused: %+v", p)
	why := p.Refused[0].Why
	assert.Equal(t, "s1-2", p.Refused[0].Key)
	assert.NotContains(t, why, "waits ready", "a refused redo is not told it waits ready")
	assert.Contains(t, why, "every route of tier pro", why)
	assert.Contains(t, why, "backing off from a rate limit", why)
	assert.Contains(t, why, "was not dealt", why)
	assert.Contains(t, why, rest.UntilSaid(), "the refusal names the backoff's end")
	assert.Contains(t, why, "give it again", why)
}

// A deal (not soft) on the same world keeps today's words: there the card does wait ready
// and is dealt when the backoff ends (tla/RateBackoff.tla, BackoffEnds; docs/SPEC-SPRINT.md
// section 5, the deal).
func TestADealOnABackedOffRouteStillTellsTheCardItWaits(t *testing.T) {
	t.Parallel()
	w := oneBackedOffProRoute(t)
	require.Equal(t, Ready, w.state("s1-1"))

	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Len(t, p.Refused, 1, "the deal is refused: %+v", p)
	why := p.Refused[0].Why
	assert.Contains(t, why, "waits ready", "a deal's card does wait ready")
	assert.NotContains(t, why, "was not dealt", "a deal's card is dealt when the backoff ends")
}

// A soft rework with a full provider and no backoff is still dealt on the full one: the
// fallback past a full provider is a rework's, only a rate-limit backoff is not
// (docs/SPEC-SPRINT.md section 5, the deal).
func TestASoftReworkWithAFullProviderIsStillDealtOnIt(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proMercury(w)
	putReview(w, "s1-1", proBrief, 1, 1, "head")
	w.s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "1"})
	inFlightOn(w.s, "x.w1", "m2", "pro-mercury3", "inception/mercury-3", Working)

	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "the fix"}))
	require.Equal(t, Working, w.state("s1-1"))
	assert.Equal(t, "pro-mercury3", w.s.Fleet.Card("s1-1.w2").F(FieldRoute), "the soft rework is dealt on the full provider")
}
