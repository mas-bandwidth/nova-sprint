package sprint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TickGate is the tick's one-second gate: the machine ticks once a second while
// it runs (docs/SPEC-SPRINT.md, "The machine"), and the dashboard reads the
// store every second, so a tick that runs past a second is critical.
const TickGate = time.Second

// loadedStore is a copy of a loaded store's shape (docs/SPEC-SPRINT.md, "the
// machine"): two up members of width 32, two flash routes and one pro, `ready`
// ready flash primaries the tick walks and deals, `done` done work cards already
// on the fleet beside them, and `friends` up friends of the flash and pro classes
// the deal offers every ready primary to first. Each brief is distinct (a real
// card's is its own), so the deal reads every one's model lines, as the loaded
// store makes it do. With friends up the deal resolves a ready primary's tier once
// in the friends' pass (dealTierOf) and again in the machines' (routeServed), plus
// once per up member in the launch check (launchersOf): the several times a tick
// resolves one card, each of which once built a served map and a set, the deal's
// dominant cost at load.
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

// The deal resolves the model lines of every ready primary more than once a tick: once
// in the friends' pass (dealTierOf, which draws the tier a friend deals it on) and again
// in the machines' (routeServed, which draws its route), each of which read the card's
// brief with a regexp on every call before the memo. Reading each brief once (modelOf,
// route.go) means the deal parses exactly as many briefs as it walks ready cards, never
// once per pass that resolves one card. The reads are counted, with no clock (as
// TestTheTicksCheckSettlesTheRestsOnceAtScale counts the rests): the branch parsed a
// brief once in each pass over the ready cards, near 40,000 over 20,000 ready; reading
// once parses 20,000. This is the model read's share of the tick, a minor cost beside
// routeOf's allocations the gate test holds.
func TestTickDealReadsEachBriefOnceAtScale(t *testing.T) {
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

// The wall the gate names, not the count alone: the drain and the deal on the loaded
// store, timed, must stay under the one-second gate. The drain is the pump's first
// update, applying the queue the previous tick's parts wrote; at load the queue is
// empty (nothing has run yet), so the deal is the tick's cost. The deal's dominant
// cost at load is routeOf's per-card allocations: it built a served map and a set of
// route fields for every ready primary each tick (and again per member in the launch
// check), the allocations the GC spent near half the deal on. The fix builds served as
// an on-demand scan over the store's few routes (servedRoute) and skips the set in the
// check (routeServed), so the deal allocates near nothing per ready card; the store is
// sized at 80,000 ready so the unfixed deal runs over the gate and the fixed one stays
// under it (the base/head pair the test holds). The first (cold) tick parses every brief
// once into the model memo; the dashboard's steady-state tick is the deal over the warm
// memo, and it is the one measured against the gate, as the tick runs it (the memo is
// process-global and warm after the first tick; only a test clears it). The non-flaky
// hold is the wall itself, measured as the least of several steady ticks so a loaded
// machine's noise does not fail it.
func TestTickDrainAndDealUnderTheGateAtLoad(t *testing.T) {
	s := loadedStore(t, 80000, 80000, 8)
	require.Equal(t, 80000, len(s.Work.Column(Ready)), "80,000 ready primaries, the loaded store's scale")

	resetModelMemo()
	t.Cleanup(resetModelMemo)

	// the drain part at load: the queue is empty on the first tick, so the pump has
	// nothing to apply; measure it beside the deal to show which part the tick spends
	// in. A real queue is the previous tick's deal queued (QueueOf), measured below.
	drainStart := time.Now()
	Drain(s, s.Queue, MachineActor)
	drainEmpty := time.Since(drainStart)

	coldStart := time.Now()
	TickDeal(s, TickReq{})
	cold := time.Since(coldStart)

	steadiest := time.Duration(0)
	for i := 0; i < 5; i++ {
		start := time.Now()
		TickDeal(s, TickReq{})
		if d := time.Since(start); i == 0 || d < steadiest {
			steadiest = d
		}
	}

	// the previous tick's deal queued for this tick's drain: the busiest realistic
	// queue at this scale, still the pump's few dozen moves beside the deal's walk
	plan, _ := TickDeal(s, TickReq{})
	_, q := QueueOf(plan, DrainVerb, MachineActor)
	drainStart = time.Now()
	Drain(s, q, MachineActor)
	drainQueued := time.Since(drainStart)

	t.Logf("TICK-GATE ready=%d drain_empty=%s drain_queued(%d)=%s cold_first_tick=%s steady_deal=%s gate=%s",
		len(s.Work.Column(Ready)), drainEmpty, len(q), drainQueued, cold, steadiest, TickGate)
	require.LessOrEqual(t, steadiest, TickGate, "the deal on the loaded store stays under the one-second gate (steady state)")
	require.Less(t, drainQueued, steadiest, "the drain is not the tick's dominant cost: the deal is")
}

// BenchmarkTickDealAtLoad is the deal part's wall at the loaded store's scale: go test
// -bench TickDealAtLoad ./internal/sprint. The deal is the dominant part of the tick's
// drain and deal at load. It stays under the gate because routeOf no longer builds a
// served map and a set of route fields for every ready primary each tick (route.go,
// servedRoute and routeServed), the allocations that once drove the GC; the model memo
// (modelOf) is warmed once before the timer, as the tick warms it on its first run, so
// the number is the steady-state deal the dashboard pays each second, never the cold
// first parse.
func BenchmarkTickDealAtLoad(b *testing.B) {
	for _, n := range []struct{ ready, done, friends int }{
		{20000, 20000, 0},
		{20000, 20000, 8},
		{50000, 50000, 0},
		{50000, 50000, 8},
		{80000, 80000, 8},
	} {
		b.Run(fmt.Sprintf("ready=%d_done=%d_friends=%d", n.ready, n.done, n.friends), func(b *testing.B) {
			s := loadedStore(b, n.ready, n.done, n.friends)
			resetModelMemo()
			TickDeal(s, TickReq{}) // warm the memo once, as the tick does on its first run
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				TickDeal(s, TickReq{})
			}
		})
	}
}

// BenchmarkTickDrainAtLoad is the drain part's wall beside the deal's: go test -bench
// TickDrainAtLoad ./internal/sprint. The drain applies the queue the previous tick's
// parts wrote; at load the queue is empty, and even the busiest realistic queue (the
// previous tick's deal, queued) is a few dozen moves. It is orders under the deal, the
// tick's dominant cost, which BenchmarkTickDealAtLoad measures.
func BenchmarkTickDrainAtLoad(b *testing.B) {
	for _, n := range []struct{ ready, done, friends int }{
		{20000, 20000, 8},
		{50000, 50000, 8},
	} {
		b.Run(fmt.Sprintf("ready=%d_done=%d_friends=%d", n.ready, n.done, n.friends), func(b *testing.B) {
			s := loadedStore(b, n.ready, n.done, n.friends)
			resetModelMemo()
			TickDeal(s, TickReq{})
			plan, _ := TickDeal(s, TickReq{})
			_, q := QueueOf(plan, DrainVerb, MachineActor)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Drain(s, q, MachineActor)
			}
		})
	}
}
