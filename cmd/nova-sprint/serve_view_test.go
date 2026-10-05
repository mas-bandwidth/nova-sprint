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

// A role view served by the server (GET /api/view/coordinator, /api/view/worker) never
// waits behind a tick (docs/SPEC-SPRINT.md section 14, The server). A cold read of the
// re-seed (PR 7, 2026-10-05) found serveView taking the line of control with a plain Lock:
// the coordinator's main read of the sprint waited behind every tick with no bound and no
// give-up when its caller left, while batches waited at most ServeWait and reads ran on the
// read lane. A view writes nothing, so it runs on the read lane; on a twin file, where every
// verb takes the line, it takes it as a batch does: bounded by ServeWait, answered busy past
// it, and not run for a caller that has gone.

// viewGet is one GET of a view through the fleet's listener (local false) or the loopback
// one, on ctx, answered off the test's goroutine: a view that waits behind the line is
// seen waiting, never a hang.
func viewGet(ctx context.Context, a *app, local bool, target string) <-chan *httptest.ResponseRecorder {
	got := make(chan *httptest.ResponseRecorder, 1)
	r := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	go func() {
		w := httptest.NewRecorder()
		if local {
			localHandler{a}.ServeHTTP(w, r)
		} else {
			a.ServeHTTP(w, r)
		}
		got <- w
	}()
	return got
}

func TestARoleViewNeverWaitsBehindATick(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	require.NotNil(t, ta.a.lanesFor(context.Background()), "the in-memory store runs the lanes")
	clock := newServeClock(ta.a)
	want := map[string]string{
		"/api/view/coordinator?all=1": ta.ok("view coordinator --json --all"),
		"/api/view/worker?as=m1":      ta.ok("view worker --as m1 --json"),
	}
	waited := make(chan struct{}, 8)
	ta.a.serial.waiting = func() { waited <- struct{}{} }
	ta.a.serial.TickLockAs("the tick begun at 03:04:05")
	for _, local := range []bool{false, true} {
		for target, body := range want {
			select {
			case w := <-viewGet(context.Background(), ta.a, local, target):
				require.Equal(t, http.StatusOK, w.Code, "%s local=%v: %s", target, local, w.Body.String())
				assert.Equal(t, strings.TrimRight(body, "\n"), strings.TrimRight(w.Body.String(), "\n"), "%s: the read lane serves the verb's JSON", target)
			case <-waited:
				ta.a.serial.Unlock()
				t.Fatalf("%s local=%v waited behind the tick for the line of control", target, local)
			case <-time.After(10 * time.Second): // a guard against a plain Lock, which tells no one it waits
				ta.a.serial.Unlock()
				t.Fatalf("%s local=%v was not answered while the tick held the line", target, local)
			}
		}
	}
	assert.Empty(t, waited, "no view waited for the line")
	assert.Empty(t, clock.asked, "no view waited on the server's clock")
	assert.Equal(t, 4, ta.a.served.reads, "every view ran on the read lane")
	assert.Zero(t, ta.a.served.serialVerbs)
	ta.a.serial.Unlock()
}

// The reversed witness: on a twin file every verb takes the line, a view too. While a tick
// holds it the view waits ServeWait on the server's clock, then is answered busy (503,
// naming the tick, nothing run); a view whose caller goes while it waits is not run; once
// the line is free the view is served.
func TestARoleViewOnATwinFileWaitsAsABatchDoes(t *testing.T) {
	t.Parallel()
	r := newServerRig(t, twoLanes()...)
	require.Nil(t, r.a.lanesFor(context.Background()), "a twin file runs no lanes")
	clock := newServeClock(r.a)
	r.a.serial.TickLockAs("the tick begun at 03:04:05")

	got := viewGet(context.Background(), r.a, true, "/api/view/coordinator")
	require.Equal(t, ServeWait, <-clock.asked, "the view waits at most ServeWait")
	clock.fire <- r.a.now().Add(ServeWait)
	w := <-got
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "view: busy: the tick begun at 03:04:05 held the line of control past 1s; nothing was run or changed; send it again")

	ctx, cancel := context.WithCancel(context.Background())
	r.a.serial.waiting = cancel // the caller goes as the view starts to wait
	w = <-viewGet(ctx, r.a, false, "/api/view/worker?as=m1")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "not run: its caller went away while it waited; nothing was changed")
	assert.Equal(t, 1, r.a.served.gone)
	assert.Zero(t, r.a.served.serialVerbs, "neither view ran while the tick held the line")
	r.a.serial.waiting = nil

	r.a.serial.Unlock()
	w = <-viewGet(context.Background(), r.a, true, "/api/view/worker?as=m1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"view":"worker"`)
	assert.Equal(t, 1, r.a.served.serialVerbs, "the view ran on the line once it was free")
	assert.Zero(t, r.a.served.reads)
}
