package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readRouteRestWorld is a world with read cards on and a pro primary in review whose read
// tier's array is pro-a, pro-b, in that rotation order; both routes enabled, on different
// providers, so a rest on pro-a's provider leaves pro-b serving. The read the ask creates
// must carry pro-b's FieldRoute, never pro-a's: docs/SPEC-SPRINT.md section 6 (the reads),
// tla/RouteIndex.tla, THE READS.
func readRouteRestWorld(t *testing.T) *world {
	t.Helper()
	w := readCardsWorld(t, 4, "m1", "m2", "m3")
	w.s.Routes = []Route{
		{Name: "pro-a", Tier: "pro", Provider: "pa", Model: "model-a", Enabled: true},
		{Name: "pro-b", Tier: "pro", Provider: "pb", Model: "model-b", Enabled: true},
	}
	w.s.Tiers = map[string][]string{"pro": {"pro-a", "pro-b"}}
	// a fleet reader whose row names no tier reads flash alone once a route exists, so
	// the readers name the pro tier they read
	w.s.Readers.Texts = map[string]map[string]string{
		"reader-m1": {ReaderTiers: "flash,pro,heavy,frontier"},
		"reader-m2": {ReaderTiers: "flash,pro,heavy,frontier"},
		"reader-m3": {ReaderTiers: "flash,pro,heavy,frontier"},
	}
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "m1", 1)
	return w
}

// readRouteRateLimited puts one rate-limited take record on the fleet table for route, as a
// withdrawn card's take, so the rest rules read it (rateBackoffs, RestsDue): the first route
// of the rotation backs off its rate limit, so it rests at s.Now.
func readRouteRateLimited(w *world, id, route string, at time.Time) {
	take := ProviderTake{Route: route, Finished: stamp(at),
		Error: "provider: class=rate-limited status=429 msg=Too many requests"}
	w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: Withdrawn,
		Fields: map[string]string{FieldRateTake + "1": take.String(), FieldRoute: route}})
}

// TestAReadIsNeverAskedOntoARestingRoute pins that readRouteOf leaves out the routes of the
// read tier that rest at s.Now while another route of the tier serves, as it leaves out
// avoid: the ask draws the serving route's fields, never a resting route's. A 3-in-10 rule
// rest (three transient provider failures in the route's window) and a rate-limit backoff
// (one rate-limited take record, the dependency rate-limit-never-fails-the-card-v2) both
// hold the first route of the rotation; the second serves.
func TestAReadIsNeverAskedOntoARestingRoute(t *testing.T) {
	t.Parallel()
	t.Run("a 3-in-10 rest", func(t *testing.T) {
		t.Parallel()
		w := readRouteRestWorld(t)
		at := func(m int) string { return stamp(w.s.Now.Add(time.Duration(m) * time.Minute)) }
		for i, id := range []string{"t1", "t2", "t3"} {
			take := ProviderTake{Route: "pro-a", Finished: at(i + 1), Error: "provider: class=provider-5xx status=503 msg=upstream unavailable"}
			w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: Withdrawn,
				Fields: map[string]string{FieldProviderTake + "1": take.String(), FieldRoute: "pro-a"}})
		}
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.NotEmpty(t, reads, "the pro primary is asked its reads")
		for _, c := range reads {
			require.Equal(t, "pro-b", c.F(FieldRoute), "a read is never asked onto a resting route while another serves")
		}
	})
	t.Run("a rate-limit backoff", func(t *testing.T) {
		t.Parallel()
		w := readRouteRestWorld(t)
		readRouteRateLimited(w, "t1", "pro-a", w.s.Now.Add(time.Minute))
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.NotEmpty(t, reads, "the pro primary is asked its reads")
		for _, c := range reads {
			require.Equal(t, "pro-b", c.F(FieldRoute), "a read is never asked onto a resting route while another serves")
		}
	})
	t.Run("every route resting deals no read and raises nothing", func(t *testing.T) {
		t.Parallel()
		w := readRouteRestWorld(t)
		at := func(m int) string { return stamp(w.s.Now.Add(time.Duration(m) * time.Minute)) }
		for _, route := range []string{"pro-a", "pro-b"} {
			for i := range 3 {
				id := route + itoa(i)
				take := ProviderTake{Route: route, Finished: at(i + 1), Error: "provider: class=provider-5xx status=503 msg=upstream unavailable"}
				w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: Withdrawn,
					Fields: map[string]string{FieldProviderTake + "1": take.String(), FieldRoute: route}})
			}
		}
		// 2026-10-15: dealt onto a resting route, the next take-back retired each read at
		// once (RetiredByRest), a generation spent every tick until every fleet reader was
		// refused past the load window. The read waits for its route instead, cutting none.
		rs, _ := w.s.withRests()
		_, waits := readCardsAsk(rs, nil, nil)
		require.Equal(t, ReadRoutesRest, waits["s1-1"], "the read waits for its route, no cannot-ask judgment")
		dealReads(t, w, nil)
		require.Empty(t, readCardsOf(w, "s1-1"), "no read is cut onto a resting route")
	})
}

// TestAReadNeverDrawsARestingRouteWhileAnotherServes: with the read mix's used models
// avoided and the only route left resting, a read draws a route that serves, its model
// used again, never the resting one. Before, the walk lapsed the rests with the avoided
// models and drew the resting route; the next tick's take-back retired the read by rest,
// which spends no reader, and the deal asked it of the next member, every tick (2026-11-05:
// deterministic-desync-local's and r3-l3-model-lists's third reads went round the fleet on
// heavy-minimaxm3-direct, retired_by=rest, 3123 asks in 20 minutes). The clock is 1000 s.
func TestAReadNeverDrawsARestingRouteWhileAnotherServes(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	s := &Snapshot{
		Now:   now,
		Fleet: NewTable(Fleet),
		Work:  NewTable(Work),
		Routes: []Route{
			{Name: "heavy-rest", Tier: "heavy", Provider: "r", Model: "rm", Enabled: true},
			{Name: "heavy-used", Tier: "heavy", Provider: "u", Model: "um", Enabled: true},
		},
		Tiers:     map[string][]string{"heavy": {"heavy-rest", "heavy-used"}},
		Providers: map[string]Provider{"r": {Name: "r", Funded: true}, "u": {Name: "u", Funded: true}},
	}
	s.rests = map[string]RouteRest{"heavy-rest": {Route: "heavy-rest", Provider: "r", At: now, Until: now.Add(RouteRestFor), Cards: []string{"x"}, Cause: RestProvider}}
	pr := &Card{ID: "h-1", Fields: map[string]string{"brief": "tier: heavy\n"}}

	set := s.readRouteOf(routeIndexesOf(s), pr, []string{"heavy-used"})
	require.NotEmpty(t, set[FieldRoute])
	assert.Equal(t, "heavy-used", set[FieldRoute], "the avoided model lapses, the rest does not")
	_, rests := s.resting(set[FieldRoute])
	assert.False(t, rests, "a read is never dealt on a resting route while one serves")

	// unchanged: nothing avoided, the resting route is left out as before
	assert.Equal(t, "heavy-used", s.readRouteOf(routeIndexesOf(s), pr, nil)[FieldRoute])
}
