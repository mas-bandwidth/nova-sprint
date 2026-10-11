package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The start-of-tick levelling moves a card only when its member cannot start
// it, and only to a member that can (the owner, 2026-10-11 ~19:05-19:20Z; the
// model card tla-level-moves-only-onto-a-free-lane; tla/Level.tla,
// NeverOffAFreeLane). A member whose work cards held (memberLoads) are at or
// below its width keeps every card dealt to it: it will take them at its next
// pass, so levelling it away is the wrong card moved.

// TestTheLevelLeavesADealtCardOnAnIdleMember (observation 4): batman width 4
// with 1 ready and 0 working, hetzner1 width 16 with 11 working. Neither holds
// more than its width, so the level moves nothing. The old mean rule moved
// batman's ready card to hetzner1; that line must fail.
func TestTheLevelLeavesADealtCardOnAnIdleMember(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "batman", Width: 4}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "hetzner1", Width: 16}))
	putWorkCard(w, "bat1", "batman", Ready, 1, nil)
	for i := 0; i < 11; i++ {
		putWorkCard(w, fmt.Sprintf("h%d", i), "hetzner1", Working, float64(i+2), nil)
	}
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	assert.Empty(t, p.Units, "an idle member's card must not be levelled away: %v", movedLines(p))
	assert.Equal(t, "batman", w.s.Fleet.Card("bat1.w1").Row)
}

// TestTheLevelMovesTheExcessOntoFreeLanesOnly: a width-4 member holding 4
// working and 6 ready (2 excess over its width); one member with 2 free lanes
// (held 2 of width 4) and one full member (held 5 of width 4, still under
// DealAhead times its width). Exactly 2 move, both onto the member with free
// lanes; none onto the full member, which has no lane to start them.
func TestTheLevelMovesTheExcessOntoFreeLanesOnly(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	for _, m := range []string{"busy", "lane", "full"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 4}))
	}
	for i := 0; i < 4; i++ {
		putWorkCard(w, fmt.Sprintf("busy-w%d", i), "busy", Working, float64(i+1), nil)
	}
	for i := 0; i < 6; i++ {
		putWorkCard(w, fmt.Sprintf("busy-r%d", i), "busy", Ready, float64(i+10), nil)
	}
	for i := 0; i < 2; i++ {
		putWorkCard(w, fmt.Sprintf("lane-w%d", i), "lane", Working, float64(i+30), nil)
	}
	for i := 0; i < 5; i++ {
		putWorkCard(w, fmt.Sprintf("full-w%d", i), "full", Working, float64(i+40), nil)
	}
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	levelHolds(t, w.s, p)
	require.Len(t, p.Units, 2, "the excess gives at most what a free lane can take: %v", movedLines(p))
	for _, u := range p.Units {
		assert.Contains(t, u.Moved, "-> lane:ready", "a card moved onto a member with no free lane: %s", u.Moved)
	}
	w.must(p)
	assert.Equal(t, map[string]int{"busy": 8, "full": 5, "lane": 4}, perMember(w.s))
}

// TestTheLevelTargetsRollTheIndex: one width-4 member holding 4 working and 4
// ready (4 excess), four width-2 members holding 1 working each (one free lane
// each). One card moves to each, in index order from deal_index.
func TestTheLevelTargetsRollTheIndex(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "src", Width: 4}))
	for _, m := range []string{"t1", "t2", "t3", "t4"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 2}))
	}
	for i := 0; i < 4; i++ {
		putWorkCard(w, fmt.Sprintf("src-w%d", i), "src", Working, float64(i+1), nil)
	}
	for i := 0; i < 4; i++ {
		putWorkCard(w, fmt.Sprintf("src-r%d", i), "src", Ready, float64(i+10), nil)
	}
	for _, m := range []string{"t1", "t2", "t3", "t4"} {
		putWorkCard(w, m+"-w", m, Working, 30, nil)
	}
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	levelHolds(t, w.s, p)
	var to []string
	for _, u := range p.Units {
		to = append(to, u.Changes[0].Entry.Move.Row)
	}
	assert.Equal(t, []string{"t1", "t2", "t3", "t4"}, to)
	w.must(p)
	assert.Equal(t, map[string]int{"src": 4, "t1": 2, "t2": 2, "t3": 2, "t4": 2}, perMember(w.s))
}
