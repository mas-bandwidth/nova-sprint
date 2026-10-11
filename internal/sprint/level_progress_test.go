package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLevelTargetNeedsAFreeLane changed for the owner's rule (2026-10-11
// ~19:05-19:20Z; tla/Level.tla, NeverOffAFreeLane): the target is the first
// member from the index that holds fewer cards than its width (a free lane),
// never one that only has room (below DealAhead times its width) and never a
// refuser. The former mean-and-gap test is gone with the mean rule.
func TestLevelTargetNeedsAFreeLane(t *testing.T) {
	t.Parallel()
	up := []string{"a", "b", "c"}
	held := map[string]int{"a": 4, "b": 3, "c": 1}
	lanes := map[string]int{"a": 4, "b": 4, "c": 2}
	for _, tc := range []struct {
		name  string
		held  map[string]int
		avoid []string
		want  string
	}{
		{"first free lane from the index", held, nil, "b"},
		{"refused free lane skipped", held, []string{"b"}, "c"},
		{"no member has a free lane", map[string]int{"a": 4, "b": 4, "c": 2}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRound(up, "0")
			to := r.levelTo(up, nil, tc.held, lanes, "a", tc.avoid)
			assert.Equal(t, tc.want, to)
			if to == "" {
				assert.Equal(t, uint64(0), r.count, "a refused move consumes no turn")
			} else {
				assert.Equal(t, to, indexPast(up, r.value()))
			}
		})
	}
}

// TestLevelStopsWhenNoMemberHasAFreeLane changed for the owner's rule
// (2026-10-11 ~19:05-19:20Z; tla/Level.tla, NeverOffAFreeLane): the former
// non-progress-target test is gone with the mean rule. Here every member holds
// at or above its width (a and b over 2, c full at 1), so the level has no
// free lane to move onto and moves nothing, whatever the newest card's refuser.
func TestLevelStopsWhenNoMemberHasAFreeLane(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 2, "a", "b")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "c", Width: 1}))
	putWorkCard(w, "newest", "a", Ready, 4, refusedBy("c"))
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
	assert.Empty(t, p.Units)
	after, afterHad := w.s.Fleet.Prop(PropDealIndex)
	assert.Equal(t, "a", w.s.Fleet.Card("newest.w1").Row)
	assert.Equal(t, "a", w.s.Fleet.Card("older.w1").Row)
	assert.Equal(t, before, after)
	assert.Equal(t, beforeHad, afterHad)
}
