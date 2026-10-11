package sprint

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// budgetRoutes is a flash tier of two routes: Mercury 3 first (inception), DeepSeek next.
func budgetRoutes() []Route {
	return []Route{
		{Name: "flash-mercury3", Tier: cardhdr.RouteFlash, Provider: "inception", Model: "mercury-3", Enabled: true, First: true, Deadline: 600},
		{Name: "flash-deepseek", Tier: cardhdr.RouteFlash, Provider: "deepseek", Model: "v4", Enabled: true, Deadline: 600},
	}
}

// proMercury is the one pro route the world's pro cards (setup) are dealt on.
func proMercury(w *world) {
	w.s.Routes = []Route{{Name: "pro-mercury3", Tier: cardhdr.RoutePro, Provider: "inception", Model: "mercury-3", Enabled: true, Deadline: 600}}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-mercury3"}}
}

func inFlightOn(s *Snapshot, id, member, route, model, col string) {
	s.Fleet.Put(&Card{ID: id, Row: member, Col: col, Fields: map[string]string{"kind": "work", FieldRoute: route, FieldModel: model}})
}

// The draw falls back past a provider at its concurrency budget to the next route of the
// tier with room, as it falls back past a resting one; a check of the tier (no index)
// does not, so a full provider is never a judgment. A route's limits ride on its cards.
func TestTheDrawFallsBackPastAFullProvider(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Now: t0, Fleet: NewTable(Fleet), Routes: budgetRoutes(),
		Tiers: map[string][]string{cardhdr.RouteFlash: {"flash-mercury3", "flash-deepseek"}}}
	s.Fleet.SetRows([]string{"m1", "m2"})
	s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "1", PropModelRPM("inception/mercury-3"): "10"})
	card := &Card{ID: "s1-1", Fields: map[string]string{"brief": "tier: flash\n"}}

	set, _, why, _ := s.withBudgets().routeOf(card, nil, routeIndexesOf(s), "m1")
	require.Empty(t, why)
	assert.Equal(t, "flash-mercury3", set[FieldRoute], "a provider with room is drawn first")
	assert.Equal(t, "10", set[FieldRateRPM], "the model's rpm rides on the card")
	assert.Equal(t, "1", set[FieldRateConcurrent], "the provider's concurrency rides on the card")

	inFlightOn(s, "x.w1", "m2", "flash-mercury3", "inception/mercury-3", Working)
	set, _, why, _ = s.withBudgets().routeOf(card, nil, routeIndexesOf(s), "m1")
	require.Empty(t, why)
	assert.Equal(t, "flash-deepseek", set[FieldRoute], "a full provider is passed over for the next route with room")
	assert.Empty(t, set[FieldRateRPM], "an unlimited route's cards carry no limit")
	assert.Empty(t, set[FieldRateConcurrent])

	set, _, why, _ = s.routeOf(card, nil, nil, "")
	require.Empty(t, why, "a check of the tier never sees a full provider as unserved")
	assert.Equal(t, "flash-mercury3", set[FieldRoute])

	// both providers full: the deal's draw waits; a rework's or a redo's (soft) is dealt
	// on a full one anyway, and the take holds it ready
	s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "1", PropProviderConcurrency("deepseek"): "1"})
	inFlightOn(s, "y.w1", "m2", "flash-deepseek", "deepseek/v4", Ready)
	_, _, why, _ = s.withBudgets().routeOf(card, nil, routeIndexesOf(s), "m1")
	assert.Contains(t, why, "waits ready")
	set, _, why, _ = s.withSoftBudgets().routeOf(card, nil, routeIndexesOf(s), "m1")
	require.Empty(t, why, "a rework is never refused for a full provider")
	assert.NotEmpty(t, set[FieldRoute])
}

// Every route of the tier full: the deal refuses the card with the words why, it waits
// ready (no attempt, no judgment), and the next deal after a slot frees draws it.
func TestEveryRouteFullTheCardWaitsReadyThenIsDealt(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	proMercury(w)
	w.s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "1"})
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
	require.Len(t, p.Units, 1, "one card dealt into the one slot: %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "concurrency budget")
	assert.Contains(t, p.Refused[0].Why, "waits ready")
	w.do(p)
	waiting := "s1-1"
	if w.state("s1-1") != Ready {
		waiting = "s1-2"
	}
	assert.Equal(t, Ready, w.state(waiting), "the card over the budget waits ready")
	assert.Empty(t, w.s.Open, "a full provider raises no judgment")
	assert.Zero(t, w.s.Work.Card(waiting).Int("attempt"), "no attempt is cut for the waiting card")

	// the slot frees: the dealt card's take ends ok
	dealt := ""
	for _, c := range w.s.Fleet.Column(Ready) {
		if c.F("kind") == "work" {
			dealt = c.ID
		}
	}
	require.NotEmpty(t, dealt)
	w.must(Take(w.s, TakeReq{As: w.s.Fleet.Card(dealt).Row, Sel: Sel{IDs: []string{dealt}}, Gens: w.gens(dealt)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{dealt}}, Gens: w.gens(dealt)}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{waiting}}}))
	assert.Equal(t, Working, w.state(waiting), "dealt once a slot freed")
}

