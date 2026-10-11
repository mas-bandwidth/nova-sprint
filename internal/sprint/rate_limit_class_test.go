package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A line the provider itself classed as its own server error or a timeout is never a rate
// limit, whatever its words: the word fallback answers only when no class parses, or one
// that is neither class5xx nor classTimeout. So a 5xx whose message mentions a rate limit
// is the provider's own failure (class provider-5xx, tla/RateBackoff.tla), and three of
// them on a route rest it (tla/RouteRest.tla, RestProvider). An out-of-credit refusal stays
// false, as today.
func TestIsRateLimitedByClass(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]bool{
		// class rate-limited, or a 429 under any class
		"provider failure: provider: class=rate-limited status=429 msg=slow down":     true,
		"provider failure: provider: class=other status=429 msg=slow down":            true,
		"provider failure: provider: class=other status=400 msg=Too many requests":    true,
		// no class parses: the words still answer
		"provider failure: Too many requests, retry later":                            true,
		// a 5xx or a timeout whose msg mentions a rate limit is not one
		"provider failure: provider: class=provider-5xx status=503 msg=upstream rate limit exceeded, retry later": false,
		"provider failure: provider: class=provider-5xx status=503 msg=Too many requests upstream":               false,
		"provider failure: provider: class=timeout status=- msg=rate limit exceeded upstream":                    false,
		"provider failure: provider: class=timeout status=- msg=concurrency 9/12 exceeds your limit":              false,
		// an out-of-credit refusal is never one, even on a 429
		"provider failure: provider: class=out-of-credit status=429 msg=insufficient balance": false,
	} {
		assert.Equal(t, want, IsRateLimited(line), line)
	}
}

// A provider's 5xx whose message mentions a rate limit spends its take, exactly as any
// other provider failure does: FieldTakeEnded is set, the take leaves a FieldProviderTake
// record and no rate_take_ record, so three such 5xxs in the window rest the route from now
// for RouteRestFor with cause RestProvider (tla/RouteRest.tla; tla/RateBackoff.tla: the
// take was the provider's, never the limiter's to absorb).
func TestAFiveHundredMentioningARateLimitStillSpendsTheTake(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proMercury(w)
	w.s.Fleet.SetProps(map[string]string{PropModelRPM("inception/mercury-3"): "10"})
	const line = "provider failure: provider: class=provider-5xx status=503 msg=upstream rate limit exceeded, retry later"
	for i := range 3 {
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wc := w.s.Work.Card("s1-1").F("work")
		member := w.s.Fleet.Card(wc).Row
		w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
		p := w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: line}))
		require.Len(t, p.Units, 1, "take %d: %+v", i+1, p)
		c := w.s.Fleet.Card(wc)
		assert.NotEmpty(t, c.F(FieldTakeEnded), "take %d ended", i+1)
		takes, _ := ProviderTakes(c)
		assert.NotEmpty(t, takes, "take %d records its provider take", i+1)
		rates, _ := RateTakes(c)
		assert.Empty(t, rates, "take %d is no rate take", i+1)
		assert.Empty(t, c.F(FieldRateReturns), "take %d is no out-of-credit return", i+1)
	}
	due := RestsDue(w.s)
	require.Len(t, due, 1, "three transient takes rest the route: %+v", due)
	assert.Equal(t, "pro-mercury3", due[0].Route)
	assert.Equal(t, RestProvider, due[0].Cause)
	assert.Equal(t, w.s.Now.Add(RouteRestFor), due[0].Until)
	assert.Len(t, due[0].Cards, 3)
}
