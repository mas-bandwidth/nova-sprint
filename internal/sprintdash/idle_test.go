package sprintdash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedRead is a Server whose second and later reads each wait for a release: a read in
// flight that a request can meet. Its clock is the test's, under a lock.
type gatedRead struct {
	mu      sync.Mutex
	now     time.Time
	n       int
	asked   chan struct{} // when set, a clock reading is said on it once (watchClock)
	began   chan int      // the read's number, as it begins
	release chan struct{} // one a read after the first
	s       *Server
}

func newGatedRead() *gatedRead {
	g := &gatedRead{now: time.Unix(1000, 0), began: make(chan int, 8), release: make(chan struct{}, 8)}
	g.s = &Server{
		Now: func() time.Time {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.asked != nil {
				close(g.asked)
				g.asked = nil
			}
			return g.now
		},
		Every:     time.Second,
		ReadTimer: func(time.Duration) (<-chan time.Time, func()) { return nil, func() {} },
		Read: func() ([]byte, error) {
			g.mu.Lock()
			g.n++
			n := g.n
			g.mu.Unlock()
			g.began <- n
			if n > 1 {
				<-g.release
			}
			return where(n), nil
		},
	}
	return g
}

func (g *gatedRead) advance(d time.Duration) { g.mu.Lock(); g.now = g.now.Add(d); g.mu.Unlock() }

// watchClock is closed at the next reading of the clock: a request's refresh reads it under
// the server's lock, in the same hold that sees the read in flight.
func (g *gatedRead) watchClock() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = make(chan struct{})
	return g.asked
}

func (g *gatedRead) reads() int { g.mu.Lock(); defer g.mu.Unlock(); return g.n }

// landedOf is the landed count of a pull route's /api/sprint answer.
func landedOf(t *testing.T, body []byte) float64 {
	t.Helper()
	var v struct {
		Data struct {
			Landed float64 `json:"landed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &v))
	return v.Data.Landed
}

// A request that meets a read in flight while no poller runs waits for that read and is
// answered from it, never from the copy left from before: the viewer back after an idle
// spell whose own arrival started the read (nova-sprint dashboard's viewers: its poller's
// full read) is shown the sprint as it is now (cold read of PR 490, 2026-10-18).
func TestARequestJoinsTheReadInFlight(t *testing.T) {
	t.Parallel()
	g := newGatedRead()
	g.s.Tick() // the first read, before any listener
	require.Equal(t, 1, <-g.began)
	g.advance(10 * time.Minute) // no one looked for ten minutes

	// the read the viewer's arrival starts (the poller's Refresh), in flight
	first := make(chan struct{})
	go func() { defer close(first); g.s.Refresh() }()
	require.Equal(t, 2, <-g.began)
	// the viewer's own request meets it
	answered := make(chan []byte, 1)
	met := g.watchClock()
	go func() {
		w := httptest.NewRecorder()
		g.s.Pull().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sprint", nil))
		answered <- w.Body.Bytes()
	}()
	<-met // the request's refresh has seen the read in flight
	for range 1000 {
		runtime.Gosched()
	}
	select {
	case body := <-answered:
		t.Fatalf("the request was answered while the read it met was in flight: %s", body)
	default:
	}
	g.release <- struct{}{}
	<-first
	body := <-answered
	assert.EqualValues(t, 2, landedOf(t, body), "the request is answered from the read it met: %s", body)
	assert.Equal(t, 2, g.reads(), "the request joined the read: no second read")
}

// While no poller runs, /healthz reads the sprint and checks its freshness itself: a store
// that went down while no one looked answers 503, and a fresh read clears it; while the
// poller runs it reads nothing (the poller's ticks check).
func TestHealthzChecksFreshnessWhileNoPollerRuns(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now, reads, down := time.Unix(1000, 0), 0, false
	counted := make(chan struct{}, 16) // one signal per read, sent once the read is counted
	s := &Server{
		Now:       func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
		Every:     time.Second,
		ReadTimer: func(time.Duration) (<-chan time.Time, func()) { return nil, func() {} },
		Read: func() ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()
			reads++
			counted <- struct{}{}
			if down {
				return nil, errors.New("the sprint's server does not answer")
			}
			return where(reads), nil
		},
	}
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	count := func() int { mu.Lock(); defer mu.Unlock(); return reads }
	health := func() int {
		w := httptest.NewRecorder()
		s.Pull().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		return w.Code
	}
	s.Tick()
	advance(10 * time.Minute)
	mu.Lock()
	down = true
	mu.Unlock()
	assert.Equal(t, http.StatusServiceUnavailable, health(), "a store down while no one looked")
	assert.Equal(t, 2, count(), "the probe read the sprint")
	mu.Lock()
	down = false
	mu.Unlock()
	advance(time.Second)
	assert.Equal(t, http.StatusOK, health(), "a fresh read clears it")
	assert.Equal(t, 3, count())

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, tick) }()
	advance(time.Second)
	tick <- s.Now()
	for range 4 { // the three reads above and the first tick's: it took its time before the clock moves on
		<-counted
	}
	advance(time.Second)
	tick <- s.Now()
	<-counted // the second tick's read, counted: no read of the poller's is still to come
	before := count()
	advance(time.Second)
	assert.Equal(t, http.StatusOK, health())
	assert.Equal(t, before, count(), "with the poller running /healthz reads nothing")
	cancel()
	<-done
}

// After a gap in the reads longer than Window, /api/sprint's throughput keeps the newest
// sample before the gap as its base until the Window's own samples span MinSpan: a rate over
// the gap at once, never null for MinSpan; then the base is dropped.
func TestTheThroughputSpansAGapInTheReads(t *testing.T) {
	t.Parallel()
	s := &Server{}
	at := time.Unix(1000, 0)
	for i := range 20 { // twenty minutes of reads, a landing a minute
		s.sampleLanded(at.Add(time.Duration(i)*time.Minute), int64(i))
	}
	at = at.Add(19 * time.Minute)
	gap := at.Add(2 * time.Hour) // no one looked for two hours; 12 more landed
	rate, minutes := s.sampleLanded(gap, 31)
	require.NotNil(t, rate, "a viewer back after the gap sees a rate at once")
	assert.Equal(t, 6.0, *rate, "12 landed over the 2 h gap")
	assert.Equal(t, 120.0, minutes)
	for i := 1; i <= 10; i++ {
		rate, minutes = s.sampleLanded(gap.Add(time.Duration(i)*time.Minute), int64(31+i))
	}
	require.NotNil(t, rate)
	assert.Equal(t, 10.0, minutes, "the Window's own samples span MinSpan: the base is dropped")
	assert.Equal(t, 60.0, *rate, "a landing a minute again")

	// the first start ever still waits for MinSpan
	fresh := &Server{}
	rate, _ = fresh.sampleLanded(at, 1)
	assert.Nil(t, rate)
	assert.Equal(t, []sample{{at, 1}}, trimWindow([]sample{{at, 1}}, at))
}
