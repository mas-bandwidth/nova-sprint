package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// syncWriter collects the loop's lines across goroutines (the loop writes while
// the test reads after synctest.Wait).
type syncWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// gcLoopApp is gcApp on the synctest bubble's clock with an inventory that
// answers as mode says (0: the config did not answer; 1: no machine row).
func gcLoopApp(t *testing.T, mode *atomic.Int32) (*app, string) {
	t.Helper()
	a, _, ai, _ := gcApp(t)
	a.now = time.Now
	if mode == nil {
		mode = &atomic.Int32{}
	}
	a.inventory = func(context.Context, string) ([]config.MachineWidth, error) {
		if mode.Load() == 1 {
			return nil, nil
		}
		return nil, errGcInventory
	}
	return a, ai
}

var errGcInventory = &gcInventoryErr{}

type gcInventoryErr struct{}

func (*gcInventoryErr) Error() string { return "the config store is away" }

// A finished job and its runner log: the local pass's rule removes the job and
// says a GC REMOVED detail line, which the loop leaves to the verb.
func gcLoopFinishedJob(t *testing.T, ai string) {
	t.Helper()
	now := time.Now()
	w := filepath.Join(ai, "buds", "b1", "working")
	done := filepath.Join(w, "jobs", "done")
	gcFile(t, filepath.Join(done, "JOB.md"), now, 5*time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(ai, "buds", "b1", "runner.log"),
		[]byte("t START done\nt END done model=m exit=0 report=Verdict: LAND\n"), 0o644))
}

// The loop says nothing before gcLoopFirst; at it, the local machine's first run
// is said with why=first, its class and summary lines prefixed with the time and
// the machine, and no removal detail line is said (gc.go gcLoop, "gcLoopFirst").
func TestTheGcLoopSaysTheFirstLocalRunAtItsFirstWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, ai := gcLoopApp(t, nil)
		gcLoopFinishedJob(t, ai)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var out syncWriter
		go a.gcLoop(ctx, &out)

		time.Sleep(gcLoopFirst - time.Second)
		synctest.Wait()
		assert.Empty(t, out.String(), "nothing runs before gcLoopFirst")

		time.Sleep(time.Second)
		synctest.Wait()
		s := out.String()
		assert.Regexp(t, `(?m)^\d\d:\d\d:\d\d GC machine=\S+ why=first: GC jobs count=1 `, s)
		assert.Contains(t, s, "GC OK freed=", "the summary line")
		assert.NotContains(t, s, "GC REMOVED", "a removal detail line is left to the verb")
		assert.NotContains(t, s, "GC WOULD-REMOVE")
		assert.NotContains(t, s, "GC KEPT")
	})
}

// After the first run the machine is not due again for sprint.GCDue's hour,
// though the loop wakes every gcLoopEvery (gc.go gcLoop, sprint.GCDue).
func TestTheGcLoopDoesNotRunTheLocalMachineTwiceInTheHour(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, ai := gcLoopApp(t, nil)
		gcLoopFinishedJob(t, ai)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var out syncWriter
		go a.gcLoop(ctx, &out)

		time.Sleep(gcLoopFirst)
		synctest.Wait()
		require.Equal(t, 1, strings.Count(out.String(), "freed="), "one run's summary")
		for i := 0; i < 3; i++ {
			time.Sleep(gcLoopEvery)
			synctest.Wait()
			assert.Equal(t, 1, strings.Count(out.String(), "freed="), "round %d", i+2)
		}
	})
}

// An inventory that errors is said once however many rounds it fails, and again
// only after a round where it answered (gc.go gcLoop, "saidInventory").
func TestTheGcLoopSaysAnUnreadInventoryOnceUntilItAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mode atomic.Int32
		a, _ := gcLoopApp(t, &mode)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var out syncWriter
		go a.gcLoop(ctx, &out)

		time.Sleep(gcLoopFirst)
		synctest.Wait()
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, 1, strings.Count(out.String(), "GC the config's machines were not read"),
			"said once however many rounds it fails")

		mode.Store(1)
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, 1, strings.Count(out.String(), "GC the config's machines were not read"),
			"a round that answered says nothing")

		mode.Store(0)
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, 2, strings.Count(out.String(), "GC the config's machines were not read"),
			"said again after a round that answered")
	})
}

// A cancelled context ends the loop with nothing more said (gc.go gcLoop,
// "case <-ctx.Done(): return").
func TestTheGcLoopEndsWhenItsContextIsCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, ai := gcLoopApp(t, nil)
		gcLoopFinishedJob(t, ai)
		ctx, cancel := context.WithCancel(context.Background())
		var out syncWriter
		done := make(chan struct{})
		go func() { a.gcLoop(ctx, &out); close(done) }()

		time.Sleep(gcLoopFirst)
		synctest.Wait()
		said := out.String()
		require.NotEmpty(t, said)

		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("the cancelled loop did not return")
		}

		time.Sleep(3 * gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, said, out.String(), "nothing more is said after the cancel")
	})
}