// A concurrency budget of 2 held at the take across the fleet: three members each hold a
// ready card on the provider; two are taken, the third waits ready (never refused as a
// failure) and is taken once one of the two ends.
func TestConcurrencyTwoTheThirdTakeWaits(t *testing.T) {
	t.Parallel()
	w := setup(t, 3)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m3"}))
	proMercury(w)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 3}}))
	byMember := map[string]string{}
	for _, c := range w.s.Fleet.Column(Ready) {
		if c.F("kind") == "work" {
			byMember[c.Row] = c.ID
		}
	}
	require.Len(t, byMember, 3, "one card a member: %v", byMember)
	w.s.Fleet.SetProps(map[string]string{PropProviderConcurrency("inception"): "2"})
	taken := 0
	var waited string
	for _, m := range []string{"m1", "m2", "m3"} {
		p := w.must(Take(w.s, TakeReq{As: m, Sel: Sel{Limit: 4}}))
		if len(p.Units) == 1 {
			taken++
		} else {
			waited = m
		}
	}
	require.Equal(t, 2, taken, "two taken under a budget of two")
	require.NotEmpty(t, waited)
	assert.Equal(t, Ready, w.s.Fleet.Card(byMember[waited]).Col, "the third card waits ready")
	by := Take(w.s, TakeReq{As: waited, Sel: Sel{IDs: []string{byMember[waited]}}, Gens: w.gens(byMember[waited])})
	require.Len(t, by.Refused, 1)
	assert.Contains(t, by.Refused[0].Why, "concurrency budget (2 of 2 working")
	assert.Contains(t, by.Refused[0].Why, "waits ready")

	// one ends: the third is taken
	var done string
	for m, id := range byMember {
		if m != waited {
			done = id
			break
		}
	}
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{done}}, Gens: w.gens(done)}))
	p := w.must(Take(w.s, TakeReq{As: waited, Sel: Sel{Limit: 4}}))
	assert.Len(t, p.Units, 1, "taken once a slot freed")
}

// A rate-limited take on a LIMITED route that reaches the sprint (an older member, or
// retries out of the card's deadline) returns the card untouched: withdrawn without a take
// ended, so no redeal and no attempt are spent; no take record, so the route's 3-in-10 rest
// never counts it; no failed-work judgment.
func TestARateLimitedTakeReturnsTheCardUntouched(t *testing.T) {
	t.Parallel()
	for _, report := range []string{
		"provider failure: provider: class=rate-limited status=429 msg=Too many requests",
		"provider failure: provider: class=other status=400 msg=Your current concurrency is 9/12, which exceeds your concurrency limit",
	} {
		w := setup(t, 1)
		proMercury(w)
		w.s.Fleet.SetProps(map[string]string{PropModelRPM("inception/mercury-3"): "10"})
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		require.Equal(t, "10", w.s.Fleet.Card(w.s.Work.Card("s1-1").F("work")).F(FieldRateRPM), "the limited route's card carries its rpm")
		wc := w.s.Work.Card("s1-1").F("work")
		member := w.s.Fleet.Card(wc).Row
		w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
		p := Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: report})
		require.Len(t, p.Units, 1, "%+v", p)
		assert.Contains(t, p.Units[0].Moved, "returned untouched")
		w.must(p)
		c := w.s.Fleet.Card(wc)
		assert.Equal(t, Withdrawn, c.Col)
		assert.Empty(t, c.F(FieldTakeEnded), "no take ended: no redeal spent")
		takes, _ := ProviderTakes(c)
		assert.Empty(t, takes, "no take record: never counted toward a route's rest")
		assert.Equal(t, Ready, w.state("s1-1"), "the primary goes back to ready")
		assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
		assert.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"), "the attempt is the same attempt")
	}
}

const tooMany = "provider failure: provider: class=rate-limited status=429 msg=Too many requests"

// takeAndRefuse deals s1-1 (again), takes it and finishes it with a 429, and says how the
// finish moved it and which work card it was.
func takeAndRefuse(t *testing.T, w *world) (string, string) {
	t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Work.Card("s1-1").F("work")
	member := w.s.Fleet.Card(wc).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	p := w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: tooMany}))
	require.Len(t, p.Units, 1)
	return p.Units[0].Moved, wc
}

// An UNLIMITED route's 429 ends the take as any provider failure does: a take ended, its
// record written (it counts toward the route's 3-in-10 rest), a redeal spent. Only a
// limited route's 429 is the limiter's to absorb.
func TestAnUnlimitedRoutes429EndsTheTake(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proMercury(w)
	moved, wc := takeAndRefuse(t, w)
	assert.NotContains(t, moved, "returned untouched")
	c := w.s.Fleet.Card(wc)
	assert.NotEmpty(t, c.F(FieldTakeEnded), "the take ended")
	takes, _ := ProviderTakes(c)
	assert.Len(t, takes, 1, "the take's record counts toward the route's rest")
}

