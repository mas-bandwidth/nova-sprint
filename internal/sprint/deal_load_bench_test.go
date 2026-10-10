package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadedStore is a copy of a loaded store's shape (docs/SPEC-SPRINT.md, "the
// machine"): two up members of width 32, two flash routes and one pro, `ready`
// ready flash primaries the tick walks and deals, `done` done work cards already
// on the fleet beside them, and `friends` up friends of the flash and pro classes
// the deal offers every ready primary to first. Each brief is distinct (a real
// card's is its own), so the deal reads every one's model lines, as the loaded
// store makes it do. With friends up the deal resolves a ready primary's model
// lines once in the friends' pass (dealTierOf) and again in the machines'
// (routeOf), which is the twice the tick pays for one card and the deal's dominant
// cost at load.
func loadedStore(t testing.TB, ready, done, friends int) *Snapshot {
	w := newWorld(t, "reader-a", "reader-b")
	for _, name := range []string{"flash-a", "flash-b", "pro-a"} {
		tier := "flash"
		if name == "pro-a" {
			tier = "pro"
		}
		w.s.Routes = append(w.s.Routes, Route{Name: name, Tier: tier, Provider: "p", Model: name, Enabled: true})
	}
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 32}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 32}))
	for i := 0; i < friends; i++ {
		w.s.Friends = append(w.s.Friends, FriendSeat{Name: fmt.Sprintf("f%02d", i), Width: 8, Status: Up, Class: "flash,pro"})
	}
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
// deal resolves the model lines of every ready primary twice a tick, once in the
// friends' pass (dealTierOf, which draws the tier a friend deals it on) and once in
// the machines' (routeOf, which draws its route); each resolution read the card's
// brief with a regexp on every call. Reading each brief once (modelOf, route.go) means
// the deal parses exactly as many briefs as it walks ready cards, never twice that for
// the two passes that resolve one card. The reads are counted, with no clock (as
// TestTheTicksCheckSettlesTheRestsOnceAtScale counts the rests): the branch parsed a
// brief once in each pass over the ready cards, near 40,000 over 20,000 ready; reading
// once parses 20,000.
func TestTheTickDealReadsEachBriefOnceAtScale(t *testing.T) {
	s := loadedStore(t, 20000, 0, 8)
	ready := len(s.Work.Column(Ready))
	require.Equal(t, 20000, ready, "20,000 ready primaries the tick walks and deals")

	resetModelMemo()
	t.Cleanup(resetModelMemo)
	before := modelReads.Load()
	TickDeal(s, TickReq{})
	parsed := modelReads.Load() - before
	t.Logf("TICK-COST ready=%d briefs_parsed=%d", ready, parsed)
	require.EqualValues(t, ready, parsed,
		"the deal parses each ready brief's model once, not once per pass (the friends' dealTierOf, then the machines' routeOf) per card")
}

// BenchmarkTickDealAtLoad is the deal part's wall at the loaded store's scale: go test
// -bench TickDealAtLoad ./internal/sprint. The deal is the dominant part of the tick's
// drain and deal at load, and it runs 0.4 to 0.7 seconds on the loaded store (50,000
// ready beside 50,000 done), which is where the one-second gate is over without the
// memo: reading each brief's model lines twice a tick. It stays under the gate because
// each brief's model lines are read once (route.go, modelOf), never once per ready card
// per pass.
func BenchmarkTickDealAtLoad(b *testing.B) {
	for _, n := range []struct{ ready, done, friends int }{
		{20000, 20000, 0},
		{20000, 20000, 8},
		{50000, 50000, 0},
		{50000, 50000, 8},
	} {
		b.Run(fmt.Sprintf("ready=%d_done=%d_friends=%d", n.ready, n.done, n.friends), func(b *testing.B) {
			s := loadedStore(b, n.ready, n.done, n.friends)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resetModelMemo()
				TickDeal(s, TickReq{})
			}
		})
	}
}
