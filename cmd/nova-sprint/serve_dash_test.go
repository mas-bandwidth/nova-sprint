package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// The dashboard's read has a lane of its own (servelanes.go dashWhere; tla/ServerLanes.tla,
// DashWaitsOnlyForDash). On 2026-10-15, after the v1.3.14 restart, the public dashboard's
// `where --json --dashboard` took 2.5 to 11.4 s through the server against 0.3 s on the
// store, because it waited behind every read on the one read lane. These steps run in one
// process on the in-memory store with no socket, no timer and the rig's clock: a wait is
// seen by the read lane's waiting hook, never by a clock.

// dashAt is the `at` of a dashboard answer: when the copy was computed.
func dashAt(t *testing.T, res sprintwire.Result) time.Time {
	t.Helper()
	require.Equal(t, 0, res.Code, res.Stderr)
	var w struct {
		At time.Time `json:"at"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &w), res.Stdout)
	require.False(t, w.At.IsZero(), res.Stdout)
	return w.At
}

// The dashboard's where is answered with the read lane held by another read; two calls
// within one second of the rig's clock share one copy and one after it computes a newer
// one; every other read still runs on the read lane.
func TestTheDashboardWhereNeverWaitsBehindARead(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	l := ta.a.lanesFor(context.Background())
	require.NotNil(t, l, "the in-memory store runs the lanes")
	const dash = "--dashboard"
	first := serveOne(ta.a, true, "where", "--json", dash)
	at0 := dashAt(t, first)
	assert.Equal(t, 1, ta.a.served.reads, "the dashboard's where is counted as a read")

	// another read is in flight: the read lane is held, and a wait on it is seen
	waiting := make(chan struct{}, 8)
	l.line.waiting = func() { waiting <- struct{}{} }
	l.line.Lock()
	freed := false
	free := func() {
		if !freed {
			freed = true
			l.line.Unlock()
		}
	}
	defer free()

	step := func(d time.Duration) {
		ta.mu.Lock()
		ta.now = ta.now.Add(d)
		ta.mu.Unlock()
	}
	ask := func() chan sprintwire.Result {
		done := make(chan sprintwire.Result, 1)
		go func() { done <- serveOne(ta.a, true, "where", "--json", dash) }()
		return done
	}
	step(1500 * time.Millisecond)
	var second sprintwire.Result
	done := ask()
	select {
	case <-waiting:
		t.Error("the dashboard's where waited behind the read lane")
		free()
		second = <-done
	case second = <-done:
	}
	at1 := dashAt(t, second)
	assert.True(t, at1.After(at0), "a call past the floor computes a newer copy: %s after %s", at1, at0)

	// within a second of the copy's start the copy is shared, byte for byte
	step(400 * time.Millisecond)
	third := <-ask()
	assert.Equal(t, second.Stdout, third.Stdout, "a call within the floor shares the copy")
	step(1200 * time.Millisecond)
	fourth := <-ask()
	assert.True(t, dashAt(t, fourth).After(at1), "a call past the floor gets a newer copy")
	assert.Empty(t, waiting, "no dashboard where waited on the read lane")

	// every other read stays on the read lane: with it held, each hits its waiting hook
	free()
	before := ta.a.served.reads
	for _, argv := range [][]string{{"where", "--json"}, {"card", "s1-1"}, {"where", "--json", dash, "--cards"}} {
		res := serveOne(ta.a, true, argv...)
		require.Equal(t, 0, res.Code, res.Stderr)
	}
	assert.Equal(t, before+3, ta.a.served.reads, "each is counted as a read")
	l.line.Lock()
	for _, argv := range [][]string{{"where", "--json"}, {"card", "s1-1"}, {"where", "--json", dash, "--cards"}} {
		d := make(chan sprintwire.Result, 1)
		go func() { d <- serveOne(ta.a, true, argv...) }()
		select {
		case <-waiting:
		case res := <-d:
			t.Errorf("%v ran with the read lane held: %s", argv, res.Stdout)
		}
		l.line.Unlock()
		require.Equal(t, 0, (<-d).Code)
		l.line.Lock()
	}
	l.line.Unlock()
}