// Even a limited route cannot loop a card forever: MaxRateReturns untouched returns, then
// the next 429 ends the take as any provider failure does.
func TestALimitedRoutesReturnsAreBounded(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proMercury(w)
	w.s.Fleet.SetProps(map[string]string{PropModelRPM("inception/mercury-3"): "10"})
	var wc string
	for i := range MaxRateReturns {
		var moved string
		moved, wc = takeAndRefuse(t, w)
		assert.Contains(t, moved, "returned untouched", "return %d", i+1)
	}
	c := w.s.Fleet.Card(wc)
	assert.Equal(t, MaxRateReturns, c.Int(FieldRateReturns))
	assert.Empty(t, c.F(FieldTakeEnded), "no take ended in the returns")
	assert.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"), "the returns spent no attempt")
	moved, wc := takeAndRefuse(t, w)
	assert.NotContains(t, moved, "returned untouched", "the return past the bound ends the take")
	assert.NotEmpty(t, w.s.Fleet.Card(wc).F(FieldTakeEnded))
}

func TestIsRateLimited(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]bool{
		"provider failure: provider: class=rate-limited status=429 msg=Too many requests":            true,
		"provider failure: provider: class=other status=429 msg=slow down":                           true,
		"provider failure: provider: class=other status=400 msg=concurrency 9/12 exceeds your limit": true,
		"provider failure: provider: class=out-of-credit status=429 msg=insufficient balance":        false,
		"provider failure: provider: class=auth status=401 msg=bad key":                              false,
		"provider failure: provider: class=provider-5xx status=503 msg=unavailable":                  false,
		cardhdr.EndNoResult + ": the child left no result":                                           false,
		"tests red": false,
	} {
		assert.Equal(t, want, IsRateLimited(line), line)
	}
}

// routes limit: an rpm on a route is its model's; a concurrency on a provider (or a
// route's provider); 0 clears; an rpm on a provider is refused; a reason is required.
func TestRoutesLimitSetsAndClearsTheBudgets(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.s.Routes = budgetRoutes()
	w.must(SetRouteLimit(w.s, RouteLimitReq{Target: "flash-mercury3", Set: "rpm", RPM: 10, Reason: "preview asks under 10 a minute", Who: "rowan"}))
	w.must(SetRouteLimit(w.s, RouteLimitReq{Target: "deepseek", Set: "concurrent", Concurrent: 8, Reason: "account limit under 9", Who: "rowan"}))
	assert.Equal(t, 10, ModelRPM(w.s, "inception/mercury-3"))
	assert.Equal(t, 8, ProviderConcurrency(w.s, "deepseek"))
	rows := Limits(w.s)
	require.Len(t, rows, 2)
	assert.Equal(t, LimitRow{Kind: "rpm", Target: "inception/mercury-3", Budget: 10}, rows[0])
	assert.Equal(t, LimitRow{Kind: "concurrent", Target: "deepseek", Budget: 8}, rows[1])
	require.Len(t, w.notesOf(NRouteLimited), 2)

	w.must(SetRouteLimit(w.s, RouteLimitReq{Target: "flash-deepseek", Set: "concurrent", Concurrent: 0, Reason: "lifted", Who: "rowan"}))
	assert.Zero(t, ProviderConcurrency(w.s, "deepseek"), "0 clears")

	for _, bad := range []RouteLimitReq{
		{Target: "inception", Set: "rpm", RPM: 10, Reason: "r"},
		{Target: "flash-mercury3", Set: "rpm", RPM: 10},
		{Target: "nowhere", Set: "concurrent", Concurrent: 1, Reason: "r"},
		{Target: "deepseek", Set: "concurrent", Concurrent: -1, Reason: "r"},
	} {
		p := SetRouteLimit(w.s, bad)
		require.Len(t, p.Refused, 1, "%+v", bad)
		assert.Empty(t, p.Props, "%+v", bad)
	}
	assert.True(t, strings.Contains(SetRouteLimit(w.s, RouteLimitReq{Target: "inception", Set: "rpm", RPM: 1, Reason: "r"}).Refused[0].Why, "a model's budget"))
}

// No budget set: the deal draws as before and stamps nothing (zero code path change for
// an unlimited route).
func TestNoBudgetDealsAsBefore(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Now: t0, Fleet: NewTable(Fleet), Routes: budgetRoutes(),
		Tiers: map[string][]string{cardhdr.RouteFlash: {"flash-mercury3", "flash-deepseek"}}}
	s.Fleet.SetRows([]string{"m1"})
	b := s.withBudgets()
	assert.Nil(t, b.budgets, "no budget set: no budget view")
	set, _, why, _ := b.routeOf(&Card{ID: "s1-1", Fields: map[string]string{"brief": "tier: flash\n"}}, nil, routeIndexesOf(s), "m1")
	require.Empty(t, why)
	_, rpm := set[FieldRateRPM]
	_, conc := set[FieldRateConcurrent]
	assert.False(t, rpm || conc, "no limit field on an unlimited route's card: %v", set)
}
