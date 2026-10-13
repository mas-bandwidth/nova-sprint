package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// the provider's line of the overnight of 2026-10-12/13: the opencode route's provider broke
// the stream mid-take, after minutes of work, through no fault of the card
const report5xx = "provider failure: provider: class=provider-5xx status=- msg=Streaming response failed: [internal_error] Stream error: h2 protocol error: error reading a body from connection"

// proDeepseekOpencode is one unlimited pro route (no rpm, so never rateLimitedRoute): the
// out-of-credit no-spend path is closed to it, and a 5xx is the only way it returns a card
// untouched.
func proDeepseekOpencode(w *world) {
	w.s.Routes = []Route{{Name: "pro-deepseek-opencode", Tier: cardhdr.RoutePro, Provider: "opencode", Model: "deepseek-v4", Enabled: true, Deadline: 600}}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-deepseek-opencode"}}
}

// takeAndFail5xx deals the primary, takes its work card and finishes it with the 5xx line,
// returning the work card, the attempt the take cut and the finish's plan (applied).
func takeAndFail5xx(t *testing.T, w *world, id string) (string, int, Plan) {
	t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{id}}}))
	wc := w.s.Work.Card(id).F("work")
	require.NotEmpty(t, wc, "%s dealt", id)
	member := w.s.Fleet.Card(wc).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	attempt := w.s.Work.Card(id).Int("attempt")
	require.NotZero(t, attempt, "the take cut an attempt")
	require.Equal(t, "pro-deepseek-opencode", w.s.Fleet.Card(wc).F(FieldRoute))
	p := Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: report5xx})
	require.Len(t, p.Units, 1, "%+v", p)
	w.must(p)
	return wc, attempt, p
}

// TestAProvider5xxReturnsTheCardUntouched: last night's case. A take the provider's server
// failed (class provider-5xx) is the provider's transient fault, never the card's: the card
// is returned untouched, as an out-of-credit refusal on a limited route is (withdrawn
// WITHOUT FieldTakeEnded, the primary back in ready, no attempt and no redeal spent, no
// failed-work judgment), FieldRateReturns counts it, and its FieldProviderTake record is
// still written so the route's 3-in-10 rest counts it (tla/RouteRest.tla,
// Provider5xxSpendsNoRedeal). Red at BASE 65973f2b: takeEnded set FieldTakeEnded and the
// redeal that followed spent one of the card's three.
func TestAProvider5xxReturnsTheCardUntouched(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proDeepseekOpencode(w)
	wc, attempt, p := takeAndFail5xx(t, w, "s1-1")
	assert.Contains(t, p.Units[0].Moved, "the provider's server failed the take (returned untouched: no attempt spent)")

	c := w.s.Fleet.Card(wc)
	assert.Equal(t, Withdrawn, c.Col)
	assert.Empty(t, c.F(FieldTakeEnded), "no take ended: no redeal spent")
	assert.Equal(t, 1, c.Int(FieldRateReturns), "the return is counted toward MaxRateReturns")
	takes, _ := ProviderTakes(c)
	require.Len(t, takes, 1, "the 5xx is recorded so the route's rest counts it")
	assert.Equal(t, "pro-deepseek-opencode", takes[0].Route)
	assert.Contains(t, takes[0].Error, "class=provider-5xx")
	assert.Contains(t, takes[0].Error, "h2 protocol error")
	assert.Equal(t, Ready, w.state("s1-1"), "the primary goes back to ready")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "the same attempt, unspent")
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")

	// dealt again: the same work card, and the redeal bound is untouched
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Equal(t, wc, w.s.Work.Card("s1-1").F("work"))
	assert.Zero(t, w.s.Fleet.Card(wc).Int("redeals"), "no redeal spent")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "the same attempt, unspent")
}

