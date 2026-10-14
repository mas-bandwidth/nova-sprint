package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// hubProbeKV is the hook tests' store with one probe more: when the hook record is
// written (writeHook, which start calls inside the line of control), probe runs first,
// so a test sees what start holds while the line is its.
type hubProbeKV struct {
	*hookSignalKV
	probe func()
}

func (k *hubProbeKV) SetKey(ctx context.Context, name, value string) error {
	if name == store.KeySeatHook && k.probe != nil {
		k.probe()
	}
	return k.hookSignalKV.SetKey(ctx, name, value)
}

// pipeSession is a hook session on an in-memory pipe whose far end is drained, so
// start's hello and challenge are written and never read back (no socket, no clock).
func pipeSession(t *testing.T, ta *testApp, name string) *hookSession {
	t.Helper()
	near, far := net.Pipe()
	t.Cleanup(func() { _ = near.Close(); _ = far.Close() })
	go func() { _, _ = io.Copy(io.Discard, far) }() // ignored: ends when the pipe closes
	return &hookSession{a: ta.a, id: hookNonce(), name: name, conn: near, sent: map[string]bool{}, done: make(chan struct{})}
}

// The v1.2.24 hang (a-card-read-never-runs-the-deal, docs/SPEC-SPRINT.md section 14, The
// server, "The tick's turn"): hookSession.start held the hook hub's mutex while it took
// the line of control, and a seat verb on the line took the hub's mutex (seatActs), so the
// two waited on each other and the tick waited behind both. The hub's mutex is a leaf: it
// is free whenever the line is held for start.
func TestTheSeatHookStartNeverHoldsTheHubAcrossTheLine(t *testing.T) {
	t.Parallel()
	const name = "rowan"
	ta, _ := hookSprint(t, name)
	h := ta.a.hookHub()

	probed, hubFree := false, false
	inner := ta.a.backend
	ta.a.backend = func(ctx context.Context, addr string, n sprint.Names) (store.Backend, error) {
		b, err := inner(ctx, addr, n)
		if err != nil {
			return nil, err
		}
		kv, ok := b.(*hookSignalKV)
		require.True(t, ok, "the hook tests' backend is a hookSignalKV")
		return &hubProbeKV{hookSignalKV: kv, probe: func() {
			if probed {
				return
			}
			probed = true
			if h.mu.TryLock() {
				hubFree = true
				h.mu.Unlock()
			}
		}}, nil
	}

	s := pipeSession(t, ta, name)
	require.NoError(t, s.start(context.Background()))
	require.True(t, probed, "start wrote the hook record on the line")
	assert.True(t, hubFree, "the hook hub's mutex is held while start holds the line of control: a seat verb on the line deadlocks against it")
	h.mu.Lock()
	assert.Same(t, s, h.cur[name], "the hub holds the new session")
	h.mu.Unlock()
}

// With the line held, a seat act (seatActs, which every seat verb runs on the line) and a
// hook start asked at the same time both finish once the line is given up: the act never
// waits on a start that waits for the line.
func TestTheSeatActAndAHookStartBothFinishOnceTheLineIsFree(t *testing.T) {
	t.Parallel()
	const name = "rowan"
	ta, _ := hookSprint(t, name)

	asked := make(chan struct{}, 4)
	ta.a.serial.waiting = func() { asked <- struct{}{} }
	ta.a.serial.Lock()

	s := pipeSession(t, ta, name)
	started := make(chan error, 1)
	go func() { started <- s.start(context.Background()) }()
	<-asked // start waits for the line

	acted := make(chan struct{})
	go func() { ta.a.seatActs(name); close(acted) }()
	<-acted // the act takes the hub's mutex alone: it never needs the line

	ta.a.serial.Unlock()
	require.NoError(t, <-started)
	h := ta.a.hookHub()
	h.mu.Lock()
	assert.Same(t, s, h.cur[name])
	h.mu.Unlock()
}

// A tick that cannot take the line within its deadline counts one wedged tick; at
// TickWedgedToExit the process is wedged; a take resets the count (run.go's loop sets it
// to 0 when a tick stops).
func TestALineNotTakenCountsAsAWedgedTick(t *testing.T) {
	t.Parallel()
	wedged := 0
	for i := 1; i < TickWedgedToExit; i++ {
		assert.False(t, lineMissed(&wedged), "miss %d is not yet wedged", i)
	}
	assert.True(t, lineMissed(&wedged), "miss %d in a row is wedged", TickWedgedToExit)
	wedged = 0 // a tick that took the line and stopped
	assert.False(t, lineMissed(&wedged))
	assert.Equal(t, 1, wedged)
}

// The SERVE line's tick-age: "-" before the first tick, the age after, and tick=stale
// past twice the deadline (no clock: the times are arguments).
func TestTheServeLineSaysTheTickAgeAndStale(t *testing.T) {
	t.Parallel()
	end := time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)
	assert.Equal(t, "tick-age=-", tickAgeField(end, time.Time{}, 10*time.Second))
	assert.Equal(t, "tick-age=15s", tickAgeField(end.Add(15*time.Second), end, 10*time.Second))
	assert.Equal(t, "tick-age=21s tick=stale", tickAgeField(end.Add(21*time.Second), end, 10*time.Second))
	assert.Equal(t, "tick-age=1h0m0s", tickAgeField(end.Add(time.Hour), end, 0), "no deadline: never stale")
}
