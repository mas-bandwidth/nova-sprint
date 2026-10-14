package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusedBy is a work card's fields when the member refused it at staging.
func refusedBy(member string) map[string]string {
	return map[string]string{FieldStagingTake + "1": ProviderTake{Member: member}.String()}
}

// wedgeWorld is the fleet of the wedge of 2026-10-02 (nova-tools#5122, the
// diagnosis's reproduction): four members up in the order A, T, S, U. A (width
// 3) holds 3 working and 3 ready, its newest refused at staging by S; T (3)
// holds 3 working and 2 ready; S (3) holds 3 working and 1 ready, the emptiest
// open member; U (1) holds 5 working and no ready, over its room, raising the
// mean.
func wedgeWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	for _, m := range []struct {
		name  string
		width int
	}{{"A", 3}, {"T", 3}, {"S", 3}, {"U", 1}} {
		w.setWidth(m.name, m.width)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m.name}))
	}
	score := 1.0
	put := func(member, col string, n int) {
		for i := 0; i < n; i++ {
			putWorkCard(w, fmt.Sprintf("%s-%s-%d", member, col, i), member, col, score, nil)
			score++
		}
	}
	put("A", Working, 3)
	put("A", Ready, 2)
	putWorkCard(w, "A-refused", "A", Ready, 99, refusedBy("S"))
	put("T", Working, 3)
	put("T", Ready, 2)
	put("S", Working, 3)
	put("S", Ready, 1)
	put("U", Working, 5)
	return w
}

// levelHolds says what every level plan must: no card moved twice, no more
// moves than the fleet had ready cards, no card onto a member that refused it
// at staging (tla/Level.tla: MovesBounded, NeverOnARefuser).
func levelHolds(t *testing.T, s *Snapshot, p Plan) {
	t.Helper()
	ready := 0
	for _, m := range s.UpMembers() {
		ready += s.Fleet.Count(m, Ready)
	}
	assert.LessOrEqual(t, len(p.Units), ready, "more moves than ready cards: %v", movedLines(p))
	seen := map[string]bool{}
	for _, u := range p.Units {
		assert.False(t, seen[u.Key], "%s moved twice in one plan: %v", u.Key, movedLines(p))
		seen[u.Key] = true
		for _, ch := range u.Changes {
			if ch.Entry.Move != nil {
				assert.NotContains(t, StagingRefusers(s.Fleet.Card(u.Key)), ch.Entry.Move.Row, "%s moved onto a member that refused it", u.Key)
			}
		}
	}
}

func movedLines(p Plan) []string {
	var out []string
	for _, u := range p.Units {
		out = append(out, u.Moved)
	}
	return out
}

// The wedge's shape under the free-lane rule: every member is at or above its
// width (A 6, T 5, S 4, U 5), so no member has a free lane and the level moves
// nothing, however long the queues (round.levelTo, nextLane; tla/Level.tla
// NeverOffAFreeLane (lands with #127)). The card that the mean rule moved to S stays on A.
func TestLevelLeavesTheWedgeWhenNoLaneIsFree(t *testing.T) {
	t.Parallel()
	w := wedgeWorld(t)
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	levelHolds(t, w.s, p)
	assert.Empty(t, movedLines(p), "the level moved a card with no free lane")
	w.must(p)
	assert.Equal(t, "A", w.s.Fleet.Card("A-refused.w1").Row, "the refused card stays")
	assert.Equal(t, "A", w.s.Fleet.Card("A-ready-1.w1").Row, "no free lane, so no move")
}

// The call ends under a target that never takes a card when no free lane
// exists, on the wedge's shape: the ready count, not the mean, bounds the
// calls, and a card the call moved is never queued again (tla/Level.tla,
// MCLevelOldRuleNoRequeue). The target counts its calls so that a loop that
// does not end fails here, not in the heap.
func TestLevelEndsWithNoFreeLaneWithinTheReadyCount(t *testing.T) {
	t.Parallel()
	w := wedgeWorld(t)
	asked := 0
	wedge := func(r *round, up []string, n, held, widths map[string]int, from string, avoid []string) string {
		asked++
		require.LessOrEqual(t, asked, 100, "the level asked for a target %d times: it does not end", asked)
		return wedgeLevelTo(r, up, n, held, widths, from, avoid)
	}
	var p Plan
	up := w.s.UpMembers()
	levelWith(w.s, &p, up, dealRound(w.s), roundMoves{}, nil, wedge)
	levelHolds(t, w.s, p)
	assert.Empty(t, p.Units, "the wedge has no free lane")
}

// wedgeLevelTo is round.levelTo as it was at b7776ca3: the longest avoided
// with the card's refusers, any open member at or below the mean a target, no
// gap.
func wedgeLevelTo(r *round, up []string, n, held, widths map[string]int, from string, avoid []string) string {
	avoid = append(append([]string(nil), avoid...), from)
	total := 0
	for _, m := range up {
		total += n[m]
	}
	mean := total / len(up)
	if total < 0 && total%len(up) != 0 {
		mean--
	}
	open := func(x string) bool { return contains(up, x) && held[x] < widths[x] && !contains(avoid, x) }
	to := r.scan(func(x string) bool { return open(x) && n[x] < mean })
	if to == "" {
		to = r.scan(func(x string) bool { return open(x) && n[x] <= mean })
	}
	if to != "" {
		r.moved(to)
	}
	return to
}

// A card the call moved is not queued again: three ready cards on b (width 1),
// b2 refused at staging by c, so the source b gives its two excess cards to the
// one free lane a they may take (b2 may not go to c), and b1 stays. No card
// moves twice, and no more cards move than b is over its width (tla/Level.tla,
// MovesBounded). The shape is the one TLC found (MCLevelBrokenRequeue).
func TestLevelMovesNoCardTwice(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	for _, m := range []struct {
		name  string
		width int
	}{{"c", 3}, {"a", 2}, {"b", 1}} {
		w.setWidth(m.name, m.width)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m.name}))
	}
	putWorkCard(w, "b1", "b", Ready, 1, nil)
	putWorkCard(w, "b2", "b", Ready, 2, refusedBy("c"))
	putWorkCard(w, "b3", "b", Ready, 3, nil)
	p := FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor})
	levelHolds(t, w.s, p)
	assert.Equal(t, []string{"b3.w1 b:ready -> a:ready gen=3", "b2.w1 b:ready -> a:ready gen=3"}, movedLines(p))
	w.must(p)
	assert.Equal(t, map[string]int{"a": 2, "b": 1, "c": 0}, perMember(w.s))
}
