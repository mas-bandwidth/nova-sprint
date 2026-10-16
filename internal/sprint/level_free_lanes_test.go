package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memberWidth is a member and its width for a test's fleet.
type memberWidth struct {
	name  string
	width int
}

// upWidths ups each member at its width, in the order given.
func upWidths(t *testing.T, w *world, spec ...memberWidth) {
	t.Helper()
	for _, m := range spec {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m.name, Width: m.width}))
	}
}

// dPutN puts n work cards <member>-<col>-<i> on the member in the column, scored from score.
func dPutN(w *world, member, col string, n int, score float64, extra map[string]string) {
	for i := 0; i < n; i++ {
		putWorkCard(w, fmt.Sprintf("%s-%s-%d", member, col, i), member, col, score+float64(i), extra)
	}
}

// The owner's rule (2026-10-11 ~19:05-19:20Z): a card moves only when its
// member cannot start it. batman (width 4) holds one ready card it can start,
// hetzner1 (width 16) holds eleven working cards and is below its width, so no
// member is over its width: the level moves nothing. Under the mean rule it
// moved batman's card to hetzner1 (tla/Level.tla NeverOffAFreeLane (lands with #127)).
func TestTheLevelLeavesADealtCardOnAnIdleMember(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	upWidths(t, w, memberWidth{"batman", 4}, memberWidth{"hetzner1", 16})
	putWorkCard(w, "batman-ready", "batman", Ready, 1, nil)
	dPutN(w, "hetzner1", Working, 11, 10, nil)
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	assert.Empty(t, p.Units, "the level moved a card off a member that can start it: %v", movedLines(p))
	w.must(p)
	assert.Equal(t, "batman", w.s.Fleet.Card("batman-ready.w1").Row)
}

// The level moves the excess of a member over its width onto members with free
// lanes only: a source (width 4, four working and six ready: held 10, excess 6)
// fills the one member with two free lanes (width 4, held 2) and never touches
// the full member (width 4, held 6: over its width but under DealAhead times
// it, so no free lane). Exactly two cards move.
func TestTheLevelMovesTheExcessOntoFreeLanesOnly(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	upWidths(t, w, memberWidth{"source", 4}, memberWidth{"free", 4}, memberWidth{"full", 4})
	dPutN(w, "source", Working, 4, 0, nil)
	dPutN(w, "source", Ready, 6, 10, nil)
	dPutN(w, "free", Working, 2, 20, nil)
	dPutN(w, "full", Working, 6, 30, nil)
	w.s.Fleet.SetProps(map[string]string{PropDealIndex: "source"})
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	require.Len(t, p.Units, 2, "the level moved %d cards, want 2: %v", len(p.Units), movedLines(p))
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Entry.Move != nil {
				assert.Equal(t, "free", ch.Entry.Move.Row, "%s moved to %q, not the member with free lanes", u.Key, ch.Entry.Move.Row)
			}
		}
	}
	w.must(p)
	assert.Equal(t, map[string]int{"source": 8, "free": 4, "full": 6}, perMember(w.s))
}

// The target rolls the deal's index: a source (width 4, four working and six
// ready: excess 6) gives one card to each of four members with one free lane,
// in index order from the deal's cursor past the source, so every free lane
// takes its card once (round.go, nextLane from the deal's index). Then no free
// lane is left: the source keeps its last two cards, and the full member f
// (width 4, four working: no free lane, but room under DealAhead times its
// width) is never a target. Under the mean rule the fifth card went to f.
func TestTheLevelTargetsRollTheIndex(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	upWidths(t, w, memberWidth{"a", 4}, memberWidth{"b", 4}, memberWidth{"c", 4}, memberWidth{"d", 4}, memberWidth{"e", 4}, memberWidth{"f", 4})
	dPutN(w, "a", Working, 4, 0, nil)
	dPutN(w, "a", Ready, 6, 10, nil)
	for _, m := range []string{"b", "c", "d", "e"} {
		dPutN(w, m, Working, 3, 20, nil)
	}
	dPutN(w, "f", Working, 4, 30, nil)
	// the index just past a: the first scan starts at b.
	w.s.Fleet.SetProps(map[string]string{PropDealIndex: "a"})
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	var to []string
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Entry.Move != nil {
				to = append(to, ch.Entry.Move.Row)
			}
		}
	}
	assert.Equal(t, []string{"b", "c", "d", "e"}, to, "the level did not roll the index over the free lanes alone: %v", movedLines(p))
	w.must(p)
	assert.Equal(t, map[string]int{"a": 6, "b": 4, "c": 4, "d": 4, "e": 4, "f": 4}, perMember(w.s))
}

// A source none of whose cards has a target does not end the level: the most
// over member (width 2, two working and three ready, every ready card refused at
// staging by the one member with a free lane) gives nothing, and the less over
// member (width 2, two working and one ready) gives its card to that free lane.
// Exactly that card moves (tla/Level.tla ChoiceFree, NoExcessWhileALaneIsFree,
// which land with #127). Before, the call ended at the first source it chose.
func TestTheLevelSkipsASourceWithNoMovableCard(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	upWidths(t, w, memberWidth{"over", 2}, memberWidth{"less", 2}, memberWidth{"free", 4})
	dPutN(w, "over", Working, 2, 0, nil)
	dPutN(w, "less", Working, 2, 10, nil)
	dPutN(w, "over", Ready, 3, 20, refusedBy("free"))
	putWorkCard(w, "less-ready", "less", Ready, 30, nil)
	putWorkCard(w, "free-working", "free", Working, 40, nil)
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	levelHolds(t, w.s, p)
	require.Equal(t, []string{"less-ready.w1 less:ready -> free:ready gen=3"}, movedLines(p), "the level ended at a source with no movable card")
	w.must(p)
	assert.Equal(t, map[string]int{"over": 5, "less": 2, "free": 2}, perMember(w.s))
}
