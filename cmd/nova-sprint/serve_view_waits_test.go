package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A role view is how the coordinator reads the sprint. It only reads, so with the lanes
// built it answers on the read lane while a tick holds the line: it does not wait for
// the line and it does not wait on the server's clock. view coordinator, view worker
// and view coordinator --needs are the same (docs/SPEC-SPRINT.md section 14, The server).

func TestARoleViewNeverWaitsBehindATick(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	require.NotNil(t, ta.a.lanesFor(context.Background()), "the in-memory store runs the lanes")
	coord := strings.TrimRight(ta.ok("view coordinator --json"), "\n")
	worker := strings.TrimRight(ta.ok("view worker --as m1 --json"), "\n")
	needs := strings.TrimRight(ta.ok("view coordinator --needs --json"), "\n")
	clock := newServeClock(ta.a)
	waited := make(chan struct{}, 4)
	ta.a.serial.waiting = func() { waited <- struct{}{} }
	ta.a.serial.LockAs("the tick begun at " + ta.a.now().Format("15:04:05"))
	t.Cleanup(func() { ta.a.serial.Unlock() })

	get := func(target string, local bool) *httptest.ResponseRecorder {
		t.Helper()
		var w *httptest.ResponseRecorder
		answerWithin(t, waited, clock, func() {
			r := httptest.NewRequest(http.MethodGet, target, nil)
			w = httptest.NewRecorder()
			if local {
				localHandler{ta.a}.ServeHTTP(w, r)
			} else {
				ta.a.ServeHTTP(w, r)
			}
		})
		return w
	}
	for _, local := range []bool{false, true} {
		w := get("/api/view/coordinator", local)
		require.Equal(t, http.StatusOK, w.Code, "local=%v %s", local, w.Body.String())
		assert.Equal(t, coord, strings.TrimRight(w.Body.String(), "\n"), "local=%v", local)
		w = get("/api/view/worker?as=m1", local)
		require.Equal(t, http.StatusOK, w.Code, "local=%v %s", local, w.Body.String())
		assert.Equal(t, worker, strings.TrimRight(w.Body.String(), "\n"), "local=%v", local)
		assert.Contains(t, w.Body.String(), `"view":"worker"`)
	}
	var reads int
	for _, argv := range [][]string{
		{"view", "coordinator", "--json"},
		{"view", "worker", "--as", "m1", "--json"},
		{"view", "coordinator", "--needs", "--json"},
	} {
		var resCode int
		var stdout, stderr string
		answerWithin(t, waited, clock, func() {
			res := serveOne(ta.a, true, argv...)
			resCode, stdout, stderr = res.Code, res.Stdout, res.Stderr
		})
		require.Equal(t, 0, resCode, "%v: %s", argv, stderr)
		reads++
		switch argv[1] {
		case "worker":
			assert.Equal(t, worker, strings.TrimRight(stdout, "\n"), "%v", argv)
		default:
			if len(argv) > 2 && argv[2] == "--needs" {
				assert.Equal(t, needs, strings.TrimRight(stdout, "\n"))
			} else {
				assert.Equal(t, coord, strings.TrimRight(stdout, "\n"))
			}
		}
	}
	assert.Equal(t, reads, ta.a.served.reads, "the views ran on the read lane")
	assert.Zero(t, ta.a.served.serialVerbs, "no view took the line")
	select {
	case d := <-clock.asked:
		t.Fatalf("a role view waited %s on the server's clock", d)
	default:
	}
}

