package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLevelTargetPicksAFreeLane pins the owner's target rule (2026-10-11
// ~19:05-19:20Z): the level moves a card onto the next member from the deal's
// index with a free lane (held below its width) that did not refuse it, never
// onto a member at or above its width and never by the mean (round.levelTo,
// tla/Level.tla NeverOffAFreeLane (lands with #127)). The mean rule and the gap of two are gone.
func TestLevelTargetPicksAFreeLane(t *testing.T) {
	t.Parallel()
	up := []string{"a", "b", "c"}
	for _, tc := range []struct {
		name  string
		held  map[string]int
		lanes map[string]int
		avoid []string
		want  string
	}{
		{"next below its width", map[string]int{"a": 5, "b": 3, "c": 4}, map[string]int{"a": 4, "b": 4, "c": 4}, nil, "b"},
		{"refused free lane skipped", map[string]int{"a": 5, "b": 4, "c": 2}, map[string]int{"a": 4, "b": 4, "c": 4}, []string{"c"}, ""},
		{"no free lane", map[string]int{"a": 9, "b": 4, "c": 4}, map[string]int{"a": 4, "b": 4, "c": 4}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRound(up, "0")
			to := r.levelTo(up, tc.held, tc.held, tc.lanes, "a", tc.avoid)
			assert.Equal(t, tc.want, to)
			if to == "" {
				assert.Equal(t, uint64(0), r.count, "a refused move consumes no turn")
			} else {
				assert.Equal(t, to, indexPast(up, r.value()))
			}
		})
	}
}

// TestLevelStopsWhenNoFreeLaneRemains pins that the call ends: with every
// member at or above its width (a and b over it, c full) there is no free lane,
// so the level moves nothing and returns (round.levelTo; tla/Level.tla, MovesBounded, and NeverOffAFreeLane, which
// lands with #127).
func TestLevelStopsWhenNoFreeLaneRemains(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 2, "a", "b")
	w.setWidth("c", 1)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "c"}))
	putWorkCard(w, "newest", "a", Ready, 4, nil)
	putWorkCard(w, "older", "a", Ready, 3, nil)
	for _, member := range []string{"a", "b"} {
		for _, suffix := range []string{"1", "2"} {
			putWorkCard(w, member+suffix, member, Working, 1, nil)
		}
	}
	putWorkCard(w, "bready", "b", Ready, 2, nil)
	putWorkCard(w, "c1", "c", Working, 1, nil)
	before, beforeHad := w.s.Fleet.Prop(PropDealIndex)
	p := FleetStep(w.s, FleetReq{Op: "level"})
	assert.Empty(t, p.Units, "the level moved a card with no free lane: %v", movedLines(p))
	w.must(p)
	assert.Equal(t, "a", w.s.Fleet.Card("newest.w1").Row)
	assert.Equal(t, "a", w.s.Fleet.Card("older.w1").Row)
	after, afterHad := w.s.Fleet.Prop(PropDealIndex)
	assert.Equal(t, before, after)
	assert.Equal(t, beforeHad, afterHad)
}
