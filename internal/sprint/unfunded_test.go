package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fundingWorld is setup's world with two pro routes of the providers named, each enabled and
// in the tier's array, and the providers' applied rows as given.
func fundingWorld(t *testing.T, n int, routes []Route, providers map[string]Provider) *world {
	t.Helper()
	w := setup(t, n)
	w.s.Routes = routes
	var arr []string
	for _, r := range routes {
		arr = append(arr, r.Name)
	}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: arr}
	w.s.Providers = providers
	return w
}

// dealtRoutes is the route of every work card on the fleet table, by card.
func dealtRoutes(s *Snapshot) map[string]string {
	out := map[string]string{}
	for _, c := range s.Fleet.Column(Ready, Working) {
		if c.F("kind") == "work" {
			out[c.ID] = c.F(FieldRoute)
		}
	}
	return out
}

// TestUnfundedProviderIsNeverDealt: two providers, one with its row applied funded=false and
// its route still enabled (one write per provider, never per route): the work deal draws
// none of its routes, a read never draws it (not even as the fallback of a tier whose every
// route rests), a fleet reader does not serve a tier only it holds, and the tier's reason
// names it. The reversed witness: with providerFunded forced true for it (the test seam),
// it is dealt.
//
// tla/ProviderFunding.tla NoDealToUnfunded (MCProviderFundingBrokenDealUnfunded)
func TestUnfundedProviderIsNeverDealt(t *testing.T) {
	t.Parallel()
	routes := []Route{
		{Name: "pro-dry", Tier: cardhdr.RoutePro, Provider: "dryprov", Model: "m-1", Enabled: true, First: true, Deadline: 600},
		{Name: "pro-paid", Tier: cardhdr.RoutePro, Provider: "paidprov", Model: "m-2", Enabled: true, Deadline: 600},
	}
	providers := map[string]Provider{
		"dryprov":  {Name: "dryprov", Billing: "metered", Funded: false},
		"paidprov": {Name: "paidprov", Billing: "metered", Funded: true},
	}
	w := fundingWorld(t, 4, routes, providers)
	pr := w.s.Work.Card("s1-1")
	require.NotNil(t, pr)

	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	dealt := dealtRoutes(w.s)
	require.NotEmpty(t, dealt, "the funded provider's route is dealt")
	for id, r := range dealt {
		assert.Equal(t, "pro-paid", r, "%s dealt on the unfunded provider's route", id)
	}
	// a read draws the funded route, even when it avoids it: the unfunded one is never the
	// fallback
	assert.Equal(t, "pro-paid", w.s.readRouteOf(routeIndexesOf(w.s), pr, nil)[FieldRoute])
	assert.NotEqual(t, "pro-dry", w.s.readRouteOf(routeIndexesOf(w.s), pr, []string{"pro-paid"})[FieldRoute])

	// a tier only the unfunded provider holds: no draw, no read, no fleet reader, and the
	// reason names the provider unfunded
	only := *w.s
	only.Routes = routes[:1]
	only.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-dry"}}
	set, _, why, _ := only.routeOf(pr, nil, routeIndexesOf(&only), "m1")
	assert.Empty(t, set[FieldRoute], "no route of the unfunded provider is drawn: %v", set)
	assert.Contains(t, why, "provider dryprov unfunded (the owner's to fund)")
	assert.False(t, only.tierRouted(cardhdr.RoutePro), "no fleet reader serves a tier only an unfunded provider holds")
	assert.Empty(t, only.readRouteOf(routeIndexesOf(&only), pr, nil)[FieldRoute], "no read draws an unfunded provider's route")
	reasons, notLive := only.TierNotLive(cardhdr.RoutePro)
	assert.True(t, notLive)
	assert.Contains(t, strings.Join(reasons, "; "), "provider dryprov unfunded")

	// the reversed witness: forced funded, the same provider is dealt and read
	forceProviderFunded.Store("dryprov")
	defer forceProviderFunded.Store("")
	set, _, why, _ = only.routeOf(pr, nil, routeIndexesOf(&only), "m1")
	assert.Equal(t, "pro-dry", set[FieldRoute], "witness: forced funded, the unfunded provider is dealt (%s)", why)
	assert.True(t, only.tierRouted(cardhdr.RoutePro), "witness: forced funded, a reader serves it")
	assert.Equal(t, "pro-dry", only.readRouteOf(routeIndexesOf(&only), pr, nil)[FieldRoute], "witness: forced funded, a read draws it")
}

// finishAll takes and finishes every work card dealt, each failed with the next report.
func finishAll(t *testing.T, w *world, reports ...string) []string {
	t.Helper()
	var routes []string
	i := 0
	for _, c := range w.s.Fleet.Column(Ready) {
		if c.F("kind") != "work" || i >= len(reports) {
			continue
		}
		wc, member := c.ID, c.Row
		routes = append(routes, c.F(FieldRoute))
		w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
		w.tick(time.Second)
		w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: reports[i]}))
		i++
	}
	require.Equal(t, len(reports), i, "every report finished a take")
	return routes
}

// owedProps is the PropUnfundedOwed writes of a plan.
func owedProps(p Plan) []PropWrite {
	var out []PropWrite
	for _, pw := range p.Props {
		if strings.HasPrefix(pw.Name, PropUnfundedOwed("")) {
			out = append(out, pw)
		}
	}
	return out
}

func twoRoutesOf(provider string, funded bool) ([]Route, map[string]Provider) {
	return []Route{
			{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: provider, Model: "m-a", Enabled: true, Deadline: 600},
			{Name: "pro-b", Tier: cardhdr.RoutePro, Provider: provider, Model: "m-b", Enabled: true, Deadline: 600},
		},
		map[string]Provider{provider: {Name: provider, Billing: "metered", Funded: funded}}
}

