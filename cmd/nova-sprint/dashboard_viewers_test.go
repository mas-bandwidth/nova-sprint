package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprintdash"
)

// fakePoller is the dashboard server's poller as dashViewers.poll drives it: it counts the
// full reads (Refresh) and says when Run starts, each tick it is given, and when it stops.
type fakePoller struct {
	mu        sync.Mutex
	refreshes int
	started   chan struct{}
	stopped   chan struct{}
	got       chan time.Time
}

func newFakePoller() *fakePoller {
	return &fakePoller{started: make(chan struct{}, 8), stopped: make(chan struct{}, 8), got: make(chan time.Time, 64)}
}

func (p *fakePoller) Refresh() { p.mu.Lock(); p.refreshes++; p.mu.Unlock() }

func (p *fakePoller) reads() int { p.mu.Lock(); defer p.mu.Unlock(); return p.refreshes }

func (p *fakePoller) Run(ctx context.Context, tick <-chan time.Time) {
	p.started <- struct{}{}
	for {
		select {
		case <-ctx.Done():
			p.stopped <- struct{}{}
			return
		case t := <-tick:
			p.got <- t
		}
	}
}

// The dashboard reads the sprint once a second while someone is looking and not at all
// while no one is (the owner, 2026-10-17): a request on its listeners (but /healthz) is a
// viewer, seen as it ends, an open /events stream one for as long as it lasts; the poller
// runs on the clock's ticks until DashboardViewerWindow after the last viewer, and a viewer
// who comes back gets a full read at once and the poller again. A fake clock and channels:
// no socket, no real time.
func TestTheDashboardReadsOnlyWhileSomeoneIsLooking(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	clockNow := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	step := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	v := newDashViewers(clockNow)
	p := newFakePoller()
	clock := make(chan time.Time)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); v.poll(ctx, p, clock) }()
	tick := func() { step(time.Second); clock <- clockNow() }
	// a send is taken once the poll loop is back at its select: the tick before it is done
	settle := func() { clock <- clockNow() }

	page := v.watch(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	get := func(path string) {
		w := httptest.NewRecorder()
		page.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
	}

	// no one looking: ten seconds of ticks read nothing and start no poller
	for range 10 {
		tick()
	}
	settle()
	require.Zero(t, p.reads(), "no read while no one looks")
	require.Empty(t, p.started, "no poller while no one looks")

	// a health check is no viewer
	get("/healthz")
	tick()
	settle()
	require.Zero(t, p.reads(), "a /healthz check starts nothing")
	require.Empty(t, p.started)

	// a page asks (at T): a full read at once, then the poller on every tick
	get("/api/sprint")
	<-p.started
	require.Equal(t, 1, p.reads(), "a viewer back is a full read at once")
	for range 5 {
		tick()
		<-p.got
	}
	// T+29 s: still looking; T+30 s, DashboardViewerWindow after it: the poller stops
	step(DashboardViewerWindow - 7*time.Second)
	tick()
	<-p.got
	tick()
	<-p.stopped
	settle()
	require.Empty(t, p.got, "the tick no one looks at is not read")
	require.Equal(t, 1, p.reads())

	// an /events stream is a viewer while it is open, however long: enter as it opens,
	// leave as it ends (watch's own two calls)
	v.enter()
	<-p.started
	require.Equal(t, 2, p.reads())
	for range 3 {
		step(time.Minute)
		tick()
		<-p.got
	}
	v.leave() // the stream ends, seen now (E)
	step(DashboardViewerWindow - 2*time.Second)
	tick() // E+29 s
	<-p.got
	tick() // E+30 s
	<-p.stopped
	settle()
	require.Empty(t, p.got)
	require.Equal(t, 2, p.reads(), "the poller's reads are its own; a full read only as a viewer comes back")

	// a viewer while the poller runs starts nothing new
	get("/api/sprint")
	<-p.started
	get("/friend/x")
	tick()
	<-p.got
	settle()
	require.Empty(t, p.started, "one poller")
	require.Equal(t, 3, p.reads())
	cancel()
	<-p.stopped
	<-done
}

// A viewer back after an idle spell is answered from a read made after it came, never from
// the copy left from before no one looked: its own request starts the poller, whose full read
// and the request's own meet, and the request waits for that read (sprintdash Refresh joins a
// read in flight). The real dashboard server, a read that waits for its release, the fake
// clock: no socket, no real time.
func TestAViewerBackIsAnsweredFromAFreshRead(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	clockNow := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	n := 0
	began := make(chan int, 8)
	release := make(chan struct{}, 8)
	srv := &sprintdash.Server{
		Now:       clockNow,
		Every:     time.Second,
		ReadTimer: func(time.Duration) (<-chan time.Time, func()) { return nil, func() {} },
		Read: func() ([]byte, error) {
			mu.Lock()
			n++
			k := n
			mu.Unlock()
			began <- k
			if k > 1 {
				<-release
			}
			return fmt.Appendf(nil, `{"at":"2026-10-18T03:00:00Z","landed":%d,"all":100,"summary":"x","tables":{"work":{}}}`, k), nil
		},
	}
	srv.Tick() // the first read, before any listener
	require.Equal(t, 1, <-began)
	v := newDashViewers(clockNow)
	clock := make(chan time.Time)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); v.poll(ctx, srv, clock) }()
	mu.Lock()
	now = now.Add(10 * time.Minute) // no one looked for ten minutes
	mu.Unlock()

	page := v.watch(srv.Pull())
	answered := make(chan []byte, 1)
	go func() {
		w := httptest.NewRecorder()
		page.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sprint", nil))
		answered <- w.Body.Bytes()
	}()
	require.Equal(t, 2, <-began, "the viewer's arrival reads the sprint")
	release <- struct{}{}
	body := <-answered
	var got struct {
		Data struct {
			Landed int `json:"landed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, 2, got.Data.Landed, "the viewer is answered from the read its arrival made: %s", body)
	mu.Lock()
	reads := n
	mu.Unlock()
	require.Equal(t, 2, reads, "one read for the viewer and the poller both")
	cancel()
	<-done
}