// On a twin file the lanes are off, so a role view takes the line as a batch does: while
// a tick holds it, the view is answered at ServeWait, busy, naming the tick, having run
// nothing. Once the line is free the same view is answered.
func TestServeViewWaitsLikeABatch(t *testing.T) {
	t.Parallel()
	r := newServerRig(t, "nova-sprint init --readers reader-a,reader-b --members m1")
	require.Nil(t, r.a.lanesFor(context.Background()), "a twin file runs no read lane")
	clock := newServeClock(r.a)
	began := r.a.now()
	holder := "the tick begun at " + began.Format("15:04:05")
	r.a.serial.LockAs(holder)
	held := true
	t.Cleanup(func() {
		if held {
			r.a.serial.Unlock()
		}
	})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r.a.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/view/coordinator", nil))
		done <- w
	}()
	select {
	case d := <-clock.asked:
		require.Equal(t, ServeWait, d, "the view waits at most ServeWait")
		require.LessOrEqual(t, ServeWait, time.Second, "a view is answered within a second of being read")
	case w := <-done:
		t.Fatalf("the view answered before its bound while the tick held the line: %d %s", w.Code, w.Body.String())
	}
	clock.fire <- began.Add(ServeWait)
	w := <-done
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "busy: "+holder+" held the line of control past 1s; nothing was run or changed; send it again")
	assert.NotContains(t, w.Body.String(), `"view"`)

	r.a.serial.Unlock()
	held = false
	free := httptest.NewRecorder()
	r.a.ServeHTTP(free, httptest.NewRequest(http.MethodGet, "/api/view/coordinator", nil))
	require.Equal(t, http.StatusOK, free.Code, free.Body.String())
	assert.Contains(t, free.Body.String(), `"view":"coordinator"`)
}

// A role view gives up when its caller leaves, on the read lane and on the line: the
// view is not run, and nothing was changed.
func TestARoleViewGivesUpWhenItsCallerLeaves(t *testing.T) {
	t.Parallel()
	t.Run("read lane", func(t *testing.T) {
		ta := laneRig(t)
		lanes := ta.a.lanesFor(context.Background())
		require.NotNil(t, lanes)
		saw := make(chan struct{})
		lanes.line.waiting = func() { close(saw) }
		lanes.line.Lock()
		held := true
		t.Cleanup(func() {
			if held {
				lanes.line.Unlock()
			}
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/view/worker?as=m1", nil).WithContext(ctx)
			ta.a.ServeHTTP(w, req)
			done <- w
		}()
		<-saw
		cancel()
		w := giveUp(t, done)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "view: not run: its caller went away while it waited; nothing was changed")
		lanes.line.waiting = nil
		lanes.line.Unlock()
		held = false
		ok := httptest.NewRecorder()
		ta.a.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/api/view/worker?as=m1", nil))
		require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	})

	t.Run("twin file", func(t *testing.T) {
		r := newServerRig(t, "nova-sprint init --readers reader-a,reader-b --members m1")
		require.Nil(t, r.a.lanesFor(context.Background()))
		saw := make(chan struct{})
		r.a.serial.waiting = func() { close(saw) }
		r.a.serial.Lock()
		held := true
		t.Cleanup(func() {
			if held {
				r.a.serial.Unlock()
			}
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/view/coordinator", nil).WithContext(ctx)
			r.a.ServeHTTP(w, req)
			done <- w
		}()
		<-saw
		cancel()
		w := giveUp(t, done)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "view: not run: its caller went away while it waited; nothing was changed")
		assert.NotContains(t, w.Body.String(), "busy:")
		// the caller's taker may still be handing the line back; the next view waits that out
		r.a.serial.waiting = nil
		r.a.serial.Unlock()
		held = false
		ok := httptest.NewRecorder()
		r.a.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/api/view/coordinator", nil))
		require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	})
}

// answerWithin runs fn and fails if it waits for the line or on the server's clock.
// The timer is only so a view that blocks on the line fails the test instead of holding
// it; a view on the read lane does not reach it.
func answerWithin(t *testing.T, waited <-chan struct{}, clock *serveClock, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-waited:
		t.Fatal("a role view waited for the line behind the tick")
	case d := <-clock.asked:
		t.Fatalf("a role view waited %s for the line", d)
	case <-time.After(15 * time.Second):
		t.Fatal("a role view did not answer while a tick held the line")
	}
}

// giveUp is the view's answer after its caller left. A view that does not give up fails
// here instead of holding the test.
func giveUp(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-done:
		return w
	case <-time.After(15 * time.Second):
		t.Fatal("a role view whose caller left did not give up")
		return nil
	}
}
