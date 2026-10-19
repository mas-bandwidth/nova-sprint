package main

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/bench"
	"github.com/mas-bandwidth/nova-tools/pkg/subproc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cardWorkNice is the level a member card's work runs at on a bench, as measured on
// vision 2026-10-18 with `ps -o ni= -p <pid>`: nova-swarm and the card's nova-sandbox
// at ni 15, and their go children at ni 19. nova-tools cmd/nova-swarm lowers the
// card's child on its gate line (`nice -n 19`, native_nice_test.go); the nova-tools
// half of this card (v17-swarm-card-work-nice-is-one-constant) settles the one
// constant. The gate only has to stay below the card work's own level.
const cardWorkNice = 15

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
// level of whatever started it. The seam stands in for subproc.Prepare, so the test
// reads the command line goRun builds and starts nothing.
func TestTheCloneGoRunsKeepTheirNice(t *testing.T) {
	// not parallel: it stands in for subproc.Prepare through the package seam
	old := subprocPrepare
	defer func() { subprocPrepare = old }()
	var got []string
	subprocPrepare = func(ctx context.Context, _ time.Duration, name string, args ...string) subproc.Bounded {
		got = append([]string{name}, args...)
		return subproc.Prepare(ctx, landGoBudget, "nova-gate-nice-test-no-such-command")
	}
	run := []string{"go", "test", "./..."}
	l := &lander{}
	if _, err := l.goRun(t.Context(), t.TempDir(), run); err == nil {
		t.Fatal("the stand-in command must fail; nothing is started")
	}
	assert.Equal(t, []string{"nice", "-n", strconv.Itoa(landGateNice), "go", "test", "./..."}, got,
		"the clone's go runs carry the gate's level")
}
