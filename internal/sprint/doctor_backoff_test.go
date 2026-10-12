package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// 2b. A tier whose every enabled route backs off from a rate limit is a wait, never a fault:
// the tier waits by itself and the backoff ends by itself at a fixed time, so doctor says
// WARN, names the backoff with its end and its remedy says the wait ends by itself
// (tla/RateBackoff.tla, BackoffEnds; docs/SPEC-SPRINT.md, the doctor's routes-per-tier check).
// The one route a member up can launch, so before the backoff the tier is usable.
func TestDoctorRoutesPerTierWarnsWhileATierBacksOff(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.preparePlanned()
	rateTaken(w, "flash-oc")
	cs := DoctorRoutesPerTier(healthyInput(w))
	require.Len(t, cs, 1, lines(cs))
	c := cs[0]
	assert.Equal(t, cardhdr.RouteFlash, c.Subject)
	assert.Equal(t, DoctorWarn, c.Status, c.Line())
	until := stamp(w.s.Now.Add(RateBackoffFor))
	assert.Contains(t, c.Evidence, "ready=2 enabled=1 resting=0 usable=0", c.Line())
	assert.Contains(t, c.Evidence, "backoff=flash-oc until "+until, c.Line())
	assert.Contains(t, c.Remedy, "ends by itself", c.Line())
	assert.Contains(t, c.Remedy, until, c.Line())
}

// 2c. The same world with the route resting for its 5xxs (a RouteRest of cause RestProvider)
// stays FAIL as today: that rest does not end by itself, so the providers lines still say the
// verb that ends it.
func TestDoctorRoutesPerTierFailsWhileATierRestsForIts5xxs(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.preparePlanned()
	rest := RouteRest{At: w.s.Now, Until: w.s.Now.Add(RouteRestFor), Cards: []string{"x1", "x2", "x3"}, Cause: RestProvider}
	w.s.Fleet.SetProps(map[string]string{PropRule3Rest("p"): "flash-oc " + rest.value()})
	cs := DoctorRoutesPerTier(healthyInput(w))
	require.Len(t, cs, 1, lines(cs))
	c := cs[0]
	assert.Equal(t, DoctorFail, c.Status, c.Line())
	assert.Contains(t, c.Evidence, "resting=1", c.Line())
	assert.NotContains(t, c.Evidence, "backoff=", c.Line())
	assert.NotContains(t, c.Remedy, "ends by itself", c.Line())
	assert.Contains(t, c.Remedy, "providers lines", c.Line())
}

// 2d. A tier with one route backing off and another a member up can launch stays as today:
// one usable route is WARN, and the backoff is named beside it.
func TestDoctorRoutesPerTierWithOneBackoffAndOneUsableWarns(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.s.Routes = append(w.s.Routes, Route{Name: "flash-oc2", Tier: cardhdr.RouteFlash, Provider: "p", Model: "b", Enabled: true, Deadline: 600})
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.preparePlanned()
	rateTaken(w, "flash-oc")
	cs := DoctorRoutesPerTier(healthyInput(w))
	require.Len(t, cs, 1, lines(cs))
	c := cs[0]
	assert.Equal(t, DoctorWarn, c.Status, c.Line())
	assert.Contains(t, c.Evidence, "usable=1", c.Line())
	assert.Contains(t, c.Evidence, "backoff=flash-oc until "+stamp(w.s.Now.Add(RateBackoffFor)), c.Line())
	assert.Contains(t, c.Evidence, "routes=flash-oc2:", c.Line())
}

// rateTaken writes the rate-limited take a withdrawn work card records on route: the record
// rateBackoffs reads to back the route off until the snapshot's now plus RateBackoffFor
// (rate-limit-never-fails-the-card-v2; internal/sprint/rate_backoff_wait_test.go).
func rateTaken(w *world, route string) {
	w.s.Fleet.Put(&Card{ID: "s1-9.w1", Row: "m1", Col: Withdrawn, Fields: map[string]string{
		"kind":     "work",
		FieldRoute: route,
		FieldRateTake + "1": ProviderTake{Route: route, Finished: stamp(w.s.Now),
			Error: "provider: class=rate-limited status=429 msg=Too many requests"}.String()}})
}
