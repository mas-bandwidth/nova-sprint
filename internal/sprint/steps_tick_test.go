package sprint

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

// A proof-pending cut keeps its wave waiting without a coordinator release
// judgment. Ready, working and width still obey the ordinary deal bounds.
func TestAutomaticSentinelDoesNotAskForAManualStarvationRelease(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2", Width: 2}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	producer := pendingSentinelTrunkFixture(w)
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"s2-gate"}, Sentinel: true, Needs: []string{producer}}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s2", Count: 4}))
	require.Len(t, w.s.Work.Column(Ready), 3)
	require.Equal(t, 4, w.s.Width("m1")+w.s.Width("m2"))
	p, _ := TickDeal(w.s, TickReq{})
	for _, n := range p.Notes {
		assert.NotEqual(t, NStarving, n.Type, "automatic cut has no manual release judgment")
	}
	w.must(p)
	require.Equal(t, 3, w.s.Fleet.Count("m1", Ready)+w.s.Fleet.Count("m2", Ready))
	require.NotEmpty(t, w.s.Fleet.Cell("m1", Ready))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 1}}))
	require.Equal(t, 1, w.s.Fleet.Count("m1", Working)+w.s.Fleet.Count("m2", Working))
	require.Empty(t, w.s.Work.Column(Ready))
	p2, _ := TickDeal(w.s, TickReq{})
	for _, n := range append(p2.Notes, p2.Updates...) {
		assert.NotEqual(t, NStarving, n.Type)
	}
	require.Equal(t, Waiting, w.s.StateOf("s2-gate"))
	require.Len(t, w.s.Work.Cell("s2", Waiting), 5, "cut and four cards still await actual trunk proof")
}

func TestAutomaticSentinelHasNoManualStarvationGateWhenAllWorkIsDealt(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2", Width: 2}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 2}))
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	require.Equal(t, 2, w.s.Fleet.Count("m1", Ready)+w.s.Fleet.Count("m2", Ready))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 1}}))
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: 1}}))
	require.Equal(t, 2, w.s.Fleet.Count("m1", Working)+w.s.Fleet.Count("m2", Working))
	require.Empty(t, w.s.Work.Column(Ready))
	require.Equal(t, 4, w.s.Width("m1")+w.s.Width("m2"))
	producer := pendingSentinelTrunkFixture(w)
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"s2-gate"}, Sentinel: true, Needs: []string{producer}}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s2", Count: 4}))
	p2, _ := TickDeal(w.s, TickReq{})
	for _, n := range append(p2.Notes, p2.Updates...) {
		assert.NotEqual(t, NStarving, n.Type)
	}
	require.Equal(t, Waiting, w.s.StateOf("s2-gate"))
	require.Len(t, w.s.Work.Cell("s2", Waiting), 5)
}
