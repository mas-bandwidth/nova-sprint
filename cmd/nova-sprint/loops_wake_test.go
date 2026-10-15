package main

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/decide"
)

// The server's land loop, seat-silent look and decide lane wake on the sprint log
// (tla/LoopWake.tla): a line of the log runs each within one event, the clock never
// stepped, and the interval is the quiet log's backstop, which also catches a push that
// was lost. Time is the test app's fake clock and the store its mem twin: nothing here
// sleeps, listens or reaches a network.

// lossyLog is a backend whose log wait is scripted: the transport of the wake. A wait
// the script says nothing about is the mem twin's own.
type lossyLog struct {
	store.Backend
	wait *func(d time.Duration) (woke bool, delegate bool)
	mu   *sync.Mutex
}

func (l lossyLog) AtEpoch(epoch uint64, old bool) store.Backend {
	return lossyLog{Backend: l.Backend.AtEpoch(epoch, old), wait: l.wait, mu: l.mu}
}

func (l lossyLog) WaitLog(ctx context.Context, after string, d time.Duration) (string, bool, error) {
	l.mu.Lock()
	f := *l.wait
	l.mu.Unlock()
	if f != nil {
		if _, delegate := f(d); !delegate {
			return after, false, nil // the push was dropped: nothing woke the wait
		}
	}
	return l.Backend.(store.LogWaiter).WaitLog(ctx, after, d)
}

// loopCase is one loop: how to start it on a rig, and how many passes it has made.
type loopCase struct {
	name     string
	interval time.Duration
	// start runs the loop on the calling goroutine until ctx ends; passes counts its
	// passes so far; line commits one line of the log and what the pass will see.
	start  func(ctx context.Context, ta *testApp)
	passes func() int
	line   func(ta *testApp)
	// queue hands the loop work that no line of the log announces (the decide lane's finish)
	queue func(ta *testApp)
}

func loopCases(t *testing.T) []func(ta *testApp) loopCase {
	return []func(ta *testApp) loopCase{
		// the land loop: a card queued to merge, and a pass is a read of the merge queue
		func(ta *testApp) loopCase {
			var mu sync.Mutex
			reads := 0
			b := ta.a.landState()
			b.mu.Lock()
			b.queueSeam = func(context.Context, string) (int, string, string, error) {
				mu.Lock()
				defer mu.Unlock()
				reads++
				return 0, "", "", nil
			}
			b.mu.Unlock()
			return loopCase{
				name: "land", interval: LandEvery,
				start: func(ctx context.Context, ta *testApp) { ta.a.landLoop(ctx, "mem:0", &bytes.Buffer{}) },
				passes: func() int {
					mu.Lock()
					defer mu.Unlock()
					return reads
				},
				line:  func(ta *testApp) { ta.ok("add --stream s1 --count 1 --one") },
				queue: func(*testApp) {},
			}
		},
		// the decide lane: an attempt decision queued, and a pass is the record of it
		func(ta *testApp) loopCase {
			ta.a.decide = newDecideLane(t.TempDir(), nil, ta.a.now, GradeWait)
			var out bytes.Buffer
			var mu sync.Mutex
			n := 0
			return loopCase{
				name: "decide", interval: DecideEvery,
				start: func(ctx context.Context, ta *testApp) {
					ta.a.decideLoop(ctx, "mem:0", &wakeWriter{mu: &mu, w: &out})
				},
				passes: func() int {
					mu.Lock()
					defer mu.Unlock()
					return strings.Count(out.String(), "DECIDE recorded=")
				},
				line: func(ta *testApp) { ta.ok("add --stream s1 --count 1 --one") },
				queue: func(ta *testApp) {
					mu.Lock()
					n++
					id := "op-" + strconv.Itoa(n)
					mu.Unlock()
					ta.a.decide.put(decide.Decision{ID: id, Decision: decide.AttemptName, Schema: "h", Backend: "b", At: "2026-10-15T00:00:00Z"})
				},
			}
		},
	}
}

type wakeWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *wakeWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// TestServerLoopsWakeOnTheSprintLog pins the three loops. For the land loop and the decide
// lane, over the fake clock: a line of the log runs the next pass within TickFloor with no
// interval spent; a quiet log runs it at its interval, once; a push dropped on the transport
// is caught by the interval, once. The seat-silent look is held back by its timer on a quiet
// log and is not by a line it has not seen.
// tla/LoopWake.tla Wake, Backstop, PushedWithinOneEvent, EveryChangeIsHandled
func TestServerLoopsWakeOnTheSprintLog(t *testing.T) {
	t.Parallel()
	for i, name := range []string{"land", "decide"} {
		mk := loopCases(t)[i]
		t.Run(name+" wakes on a line and keeps its interval", func(t *testing.T) {
			t.Parallel()
			ta := newTestApp(t)
			ta.ok("init --readers reader-a,reader-b --members m1")
			lc := mk(ta)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cancelAll := cancel
			sleeps := 0
			sleep := ta.a.sleep
			ta.a.sleep = func(d time.Duration) {
				sleep(d)
				if sleeps++; sleeps > 40 {
					t.Errorf("%s: the loop ran on the clock alone", lc.name)
					cancelAll()
				}
			}
			before := ta.a.now()
			waits := 0
			must := func(ok bool) {
				if !ok {
					cancel()
				}
			}
			ta.m.LogWait = func(d time.Duration) {
				waits++
				must(assert.Equal(t, waits, lc.passes(), "%s: a pass ran before each wait", lc.name))
				must(assert.Equal(t, lc.interval, d, "%s: the wait is bounded by the interval", lc.name))
				switch waits {
				case 1: // one line of the log
					lc.queue(ta)
					lc.line(ta)
				case 2: // a quiet log: the interval is the clock
					lc.queue(ta)
					must(assert.Equal(t, before.Add(store.TickFloor), ta.a.now(), "%s: the line was taken within the floor, no interval spent", lc.name))
					ta.a.sleep(d)
				default:
					must(assert.Equal(t, before.Add(store.TickFloor+lc.interval), ta.a.now(), "%s: the quiet log ran its pass at the interval", lc.name))
					cancel()
				}
			}
			lc.queue(ta)
			lc.start(ctx, ta)
			require.Equal(t, 3, waits, "%s: initial pass, the line's pass, the backstop's pass", lc.name)
			require.Equal(t, 3, lc.passes())
		})
		t.Run(name+" a dropped push is caught by the backstop once", func(t *testing.T) {
			t.Parallel()
			ta := newTestApp(t)
			ta.ok("init --readers reader-a,reader-b --members m1")
			lc := mk(ta)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cancelAll := cancel
			sleeps := 0
			sleep := ta.a.sleep
			ta.a.sleep = func(d time.Duration) {
				sleep(d)
				if sleeps++; sleeps > 40 {
					t.Errorf("%s: the loop ran on the clock alone", lc.name)
					cancelAll()
				}
			}
			before := ta.a.now()
			waits := 0
			must := func(ok bool) {
				if !ok {
					cancel()
				}
			}
			var mu sync.Mutex
			var script func(d time.Duration) (bool, bool)
			ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
				return lossyLog{Backend: ta.m, wait: &script, mu: &mu}, nil
			}
			mu.Lock()
			script = func(d time.Duration) (bool, bool) {
				waits++
				must(assert.Equal(t, waits, lc.passes(), "%s: a pass ran before each wait", lc.name))
				switch waits {
				case 1: // a line is committed and its push is dropped: the interval passes
					lc.queue(ta)
					lc.line(ta)
					ta.a.sleep(d)
				default:
					must(assert.Equal(t, before.Add(lc.interval), ta.a.now(), "%s: the backstop ran at the interval, nothing woke it sooner", lc.name))
					cancel()
				}
				return false, false
			}
			mu.Unlock()
			lc.queue(ta)
			lc.start(ctx, ta)
			require.Equal(t, 2, waits, "%s: the initial pass and the backstop's, once", lc.name)
			require.Equal(t, 2, lc.passes())
		})
	}
	t.Run("seat-silent look", func(t *testing.T) {
		t.Parallel()
		b := &tokenBus{fails: 2}
		ta, st, _ := seatSilentRig(t, b)
		ctx := context.Background()
		before := ta.a.now()
		ta.a.tellSeatSilent(ctx, st)
		require.Len(t, b.attempts, 1, "the first look sends, and the send fails")
		ta.step(time.Second)
		ta.a.tellSeatSilent(ctx, st)
		require.Len(t, b.attempts, 1, "a quiet log holds the look to its timer")
		ta.ok("add --stream s1 --count 1 --one") // one line of the log, the clock where it was
		ta.a.tellSeatSilent(ctx, st)
		require.Len(t, b.attempts, 2, "a line the last look has not seen runs the next look at once")
		require.Empty(t, b.delivered)
		require.Equal(t, before.Add(time.Second), ta.a.now(), "the clock did not advance for it")
		ta.step(time.Millisecond) // a look at the very instant of the last is never held back
		ta.a.tellSeatSilent(ctx, st)
		require.Len(t, b.attempts, 2, "the line is seen: the look is held to its timer again")
		ta.pastTheLook()
		ta.a.tellSeatSilent(ctx, st)
		require.Len(t, b.attempts, 3, "the quiet log's backstop look runs at the timer")
		require.Len(t, b.delivered, 1)
	})
}
