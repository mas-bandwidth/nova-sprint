package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergingNags is the plan's merge-queue nags.
func mergingNags(p Plan) []Nudge {
	var out []Nudge
	for _, n := range p.Nudges {
		if n.Card == MergingNagCard {
			out = append(out, n)
		}
	}
	return out
}

// withCoordinator is the sim with carol hooked in as coordinator, on the bus.
func withCoordinator(s *sim) {
	s.must("coord", store.PutRow{Name: "carol", Session: "hook-carol", Role: "coordinator"})
	s.must("coord up", store.SetRowState{Row: "carol", State: "up"})
}

// TestMergingNagFromTheThreshold: a card merging up to timeouts merging_nag
// nags no one; past it, every tick pushes the coordinator one nudge with
// the count, the oldest card and its age, and the command that clears it
// (its hand landing while no judgement is open), every merging_nag_every;
// the nag stops the tick the card lands. The clock is the world's.
func TestMergingNagFromTheThreshold(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	withCoordinator(s)
	req := toMerging(s)
	assert.Empty(t, mergingNags(s.tick()), "merging a few seconds: no nag")
	s.now = s.now.Add(seconds(Engine.Timeouts.MergingNag) - 10*time.Second)
	assert.Empty(t, mergingNags(s.tick()), "not yet past merging_nag")
	s.now = s.now.Add(10 * time.Second)
	ns := mergingNags(s.tick())
	require.Len(t, ns, 1, "past merging_nag: one nudge")
	n := ns[0]
	assert.Equal(t, "carol", n.Row)
	assert.Equal(t, time.Duration(Engine.Timeouts.MergingNagEvery)*time.Second, n.Every)
	assert.True(t, strings.HasPrefix(n.Line, "MERGING 1 cards merging longer than 5m0s; oldest p1 merging "), n.Line)
	assert.Contains(t, n.Line, "with no land judgement")
	assert.Contains(t, n.Line, "run: nova-sprint land p1 --head "+req.Heads[0]+" --reason \"<why>\"")
	assert.Len(t, mergingNags(s.tick()), 1, "and every tick after while it is past")
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "landed", Landed: req.Heads, Merge: sha(9), Main: true})
	s.tick()
	s.requireState("p1", "landed")
	assert.Empty(t, mergingNags(s.tick()), "cleared: the nag stops")
}

// TestMergingNagOneLineForTheQueue: two cards past the threshold are one
// nudge naming the count and the oldest, with the open land judgement's
// own command when the oldest is under one.
func TestMergingNagOneLineForTheQueue(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	withCoordinator(s)
	s.stream("s1")
	s.row("r1", 4, "pro")
	s.row("r2", 4, "pro", "heavy")
	for _, p := range []string{"p1", "p2"} {
		s.producer(p, "s1", "pro", "a/"+p)
	}
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(2)
	first := s.request("land", "s1")
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "ci_red", Reason: "failing unit"})
	s.tick()
	require.Equal(t, []string{"land.p1.1"}, s.openJudgements("land"))
	s.now = s.now.Add(60 * time.Second)
	s.take("p2.w1")
	s.report("p2.w1", "ok", sha(2), "done")
	s.tick()
	s.take("p2.r1.1")
	s.report("p2.r1.1", "ok", sha(2), "fine")
	s.run(2)
	s.requireState("p2", "merging")
	s.now = s.now.Add(seconds(Engine.Timeouts.MergingNag))
	ns := mergingNags(s.tick())
	require.Len(t, ns, 1, "one line for the whole queue, never one per card")
	assert.True(t, strings.HasPrefix(ns[0].Line, "MERGING 2 cards merging longer than 5m0s; oldest p1 merging "), ns[0].Line)
	assert.Contains(t, ns[0].Line, "under land judgement land.p1.1; run: nova-sprint judge land.p1.1 <choice>")
}

// TestMergingNagNeedsTheCoordinatorOnTheBus: with no coordinator on the
// bus there is no nudge (the bus would queue one a second for nobody);
// check's red line says it instead.
func TestMergingNagNeedsTheCoordinatorOnTheBus(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	toMerging(s)
	s.now = s.now.Add(seconds(Engine.Timeouts.MergingNag) + time.Second)
	assert.Empty(t, mergingNags(s.tick()), "no coordinator: no nudge")
	withCoordinator(s)
	s.away = map[string]struct{}{"carol": {}}
	assert.Empty(t, mergingNags(s.tick()), "coordinator off the bus: no nudge")
	s.away = nil
	assert.Len(t, mergingNags(s.tick()), 1)
}
