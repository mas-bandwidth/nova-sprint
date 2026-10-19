package main

import (
	"context"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/bench"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cardWorkNice is the level a member card's work runs at on a bench: nova-tools
// cmd/nova-swarm lowers the card's child (a harness or the nova-sandbox) with
// `nice -n 19` on its gate line (native_nice_test.go), and the coordinator's reading
// of the 2026-10-18 bench is 10. The nova-tools half of this card
// (v17-swarm-card-work-nice-is-one-constant) settles the one constant; the gate only
// has to stay below it.
const cardWorkNice = 10

// The owner, 2026-10-18: landing is the highest priority in the sprint, above
// everything else. A gate run must never be niced down to the card work's level: its
// argv names the level explicitly, even at 0, so the tell is on the command line and
// in the fake runner.
func TestTheGateRunsAtAHigherPriorityThanCardWork(t *testing.T) {
	// not parallel: it stands in for bench.Run through the package seam
	oldRun := benchRun
	defer func() { benchRun = oldRun }()
	f := newBenchFullRig(t)
	b := f.r.a.landState()
	b.mu.Lock()
	gate := b.gateBench
	b.gateBench = nil // the hook short-circuits the bench; the seam sees the argv
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.gateBench = gate
		b.mu.Unlock()
	}()

	var got bench.Options
	benchRun = func(_ context.Context, _ bench.Transport, o bench.Options) (bench.Result, error) {
		got = o
		return bench.Result{}, nil
	}
	runs := [][]string{{"go", "build", "./..."}}
	script := gateScript(runs)
	_, _, err := f.l.runOnBench(t.Context(), f.hosts[0], f.r.clone, runs, false, &bench.MirrorStage{})
	require.NoError(t, err)
	assert.Equal(t, []string{"nice", "-n", strconv.Itoa(landGateNice), "sh", "-c", script}, got.Argv,
		"the gate's argv names its level, then the script")
	assert.Equal(t, 0, landGateNice, "a gate run is never niced down")
	assert.Less(t, landGateNice, cardWorkNice, "the gate outranks the card work it competes with")
	f.r.clean()
}

// The lander's own go runs in the clone take the same word, so no gate exec takes the
// level of whatever started it. goRun checks the run's own name for the gofmt branch
// before it wraps it.
func TestTheCloneGoRunsKeepTheirNice(t *testing.T) {
	t.Parallel()
	run := []string{"go", "test", "./..."}
	assert.Equal(t, []string{"nice", "-n", strconv.Itoa(landGateNice), "go", "test", "./..."}, cloneGoArgv(run),
		"the clone's go runs carry the gate's level")
	assert.Equal(t, run, []string{"go", "test", "./..."}, "the caller's run is not changed in place")
}
