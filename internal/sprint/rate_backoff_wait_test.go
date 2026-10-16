package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// An ok end resets a route's rate-limit count and a failed end does not (tla/RateBackoff.tla,
// OkEnd: "the take ends ok"). The worker's finish waits in the member's finished cell for
// the readers' verdict (Finished): an ok finish there resets the count, and a card there
// still has its rate-limited take records read.
func TestAnOkFinishResetsTheBackoffAndAFailedOneDoesNot(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	rated := func(m int) string {
		return ProviderTake{Route: "a", Finished: stamp(t0.Add(time.Duration(m) * time.Second)),
			Error: "provider: class=rate-limited status=429 msg=Too many requests"}.String()
	}
	fleet := func(cards ...*Card) *Table {
		f := NewTable(Fleet)
		f.SetRows([]string{"m1"})
		f.Put(&Card{ID: "r1", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldRoute: "a", FieldRateTake + "1": rated(0)}})
		for _, c := range cards {
			f.Put(c)
		}
		return f
	}
	ended := func(col, ok string) *Card {
		return &Card{ID: "e1", Row: "m1", Col: col, Fields: map[string]string{FieldRoute: "a", "finished": stamp(t0.Add(10 * time.Second)), "ok": ok}}
	}
	for _, tc := range []struct {
		name   string
		card   *Card
		resets bool
	}{
		{"an ok finish waiting for its verdict resets", ended(Finished, "yes"), true},
		{"an ok finish the readers passed resets", ended(DoneOK, "yes"), true},
		{"an ok finish a reader read broken still resets: the route served", ended(DoneFailed, "yes"), true},
		{"a failed finish waiting in finished does not reset", ended(Finished, "no"), false},
		{"a failed finish does not reset", ended(DoneFailed, "no"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, backs := rateBackoffs(fleet(tc.card))["a"]
			assert.Equal(t, !tc.resets, backs)
		})
	}
	// a card in the finished cell has its own rate-limited take records read
	f := NewTable(Fleet)
	f.SetRows([]string{"m1"})
	f.Put(&Card{ID: "f1", Row: "m1", Col: Finished, Fields: map[string]string{FieldRoute: "a", "ok": "no",
		"finished": stamp(t0.Add(-time.Minute)), FieldRateTake + "1": rated(0)}})
	back, ok := rateBackoffs(f)["a"]
	require.True(t, ok, "the finished card's rate-limited take is read")
	assert.Equal(t, t0.Add(RateBackoffFor), back.Until)
}

// twoProRoutes is a world with two pro routes, pro-a and pro-b, pro-b resting 30 minutes
// for its 5xxs (a 3-in-10 rest), so a deal draws pro-a.
func twoProRoutes(t *testing.T) *world {
	t.Helper()
	w := twoProRoutesServing(t)
	rest := RouteRest{At: w.s.Now, Until: w.s.Now.Add(RouteRestFor), Cards: []string{"x1", "x2", "x3"}, Cause: RestProvider}
	w.s.Fleet.SetProps(map[string]string{PropRule3Rest("deepseek"): "pro-b " + rest.value()})
	return w
}

// twoProRoutesServing is twoProRoutes with both routes serving: nothing rests.
func twoProRoutesServing(t *testing.T) *world {
	t.Helper()
	w := setup(t, 1)
	w.s.Routes = []Route{
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "inception", Model: "mercury-3", Enabled: true, Deadline: 600},
		{Name: "pro-b", Tier: cardhdr.RoutePro, Provider: "deepseek", Model: "v4", Enabled: true, Deadline: 600},
	}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-a", "pro-b"}}
	return w
}

// A tier that is only partly backed off -- one route backs off from a rate limit, the other
// rests for its 5xxs -- has no route a deal can draw: the ready card waits out the backoff,
// never stalled work and never a no-route judgment (tla/RateBackoff.tla, BackoffEnds).
func TestAPartlyBackedOffTierIsAWaitNeverAStall(t *testing.T) {
	t.Parallel()
	w := twoProRoutes(t)
	_, wc := takeAndRefuse(t, w)
	require.Equal(t, "pro-a", w.s.Fleet.Card(wc).F(FieldRoute), "the deal drew the route that served")
	require.Equal(t, Ready, w.state("s1-1"))
	for _, f := range Unheld(running(w), w.s.Now) {
		assert.NotEqual(t, "s1-1", f.Subject, "a backoff is never stalled work: %s", f.Why)
	}
	hd := Holder(running(w), w.s.Now, "s1-1")
	assert.Contains(t, hd.Why, "waits for the rate-limit backoff on pro-a", "%s", hd)
	_, why, _ := w.s.routeServed(w.s.Work.Card("s1-1"), nil, "")
	assert.Empty(t, why, "no no-route judgment: the backed-off route is not unserved")
}