// TestOutOfCreditOwesOneFlip: two takes refused for credit (402) on two routes of one
// provider whose row says funded=true: the tick rests the provider once, and the plan step
// records ONE flip owed for the provider (never one per route or per take), carrying the
// class, the status, the card and the words; recorded, it is owed no second time; a provider
// whose row says funded=false already is recorded done, nothing to write.
//
// tla/ProviderFunding.tla BillingFailure, OneWritePerProvider (MCProviderFundingBrokenPerRoute)
func TestOutOfCreditOwesOneFlip(t *testing.T) {
	t.Parallel()
	routes, providers := twoRoutesOf("inception", true)
	w := fundingWorld(t, 2, routes, providers)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 2}}))
	credit := "provider failure: provider: class=out-of-credit status=402 msg=Insufficient credits"
	took := finishAll(t, w, credit, credit)
	assert.ElementsMatch(t, []string{"pro-a", "pro-b"}, took, "the two refusals are on two routes of the provider")
	w.tick(time.Second)
	w.part(TickDeal, TickReq{})
	rest, ok := ProviderRests(w.s.Fleet)["inception"]
	require.True(t, ok, "the tick rested the provider")
	require.True(t, rest.Refused(), "a refusal for credit: %+v", rest)

	p := UnfundedOwed(w.s)
	owed := owedProps(p)
	require.Len(t, owed, 1, "one flip owed for the provider: %+v", p.Props)
	assert.Equal(t, PropUnfundedOwed("inception"), owed[0].Name)
	assert.True(t, strings.HasPrefix(owed[0].Value, "owed "), owed[0].Value)
	for _, want := range []string{"out-of-credit", "402", "Insufficient credits"} {
		assert.Contains(t, owed[0].Value, want)
	}
	w.must(p)
	flips := FlipsOwed(w.s.Fleet.Props())
	require.Len(t, flips, 1)
	assert.Equal(t, rest.At, flips[0].Rest, "the flip is keyed by the rest it is owed for")
	assert.Contains(t, flips[0].ConfigNote(w.s.Now), "unfunded by the machine ")
	assert.Empty(t, owedProps(UnfundedOwed(w.s)), "owed once: a second pass records nothing")

	// written: recorded done once, and never owed again for the same rest
	w.must(UnfundedFlipped(w.s, "inception", rest.At, flips[0].ConfigNote(w.s.Now)))
	assert.Empty(t, FlipsOwed(w.s.Fleet.Props()))
	w.s.Providers = map[string]Provider{"inception": {Name: "inception", Billing: "metered", Funded: true}}
	assert.Empty(t, owedProps(UnfundedOwed(w.s)), "an owner who funds the row while the rest holds is never flipped back")
	assert.False(t, FlipsDue(w.s.Fleet.Props(), w.s.Providers, w.s.Now))

	// a row that says funded=false already: done, nothing owed
	routes2, providers2 := twoRoutesOf("dry", false)
	w2 := fundingWorld(t, 1, routes2, providers2)
	w2.s.Providers = map[string]Provider{"dry": {Name: "dry", Billing: "metered", Funded: true}}
	w2.must(Deal(w2.s, DealReq{Sel: Sel{Limit: 1}}))
	w2.s.Providers = providers2
	finishAll(t, w2, credit)
	w2.tick(time.Second)
	w2.part(TickDeal, TickReq{})
	p2 := UnfundedOwed(w2.s)
	require.Len(t, owedProps(p2), 1)
	assert.True(t, strings.HasPrefix(owedProps(p2)[0].Value, "done "), "a row false already is done: %s", owedProps(p2)[0].Value)
	assert.Empty(t, p2.Units, "nothing owed")
}

// TestRateLimitOwesNoFlip: a rate limit (a 429), a provider 5xx and a timeout on a funded
// provider owe no flip and leave its row funded: only a refusal for credit does. The reversed
// witness: a 429 let through as a refusal for credit (the test seam) owes a flip.
//
// tla/ProviderFunding.tla RateLimit, RateLimitKeepsFunding (MCProviderFundingBrokenRateLimitFlips)
func TestRateLimitOwesNoFlip(t *testing.T) {
	t.Parallel()
	const mark = "witness-of-TestRateLimitOwesNoFlip"
	transients := []string{
		"provider failure: provider: class=rate-limited status=429 msg=Too many requests " + mark,
		"provider failure: provider: class=provider-5xx status=503 msg=upstream unavailable",
		"provider failure: provider: class=timeout status=- msg=no answer in time",
	}
	run := func() *world {
		routes, providers := twoRoutesOf("busyprov", true)
		w := fundingWorld(t, 3, routes, providers)
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 3}}))
		finishAll(t, w, transients...)
		w.tick(time.Second)
		w.part(TickDeal, TickReq{})
		return w
	}
	w := run()
	assert.Empty(t, owedProps(UnfundedOwed(w.s)), "a 429, a 5xx and a timeout owe no flip")
	assert.False(t, FlipsDue(w.s.Fleet.Props(), w.s.Providers, w.s.Now))
	assert.True(t, w.s.Providers["busyprov"].Funded, "the row stays funded")

	// the reversed witness: the 429 let through as credit owes a flip
	rateLimitWitness.Store(mark)
	defer rateLimitWitness.Store("")
	ww := run()
	owed := owedProps(UnfundedOwed(ww.s))
	require.Len(t, owed, 1, "witness: a 429 let through owes a flip")
	assert.Contains(t, owed[0].Value, "429")
}
