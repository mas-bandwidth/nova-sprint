package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The deal fills a member with a free lane before one that has room but no
// lane (the-work-deal-fills-free-lanes-first-round-robin; the owner,
// 2026-10-11: "cards only may accrue per-fleet or friend, if there are not
// other machines/friends ready to do that work right now!"). A free lane is a
// load below the member's width (round.go, nextFree), and q is memberLoads
// (ready and working, a read at half a slot).

// working puts n working cards on member, one primary each, so its load is n.
func working(w *world, member string, n, from int) {
	w.t.Helper()
	for i := 0; i < n; i++ {
		putWorkCard(w, fmt.Sprintf("w-%d", from+i), member, Working, float64(from+i), nil)
	}
}

// readyWork puts n ready cards on member, one primary each.
func readyWork(w *world, member string, n, from int) {
	w.t.Helper()
	for i := 0; i < n; i++ {
		putWorkCard(w, fmt.Sprintf("r-%d", from+i), member, Ready, float64(from+i), nil)
	}
}

// primaries adds n ready primaries to stream s1.
func primaries(w *world, n int) {
	w.t.Helper()
	w.must(Add(w.s, AddReq{Stream: "s1", Count: n}))
}

// TestTheDealFillsTheIdleMemberBeforeAFullOne: hetzner2 holds nothing while b2
// and hetzner1 each have 16 working and nothing ready (at their width of 16:
// no free lane). One tick deals 16 ready primaries, all to hetzner2's free
// lanes; the next tick deals 6 more, none past DealAhead times a width, round
// the three in turn, two each.
func TestTheDealFillsTheIdleMemberBeforeAFullOne(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	for _, m := range []string{"b2", "hetzner1", "hetzner2"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 16}))
	}
	working(w, "b2", 16, 100)
	working(w, "hetzner1", 16, 200)

	primaries(w, 16)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 16}}))
	got := map[string]int{}
	for i := 1; i <= 16; i++ {
		c := w.s.Fleet.Card(WorkCardID(fmt.Sprintf("s1-%d", i), 1))
		require.NotNil(t, c, "s1-%d was dealt", i)
		got[c.Row]++
	}
	require.Equal(t, map[string]int{"hetzner2": 16}, got, "the 16 idle lanes take the 16 cards: %v", got)

	// the second tick: no member has a free lane (all at their width of 16), so
	// the room scan goes round the three from past hetzner2, two each and none
	// past DealAhead times a width
	primaries(w, 6)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 6}}))
	round := map[string]int{}
	for i := 17; i <= 22; i++ {
		c := w.s.Fleet.Card(WorkCardID(fmt.Sprintf("s1-%d", i), 1))
		require.NotNil(t, c, "s1-%d was dealt", i)
		require.Less(t, w.s.Fleet.Count(c.Row, Ready)+w.s.Fleet.Count(c.Row, Working), DealAhead*16+1, "s1-%d past a width", i)
		round[c.Row]++
	}
	require.Equal(t, map[string]int{"b2": 2, "hetzner1": 2, "hetzner2": 2}, round, "round the three, two each: %v", round)
}

// TestTheDealRollsOverFreeMembersFirst: four idle members of width 2 and two
// full ones. Four ready primaries go one to each idle member; the next four go
// one to each idle member again, the first on the member after the last one
// dealt, never all on the first by name and none on the full ones.
func TestTheDealRollsOverFreeMembersFirst(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	idle := []string{"m1", "m2", "m3", "m4"}
	for _, m := range idle {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 2}))
	}
	for i, m := range []string{"m5", "m6"} { // full: at DealAhead times their width, 4
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 2}))
		working(w, m, 4, 100+100*i)
	}
	primaries(w, 4)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	first := map[string]string{}
	for i := 1; i <= 4; i++ {
		c := w.s.Fleet.Card(WorkCardID(fmt.Sprintf("s1-%d", i), 1))
		require.NotNil(t, c, "s1-%d was dealt", i)
		first[c.Row] = c.ID
	}
	for _, m := range idle {
		require.Contains(t, first, m, "a card landed on the idle member %s: %v", m, first)
	}
	require.NotContains(t, first, "m5")
	require.NotContains(t, first, "m6")

	// the next four, from past m4: m4 was the last dealt, so the first is the
	// member after it that has a free lane; the full ones are skipped
	primaries(w, 4)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	second := map[string]int{}
	for i := 5; i <= 8; i++ {
		c := w.s.Fleet.Card(WorkCardID(fmt.Sprintf("s1-%d", i), 1))
		require.NotNil(t, c, "s1-%d was dealt", i)
		second[c.Row]++
	}
	require.Equal(t, map[string]int{"m1": 1, "m2": 1, "m3": 1, "m4": 1}, second, "one to each idle member again: %v", second)
}

// TestAHeldMembersCardsGoToFreeLanesFirst: hold (no --return) of a member with
// 6 ready cards, while one member is idle and two are full at their room: all
// 6 land on the idle member's free lanes.
func TestAHeldMembersCardsGoToFreeLanesFirst(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "idle", Width: 4}))
	for i, m := range []string{"fullA", "fullB"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 4}))
		working(w, m, 8, 100+100*i) // at DealAhead times the width, 8
	}
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "held", Width: 4}))
	readyWork(w, "held", 6, 300)

	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "held", Reason: "a test"}))
	got := map[string]int{}
	for i := 0; i < 6; i++ {
		c := w.s.Fleet.Card(fmt.Sprintf("r-%d.w1", 300+i))
		require.NotNil(t, c, "r-%d.w1 was dealt again", 300+i)
		require.Equal(t, Ready, c.Col)
		got[c.Row]++
	}
	require.Equal(t, map[string]int{"idle": 6}, got, "the held member's cards to the idle lanes: %v", got)
}