// backoffWaitAsserts are the three asserts of a tier with one route backing off and the other
// undrawable for a reason only the deal's own view knows: the ready card s1-1 is never
// stalled work (no NStalled), its holder names the backoff, and the rebalance moves a
// friend's queued pro card onto no machine (no machine's tier has a route to draw).
func backoffWaitAsserts(t *testing.T, w *world) {
	t.Helper()
	require.Equal(t, Ready, w.state("s1-1"))
	for _, f := range Unheld(running(w), w.s.Now) {
		assert.NotEqual(t, "s1-1", f.Subject, "a backoff is never stalled work: %s", f.Why)
	}
	hd := Holder(running(w), w.s.Now, "s1-1")
	assert.Contains(t, hd.Why, "waits for the rate-limit backoff on pro-a", "%s", hd)
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RoutePro}})
	rbPlace(w, "s1-8", cardhdr.RoutePro, FriendRow("amy"), Working)
	rbPlace(w, "s1-9", cardhdr.RoutePro, FriendRow("amy"), Ready)
	assert.Empty(t, rebalanced(Rebalance(w.s, workerSeatFixtures([]FriendSeat{amy}), "machine")), "no machine has a pro route to draw")
}

// A tier whose one route backs off from a rate limit while the other's provider is at its
// concurrency budget has no route a deal can draw, and the no-stall rule and the rebalance
// decide with the deal's own view (its budgets): the ready card waits out the backoff, never
// stalled work, and the rebalance moves nothing onto a machine with no route to draw
// (tla/RateBackoff.tla, BackoffEnds; docs/SPEC-SPRINT.md section 5). At PR #86's head both
// read a nil budget plan as "no provider is full" and raised NStalled.
func TestABackedOffTierWithTheOtherProviderFullIsAWaitNeverAStall(t *testing.T) {
	t.Parallel()
	w := twoProRoutesServing(t)
	w.s.Fleet.SetProps(map[string]string{PropProviderConcurrency("deepseek"): "1"})
	w.s.Fleet.Put(&Card{ID: "d1", Row: "m2", Col: Working, Fields: map[string]string{"kind": "work", FieldRoute: "pro-b", FieldModel: "deepseek/v4"}})
	_, wc := takeAndRefuse(t, w)
	require.Equal(t, "pro-a", w.s.Fleet.Card(wc).F(FieldRoute), "the deal drew the route with room")
	backoffWaitAsserts(t, w)
}

// A tier whose one route backs off from a rate limit while the other is enabled but left out
// of the tier's configured route array (tierArray) has no route a deal can draw: the draw
// walks the array, so the no-stall rule and the rebalance walk it too (tla/RateBackoff.tla,
// BackoffEnds; docs/SPEC-SPRINT.md section 5). At PR #86's head both walked every enabled
// route of the tier and counted the one never drawn as drawable, raising NStalled.
func TestABackedOffTierWithTheOtherRouteOutOfTheArrayIsAWaitNeverAStall(t *testing.T) {
	t.Parallel()
	w := twoProRoutesServing(t)
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-a"}}
	_, wc := takeAndRefuse(t, w)
	require.Equal(t, "pro-a", w.s.Fleet.Card(wc).F(FieldRoute), "the deal drew the array's one route")
	backoffWaitAsserts(t, w)
}

// The rebalance never moves a friend's queued card onto a machine whose tier's every route
// it could draw backs off: the move's draw would find no route and the card would land on
// the machine with no route or model, outside the routes and the backoff.
func TestRebalanceNeverMovesACardOntoAMachineWhoseTierBacksOff(t *testing.T) {
	t.Parallel()
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}})
	w := rbWorld(t, "m1")
	rbPlace(w, "s1-1", cardhdr.RouteFlash, FriendRow("amy"), Working)
	rbPlace(w, "s1-2", cardhdr.RouteFlash, FriendRow("amy"), Ready)
	w.s.Fleet.Put(&Card{ID: "x.w1", Row: "m1", Col: Withdrawn, Fields: map[string]string{"kind": "work", FieldRoute: "flash-a",
		FieldRateTake + "1": ProviderTake{Route: "flash-a", Finished: stamp(w.s.Now), Error: "provider: class=rate-limited status=429 msg=Too many requests"}.String()}})
	assert.Empty(t, rebalanced(Rebalance(w.s, workerSeatFixtures([]FriendSeat{amy}), "machine")), "flash-a backs off: no machine route to draw")
	w.tick(RateBackoffFor + time.Second)
	p := w.must(Rebalance(w.s, workerSeatFixtures([]FriendSeat{amy}), "machine"))
	require.Len(t, rebalanced(p), 1, "the backoff ended: the machine draws flash-a again")
	assert.Equal(t, "flash-a", w.s.Fleet.Card(WorkCardID("s1-2", 1)).F(FieldRoute))
}
