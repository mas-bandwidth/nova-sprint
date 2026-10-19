package main

import (
	"context"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShutdownCancelsLandGate pins that a cancelled land context reaches the gate's
// runner and ends the stage: shutdown is a cancelled context, not a wait for the run.
func TestShutdownCancelsLandGate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	sawCancel := make(chan struct{})
	run := func(ctx context.Context, dir string, run []string) (string, error) {
		close(started)
		<-ctx.Done()
		close(sawCancel)
		return "", ctx.Err()
	}
	stages := []gateStage{{Stage: 0, Runs: []gateRun{{Run: []string{"go", "test", "./..."}}}}}
	done := make(chan string, 1)
	go func() { done <- (&lander{}).runGateStages(ctx, t.TempDir(), stages, run) }()
	<-started
	cancel()
	why := <-done
	<-sawCancel
	assert.NotEmpty(t, why, "the stage reports the cancelled run")
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

// TestGateGroupEndedAfterGrace pins the group's signal sequence: SIGTERM to the whole
// group at once, and SIGKILL to it only after gateKillGrace, never when the group is
// gone before. The clock starts at 1000.0 seconds; no process and no real time.
func TestGateGroupEndedAfterGrace(t *testing.T) {
	old := gateGroupSignal
	t.Cleanup(func() { gateGroupSignal = old })

	const pgid = 4242
	for _, c := range []struct {
		name  string
		there bool
		want  []syscall.Signal
	}{
		{"the group is still there at the grace", true, []syscall.Signal{syscall.SIGTERM, 0, syscall.SIGKILL}},
		{"the group is gone before the grace", false, []syscall.Signal{syscall.SIGTERM, 0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var sent []syscall.Signal
			gateGroupSignal = func(pid int, sig syscall.Signal) error {
				assert.Equal(t, -pgid, pid, "the signal goes to the group")
				mu.Lock()
				sent = append(sent, sig)
				mu.Unlock()
				if sig == 0 && !c.there {
					return syscall.ESRCH
				}
				return nil
			}
			clock := 1000.0
			var waited time.Duration
			timer := make(chan time.Time, 1)
			after := func(d time.Duration) <-chan time.Time {
				waited = d
				return timer
			}
			done, err := gateGroupEnd(pgid, after)
			require.NoError(t, err)
			mu.Lock()
			early := append([]syscall.Signal(nil), sent...)
			mu.Unlock()
			assert.Equal(t, []syscall.Signal{syscall.SIGTERM}, early, "SIGTERM goes at once, before the grace")
			require.NotNil(t, done)
			clock += gateKillGrace.Seconds()
			timer <- time.Unix(int64(clock), 0)
			<-done
			mu.Lock()
			got := append([]syscall.Signal(nil), sent...)
			mu.Unlock()
			assert.Equal(t, c.want, got)
			assert.Equal(t, gateKillGrace, waited)
			assert.Equal(t, 1000.0+gateKillGrace.Seconds(), clock)
		})
	}
}
