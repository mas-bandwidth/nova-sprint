package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lineFreeWriter records each line written and whether the server's line of control was
// free as it was written.
type lineFreeWriter struct {
	mu    sync.Mutex
	a     *app
	buf   bytes.Buffer
	held  []string // lines written while the line was held
	lines []string
}

func (w *lineFreeWriter) Write(p []byte) (int, error) {
	free := w.a.serial.TryLock()
	if free {
		w.a.serial.Unlock()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		w.lines = append(w.lines, l)
		// the machine's state line is said once a change, before the tick's lines
		if !free && !strings.Contains(l, " machine ") {
			w.held = append(w.held, l)
		}
	}
	return w.buf.Write(p)
}

// sprintLines are the sprint lines among what was written ("<landed>/<all> ...").
func (w *lineFreeWriter) sprintLines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, l := range w.lines {
		if strings.Contains(l, "-> ETA") {
			out = append(out, l)
		}
	}
	return out
}

// A RUNNING tick says its lines (tick, TIMES, the sprint line) off the server's line of
// control, and the sprint line is made again only when the work table or the machine
// changed, or once a minute (sprintLineMemo; 2026-10-17: every tick, up to ten a second,
// held the line for its seven round trips).
func TestTheRunTickSaysItsLinesOffTheLineAndTheSprintLineOnChange(t *testing.T) {
	t.Parallel()
	w := newWakeApp(t)
	w.ok("init --readers reader-a,reader-b --members m1")
	w.ok("add --stream a --count 3")
	w.ok("start")
	st, _, code := w.a.machineVerb("run", nil, &bytes.Buffer{})
	require.NotNil(t, st, "run: %d", code)
	out := &lineFreeWriter{a: w.a}
	var errb bytes.Buffer
	quiet := func(d time.Duration) { w.a.sleep(d) }
	// tick 2 is woken by tick 1's own lines on the log and tick 5 by tick 4's (its deal);
	// the world's steps are the other waits
	w.world = []func(time.Duration){
		quiet, // tick 3: nothing changed
		func(time.Duration) { w.ok("add --stream b --count 2") }, // tick 4: the work table changed
		quiet, // tick 6: nothing changed since tick 4's line (its deal included)
		quiet, // tick 7
		func(time.Duration) { w.a.sleep(sprintLineEvery) }, // tick 8: a minute on
	}
	var before []int // the sprint lines said before each tick began
	w.a.ticked = func(int, time.Time, string) { before = append(before, len(out.sprintLines())) }
	w.a.runLoop(context.Background(), st, 20, 8, out, &errb)
	require.Empty(t, errb.String())
	require.Empty(t, out.held, "every tick line was said off the line:\n%s", out.buf.String())
	require.Len(t, before, 8, "eight ticks:\n%s", out.buf.String())
	require.Empty(t, w.world, "every step of the world was taken")
	// before tick k (1-based) began: the line is said on tick 1, on tick 4 (the add) and
	// on tick 8 (a minute on), never on a quiet tick
	require.Equal(t, []int{0, 1, 1, 1, 2, 2, 2, 2}, before, "lines said before each tick:\n%s", out.buf.String())
	require.Len(t, out.sprintLines(), 3, "and the last tick, a minute on, said it:\n%s", out.buf.String())
	require.Contains(t, out.sprintLines()[0], "0/3")
	require.Contains(t, out.sprintLines()[1], "0/5", "the line after the add says the new count")
	require.Contains(t, out.lines, "03:04:05 tick", "the ticks said their lines:\n%s", out.buf.String())
}

// The sprint line reads the machine and its heartbeat once, with the where facts' one
// exchange, never again by itself: its store reads are the facts' and the landing rate's.
func TestTheSprintLineReadsTheMachineWithTheWhereFacts(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream a --count 3")
	ta.ok("start")
	ta.ok("tick")
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	ctx := context.Background()
	kv := func() int { return ta.m.Calls["kv"] }
	before := kv()
	line := sprintLine(ctx, st)
	used := kv() - before
	require.Contains(t, line, "0/3 0.0% -> ETA -  machine: running", "the line is as it was")
	before = kv()
	st.LandingRate(ctx, nil, 0)
	rate := kv() - before
	require.Equal(t, 1+rate, used, "the line's machine reads are the where facts' one exchange and the landing rate's alone")
}
