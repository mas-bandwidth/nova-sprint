package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldRunOutput leaves every underlying Write blocked until released. Inside
// synctest, Wait proves the RUN and BALANCE goroutines have both reached their
// blocking point, without a wall-clock sleep or a lucky scheduler overlap.
type heldRunOutput struct {
	entered chan struct{}
	release chan struct{}
	lines   chan string
	active  atomic.Int32
	peak    atomic.Int32
}

func (w *heldRunOutput) Write(p []byte) (int, error) {
	n := w.active.Add(1)
	for was := w.peak.Load(); n > was; was = w.peak.Load() {
		if w.peak.CompareAndSwap(was, n) {
			break
		}
	}
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	w.lines <- string(p)
	w.active.Add(-1)
	return len(p), nil
}

func TestRunSerializesBalanceAndTickOutput(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 3")
	ta.ok("start")
	exe := filepath.Join(t.TempDir(), "nova-sprint")
	require.NoError(t, os.WriteFile(exe, []byte("original build"), 0o755))
	ta.a.executable = func() (string, error) { return exe, nil }
	ta.a.ticked = func(int, time.Time, string) {
		require.NoError(t, os.WriteFile(exe, []byte("replacement build, longer"), 0o755))
	}
	var out string
	synctest.Test(t, func(t *testing.T) {
		w := &heldRunOutput{entered: make(chan struct{}, 1), release: make(chan struct{}), lines: make(chan string, 100)}
		done := make(chan int, 1)
		go func() {
			done <- runInto(ta.a, ta.withEpoch([]string{"run", "--tick-deadline", "0"}), w, io.Discard)
		}()
		<-w.entered
		synctest.Wait()
		// Both startup writers are blocked now: on the underlying writer for
		// the first, and on runOutput's write turn for the other. Without runOutput
		// both entered the underlying writer and peak is two.
		assert.Equal(t, int32(1), w.active.Load(), "RUN and BALANCE must not enter stdout together")
		close(w.release)
		assert.Equal(t, exitReplaced, <-done)
		synctest.Wait()
		assert.Equal(t, int32(0), w.active.Load(), "no balance writer remains after run returns")
		assert.Equal(t, int32(1), w.peak.Load())
		close(w.lines)
		var lines []string
		for line := range w.lines {
			lines = append(lines, line)
		}
		out = strings.Join(lines, "")
	})
	assert.Contains(t, out, "BALANCE every ")
	assert.Contains(t, out, "RUN ticking ")
	assert.Contains(t, out, "RUN STOP ")
}

// A returning run hands its ordinary unsynchronized buffer back to its caller.
// The balance loop must have stopped before these reads and the next call.
func TestRunReturnsAfterItsBalanceOutput(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("start")
	code, out, errs := runOnce(t, ta, "--tick-deadline", "0")
	assert.Equal(t, exitReplaced, code, out+errs)
	assert.Contains(t, out, "BALANCE every ")
}

// A given-up tick may retain the line until process exit. Ending run must
// still finish its balance poll rather than wait on that tick forever.
func TestBalancePollStopsWhileTickHoldsControlLine(t *testing.T) {
	t.Parallel()
	ta, _, _ := balanceApp(t, sprint.Route{Name: "flash-or", Tier: "flash", Provider: "openrouter", Model: "m", Enabled: true})
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ta.a.serial.Lock()
	defer ta.a.serial.Unlock()
	waiting := make(chan struct{})
	ta.a.serial.waiting = func() { close(waiting) }
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		ta.a.pollBalances(ctx, st, &out)
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the balance poll never reached the tick's held control line")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled balance poll waited on a wedged tick")
	}
	assert.Empty(t, out.String(), "a canceled poll never acquired the line or wrote a balance")
}
