package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Automatic sentinels await ancestry rather than opening a coordinator's manual
// release gate. Preserve the ready/working/width counts of the old starvation
// fixture while checking that the proof-pending wave cannot advance without proof.
func TestTheTickDoesNotAskForManualReleaseOfAProofPendingWave(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 2}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 3}))
	h.must(AddStep(sprint.AddReq{Stream: "s2", IDs: []string{"s2-gate"}, Sentinel: true, Needs: []string{"s1-1"}}))
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 4}))
	h.startMachine()
	h.machine()
	require.Equal(t, 3, h.snap().Work.Count("s1", sprint.Ready))
	assert.Equal(t, 4, h.snap().Width("m1")+h.snap().Width("m2"))
	assert.Equal(t, 0, len(h.snap().Fleet.Column(sprint.Working)))
	assert.Empty(t, h.openOf(sprint.NStarving))
	h.machine()
	h.machine()
	assert.Equal(t, 0, h.snap().Work.Count("s1", sprint.Ready), "the three were dealt")
	assert.Equal(t, sprint.Waiting, h.state("s2-gate"))
	assert.Equal(t, 5, h.snap().Work.Count("s2", sprint.Waiting), "no manual gate or absent proof releases this wave")
	assert.Empty(t, h.openOf(sprint.NStarving))
	assert.Zero(t, h.written(sprint.NStarving))
	h.clean("proof-pending wave")
}
