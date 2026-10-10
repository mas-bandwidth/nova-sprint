package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadedStore is a copy of a loaded store's shape (docs/SPEC-SPRINT.md, "the
// machine"): one up member of width 32, two flash routes, `ready` ready flash
// primaries the tick walks but does not deal, and `done` done work cards already
// on the fleet beside them. Each brief is distinct (a real card's is its own), so
// the deal reads every one's model lines, as the loaded store makes it do.
func loadedStore(t testing.TB, ready, done int) *Snapshot {
	w := newWorld(t)
	for _, name := range []string{"flash-a", "flash-b"} {
		w.s.Routes = append(w.s.Routes, Route{Name: name, Tier: "flash", Provider: "p", Model: name, Enabled: true})
	}
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 32}))
	cards := make([]CardAdd, ready)
	for i := range cards {
		cards[i] = CardAdd{ID: fmt.Sprintf("c%06d", i), Brief: fmt.Sprintf("c: the work tier: flash\nThe task of card %d.\n", i)}
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: cards}))
	for i := 0; i < done; i++ {
		w.s.Fleet.Put(&Card{ID: fmt.Sprintf("old-%d.w1", i), Row: "m1", Col: DoneOK, Fields: map[string]string{
			"ok": "yes", FieldRoute: []string{"flash-a", "flash-b"}[i%2], "finished": stamp(t0)}})
	}
	return w.s
}

// The tick's cost at that scale is a number in the gate (the sub-second tick): the
// deal resolves the route of every ready primary, and each resolution read the card's
// model lines with a regexp on every call; the same brief was read again by the
// friends' dealTierOf in the same tick, so one card's brief was parsed more than once a
// tick. The reads are counted, with no clock: reading them once a brief (modelOf,
// route.go) means the deal parses exactly as many briefs as it walks ready cards, never
// twice that for the two processes that resolve one card.
func TestTheTickDealReadsEachBriefOnceAtScale(t *testing.T) {
	s := loadedStore(t, 20000, 0)
	ready := len(s.Work.Column(Ready))
	require.Equal(t, 20000, ready, "20,000 ready primaries the tick walks and does not deal")

	before := modelReads.Load()
	TickDeal(s, TickReq{})
	parsed := modelReads.Load() - before
	t.Logf("TICK-COST ready=%d briefs_parsed=%d", ready, parsed)
	require.EqualValues(t, ready, parsed,
		"the deal parses each ready brief's model once, not once per part (routeOf, then the friends' dealTierOf) per card")

	before = modelReads.Load()
	TickDeal(s, TickReq{})
	require.Zero(t, modelReads.Load()-before, "a warm deal parses no brief again")
}

// BenchmarkTickDealAtLoad is the deal part's wall at the loaded store's scale: go test
// -bench TickDealAtLoad ./internal/sprint. The deal is the dominant part of the tick's
// drain and deal at load; the one-second tick gate hangs on it, and it stays under the
// gate because each brief's model lines are read once (route.go, modelOf), never once
// per ready card per part.
func BenchmarkTickDealAtLoad(b *testing.B) {
	for _, n := range []struct{ ready, done int }{
		{20000, 0}, {20000, 20000}, {50000, 0},
	} {
		b.Run(fmt.Sprintf("ready=%d_done=%d", n.ready, n.done), func(b *testing.B) {
			s := loadedStore(b, n.ready, n.done)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				TickDeal(s, TickReq{})
			}
		})
	}
}