// TestTheSixth5xxEndsTheTakeAsToday: a card already returned MaxRateReturns times ends its
// take as any provider failure does (takeEnded): FieldTakeEnded set, and the redeal that
// follows spends one. Green before and after: the bound on the no-spend path holds.
func TestTheSixth5xxEndsTheTakeAsToday(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proDeepseekOpencode(w)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Work.Card("s1-1").F("work")
	w.s.Fleet.Card(wc).Fields[FieldRateReturns] = itoa(MaxRateReturns)
	member := w.s.Fleet.Card(wc).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	p := Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: report5xx})
	require.Len(t, p.Units, 1, "%+v", p)
	assert.NotContains(t, p.Units[0].Moved, "returned untouched")
	assert.Contains(t, p.Units[0].Moved, "the provider failed the take")
	w.must(p)

	c := w.s.Fleet.Card(wc)
	assert.Equal(t, Withdrawn, c.Col)
	assert.NotEmpty(t, c.F(FieldTakeEnded), "the take ended")
	assert.Equal(t, MaxRateReturns, c.Int(FieldRateReturns), "past the bound nothing more is counted")
	takes, _ := ProviderTakes(c)
	require.Len(t, takes, 1)
	assert.Equal(t, Ready, w.state("s1-1"))

	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	assert.Equal(t, 1, w.s.Fleet.Card(wc).Int("redeals"), "the ended take spends a redeal")
}

// TestThree5xxOnOneCardRestTheRoute: the same card failed three times by the provider's
// server, returned untouched each time, keeps three records, each numbered by its return
// (returnTakeNumber), never by the redeals the returns leave unspent, so the route rests on
// them exactly as it did on three ended takes. Red before the records were numbered per
// return: every return wrote provider_take_1 and the route never rested.
func TestThree5xxOnOneCardRestTheRoute(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proDeepseekOpencode(w)
	var wc string
	for i := range 3 {
		require.Empty(t, RestsDue(w.s), "no rest before the third 5xx")
		wc, _, _ = takeAndFail5xx(t, w, "s1-1")
		c := w.s.Fleet.Card(wc)
		assert.Empty(t, c.F(FieldTakeEnded), "return %d spent no take", i+1)
		assert.Zero(t, c.Int("redeals"), "return %d spent no redeal", i+1)
		takes, numbers := ProviderTakes(c)
		require.Len(t, takes, i+1, "return %d keeps its own record", i+1)
		assert.Equal(t, returnTakeNumber(i+1), numbers[i], "return %d is numbered by its return", i+1)
	}
	rests := RestsDue(w.s)
	require.Len(t, rests, 1, "the route rests on one card's three 5xx returns: %+v", rests)
	assert.Equal(t, "pro-deepseek-opencode", rests[0].Route)
	assert.Equal(t, RestProvider, rests[0].Cause)
	assert.Equal(t, []string{wc, wc, wc}, rests[0].Cards, "the rest names the card once per 5xx")
}

// TestA5xxStillCountsTowardTheRouteRest: three takes on one route the provider's server
// failed rest the route for RouteRestFor, exactly as three ended 5xx takes did at BASE: the
// returns leave their FieldProviderTake records, which routeEnds reads (transient) and
// RestsDue counts. Green before and after: it pins that the rest did not move.
func TestA5xxStillCountsTowardTheRouteRest(t *testing.T) {
	t.Parallel()
	w := setup(t, 3)
	proDeepseekOpencode(w)
	var cards []string
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		require.Empty(t, RestsDue(w.s), "no rest before the third 5xx")
		wc, _, _ := takeAndFail5xx(t, w, id)
		cards = append(cards, wc)
	}
	rests := RestsDue(w.s)
	require.Len(t, rests, 1, "the route rests on its three 5xx takes: %+v", rests)
	assert.Equal(t, "pro-deepseek-opencode", rests[0].Route)
	assert.Equal(t, RestProvider, rests[0].Cause)
	assert.Equal(t, w.s.Now.Add(RouteRestFor), rests[0].Until)
	assert.ElementsMatch(t, cards, rests[0].Cards, "the rest names the three cards")
}
