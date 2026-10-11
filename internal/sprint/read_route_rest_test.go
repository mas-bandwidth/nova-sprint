package sprint

import (
	"testing"
	"time"

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
	t.Run("every route resting behaves as today", func(t *testing.T) {
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
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.NotEmpty(t, reads, "the ask refuses nothing and raises nothing when every route rests: the take-back retires it")
		for _, c := range reads {
			require.NotEmpty(t, c.F(FieldRoute), "a read is still asked a route when none serves")
		}
	})
}
