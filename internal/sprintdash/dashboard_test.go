package sprintdash

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rig is a server on a hand-moved clock whose reads answer from a script: no socket and
// no real time.
type rig struct {
	t     *testing.T
	s     *Server
	now   time.Time
	reads int
	next  func() ([]byte, error)
	log   bytes.Buffer
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, now: time.Date(2026, 10, 2, 19, 0, 0, 0, time.UTC)}
	r.next = func() ([]byte, error) { return where(0), nil }
	r.s = &Server{
		Now:   func() time.Time { return r.now },
		Every: time.Second,
		Read: func() ([]byte, error) {
			r.reads++
			return r.next()
		},
		Log: &r.log,
	}
	return r
}

// where is a where --json object with landed cards.
func where(landed int) []byte {
	return []byte(fmt.Sprintf(`{"at":"2026-10-02T19:00:00Z","landed":%d,"all":100,"summary":"x","tables":{"work":{}}}`, landed))
}

func (r *rig) advance(d time.Duration) { r.now = r.now.Add(d) }

// api is /api/sprint's answer, decoded.
func (r *rig) api() map[string]any {
	r.t.Helper()
	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sprint", nil))
	require.Equal(r.t, http.StatusOK, w.Code)
	var v map[string]any
	require.NoError(r.t, json.Unmarshal(w.Body.Bytes(), &v))
	return v
}

// The server reads the sprint at most once per Every, however often pages ask.
func TestDashboardReadsAtMostOncePerEvery(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.api()
	r.api()
	assert.Equal(t, 1, r.reads, "two asks in the same instant: one read")
	r.advance(999 * time.Millisecond)
	r.api()
	assert.Equal(t, 1, r.reads, "an ask under Every after the last read began: the cached copy")
	r.advance(time.Millisecond)
	r.api()
	assert.Equal(t, 2, r.reads, "an ask Every after the last read began: a read")
}

// While a read runs, every other ask is answered from the cached copy at once and starts
// no second read, however long the read takes: reads never overlap.
func TestDashboardAnswersFromTheCacheWhileAReadRuns(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.api()
	r.advance(time.Second)
	entered, release := make(chan struct{}), make(chan struct{})
	r.next = func() ([]byte, error) {
		close(entered)
		<-release
		return where(5), nil
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.s.Refresh()
	}()
	<-entered
	r.advance(2 * time.Second) // the read is still running past Every (the channels order the clock)
	v := r.api()
	assert.Equal(t, 2, r.reads, "an ask while a read runs starts no second read")
	assert.InDelta(t, 0, v["data"].(map[string]any)["landed"], 0, "the ask during a read is the copy before it")
	close(release)
	wg.Wait()
	r.next = func() ([]byte, error) { return where(5), nil }
	assert.InDelta(t, 5, r.api()["data"].(map[string]any)["landed"], 0)
}

// A failed read holds the last good copy and says nothing on the page; the log takes
// one line per new failure.
func TestDashboardHoldsSilentlyOnAFailedRead(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.next = func() ([]byte, error) { return where(7), nil }
	good := r.api()
	require.Equal(t, true, good["ok"])

	for i, fail := range []func() ([]byte, error){
		func() ([]byte, error) { return nil, errors.New("where exited 2") },
		func() ([]byte, error) { return nil, errors.New("where exited 2") },
		func() ([]byte, error) { return []byte("not json"), nil },
		func() ([]byte, error) { return []byte(`{"landed":3}`), nil },
	} {
		r.advance(time.Second)
		r.next = fail
		v := r.api()
		assert.Equal(t, false, v["ok"], "read %d", i)
		assert.Equal(t, good["data"], v["data"], "read %d: the data held is the last good read's", i)
		assert.Equal(t, good["fetchedAt"], v["fetchedAt"], "read %d", i)
		assert.NotEmpty(t, v["error"], "read %d", i)
	}
	assert.Equal(t, 3, strings.Count(r.log.String(), "read failed:"), "one log line per new failure:\n%s", r.log.String())
	assert.Contains(t, r.log.String(), "read failed: where JSON has no tables")

	r.advance(time.Second)
	r.next = func() ([]byte, error) { return where(9), nil }
	v := r.api()
	assert.Equal(t, true, v["ok"])
	assert.Nil(t, v["error"])
	assert.InDelta(t, 9, v["data"].(map[string]any)["landed"], 0)
}

// Throughput is the cards landed per hour over the last hour, null until ten minutes of
// samples; a drop in landed (a cleared sprint) starts the samples again.
func TestDashboardThroughput(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	landed := 100
	r.next = func() ([]byte, error) { return where(landed), nil }
	v := r.api()
	assert.Nil(t, v["throughput"])
	assert.InDelta(t, 0, v["throughputMinutes"], 0)

	r.advance(9 * time.Minute)
	landed = 109
	v = r.api()
	assert.Nil(t, v["throughput"], "nine minutes of samples: not yet")
	assert.InDelta(t, 9, v["throughputMinutes"], 0)

	r.advance(time.Minute)
	landed = 110
	v = r.api()
	assert.InDelta(t, 60, v["throughput"], 0, "10 cards in 10 minutes: 60 an hour")

	for i := 0; i < 6; i++ { // an hour and ten minutes in: the first sample has left the window
		r.advance(10 * time.Minute)
		landed += 20
		r.api()
	}
	v = r.api() // the same instant: the cached copy
	assert.InDelta(t, 120, v["throughput"], 0, "the last hour: 120 cards")
	assert.InDelta(t, 60, v["throughputMinutes"], 0)

	r.advance(time.Second)
	landed = 3 // cleared
	v = r.api()
	assert.Nil(t, v["throughput"], "a cleared sprint starts the samples again")
	assert.InDelta(t, 0, v["throughputMinutes"], 0)
}

// The samples are kept across a restart: an instance with 30 minutes of samples saves them, and a
// fresh one over the same saved bytes shows a rate on its first read, not a 10 minute warm-up. A
// landed below the last saved still starts the samples again.
func TestDashboardThroughputSurvivesARestart(t *testing.T) {
	t.Parallel()
	var saved []byte
	keep := func(r *rig) {
		r.s.LoadSamples = func() ([]byte, error) { return saved, nil }
		r.s.SaveSamples = func(b []byte) error { saved = b; return nil }
	}
	old := newRig(t)
	keep(old)
	landed := 100
	old.next = func() ([]byte, error) { return where(landed), nil }
	for i := 0; i <= 3; i++ { // 30 minutes of samples, 10 cards each 10 minutes
		old.api()
		old.advance(10 * time.Minute)
		landed += 10
	}
	require.NotEmpty(t, saved, "the samples were saved")

	fresh := newRig(t) // a restart: a new process over the same saved bytes
	fresh.now = old.now
	keep(fresh)
	fresh.next = func() ([]byte, error) { return where(landed), nil }
	v := fresh.api()
	require.NotNil(t, v["throughput"], "the first read after a restart shows a rate")
	assert.Greater(t, v["throughputMinutes"], 10.0)

	cleared := newRig(t) // the sprint was cleared while it was down: landed below the last saved
	cleared.now = old.now
	keep(cleared)
	cleared.next = func() ([]byte, error) { return where(1), nil }
	assert.Nil(t, cleared.api()["throughput"], "a landed that fell starts the samples again")

	empty := newRig(t) // the first ever start: nothing saved, the warm-up is MinSpan's
	empty.s.LoadSamples = func() ([]byte, error) { return nil, nil }
	empty.s.SaveSamples = func([]byte) error { return nil }
	assert.Nil(t, empty.api()["throughput"])
}

// An archive moves landed cards off the headline (landed falls by them, archived_landed
// rises by them) and lands nothing: the pull routes' throughput samples the epoch's landed
// cards, so it is no fall that starts them again (the page counts as the live page does:
// TestThePageIsTheLivePageThroughput).
func TestDashboardThroughputHoldsAcrossAnArchive(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	landed, archived := 100, 0
	r.next = func() ([]byte, error) {
		return []byte(fmt.Sprintf(`{"at":"2026-10-02T19:00:00Z","landed":%d,"archived_landed":%d,"all":100,"summary":"x","tables":{"work":{}}}`, landed, archived)), nil
	}
	r.pullAPI()
	r.advance(10 * time.Minute)
	landed = 110
	assert.InDelta(t, 60, r.pullAPI()["throughput"], 0)
	r.advance(10 * time.Minute)
	landed, archived = 20, 100 // 100 landed cards archived, 10 more landed
	v := r.pullAPI()
	assert.InDelta(t, 60, v["throughput"], 0, "20 cards in 20 minutes, the archive's 100 not a fall")
	assert.InDelta(t, 20, v["throughputMinutes"], 0)
}

// A sprint done (where --json's done) carries the epoch's landed cards in landed, the archived
// ones included, beside archived_landed: the pull routes' sample is landed alone then, so the finish is no
// spike, and an add after it (done no more, landed the table's again) no fall that restarts
// the samples.
func TestDashboardThroughputHoldsAcrossDoneAndBack(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	landed, archived, done := 90, 10, false
	r.next = func() ([]byte, error) {
		return []byte(fmt.Sprintf(`{"at":"2026-10-02T19:00:00Z","landed":%d,"archived_landed":%d,"done":%t,"all":100,"summary":"x","tables":{"work":{}}}`, landed, archived, done)), nil
	}
	r.pullAPI()
	r.advance(10 * time.Minute)
	landed = 95 // 105 in the epoch
	assert.InDelta(t, 30, r.pullAPI()["throughput"], 0, "5 cards in 10 minutes")
	r.advance(10 * time.Minute)
	landed, archived, done = 110, 110, true // done: the tick archived all; landed is the epoch's 110
	v := r.pullAPI()
	assert.InDelta(t, 30, v["throughput"], 0, "10 cards in 20 minutes: the finish is no spike")
	assert.InDelta(t, 20, v["throughputMinutes"], 0)
	r.advance(10 * time.Minute)
	landed, archived, done = 0, 110, false // an add: done no more, the table's landed again
	v = r.pullAPI()
	assert.InDelta(t, 20, v["throughput"], 0, "10 cards in 30 minutes: the add is no fall")
	assert.InDelta(t, 30, v["throughputMinutes"], 0, "the samples are not restarted")
}

// The read-time summary is one line a minute.
func TestDashboardLogsAReadSummaryAMinute(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	for i := 0; i < 60; i++ {
		r.api()
		r.advance(time.Second)
	}
	assert.Empty(t, r.log.String())
	r.api()
	assert.Equal(t, 1, strings.Count(r.log.String(), "reads=61 failed=0"), r.log.String())
}

// Every answer is no-store; the page's files are embedded; the script link carries the
// build number, and /api/sprint carries the same one.
func TestDashboardServesThePageNoStore(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, "no-store, max-age=0", w.Header().Get("Cache-Control"), path)
		return w
	}
	for path, ctype := range map[string]string{
		"/": "text/html", "/index.html": "text/html", "/app.js": "text/javascript",
		"/nunito-800.woff2": "font/woff2", "/api/sprint": "application/json", "/healthz": "text/plain",
	} {
		w := get(path)
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), ctype), "%s: %s", path, w.Header().Get("Content-Type"))
	}
	assert.Equal(t, http.StatusNotFound, get("/server.py").Code)
	assert.Equal(t, http.StatusNotFound, get("/logo").Code, "no --logo: no logo")
	page := get("/").Body.String()
	assert.Contains(t, page, `src="app.js?v=`+r.s.Build()+`"`)
	assert.Contains(t, page, `<img id="logo" class="logo-tile" src="data:image/png;base64,`, "no --logo: the live page's inline logo, as index.html carries it")
	assert.Contains(t, page, `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=`+r.s.Build()+`">`)
	assert.Equal(t, r.s.Build(), r.api()["build"])
}

// Every path app.js still asks for is served: a path the page fetches that the
// server does not answer is a dead request on every load.
func TestEveryPathThePageAsksIsServed(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	js := string(file("app.js"))
	asked := map[string]bool{}
	for _, re := range []string{`fetch\("([^"]+)"`, `EventSource\("([^"]+)"`} {
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(js, -1) {
			asked[m[1]] = true
		}
	}
	require.NotEmpty(t, asked, "app.js asks for nothing: the path scan found no fetch")
	for path := range asked {
		w := httptest.NewRecorder()
		r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, w.Code, "app.js asks for %s, which the server must answer", path)
	}
	assert.NotContains(t, js, "/landings.json")
}

// --logo names the image served as the logo and the favicon; a new file changes the
// build number, so an open page reloads itself.
func TestDashboardServesTheLogoAndItsBuild(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	logo := filepath.Join(t.TempDir(), "logo.webp")
	webp := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	require.NoError(t, os.WriteFile(logo, webp, 0o600))
	r.s.Logo = logo
	before := r.s.Build()

	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/logo", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/webp", w.Header().Get("Content-Type"))
	assert.Equal(t, webp, w.Body.Bytes())

	w = httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Contains(t, w.Body.String(), `<img id="logo" class="logo-tile" src="/logo?v=`+before+`"`)
	assert.Contains(t, w.Body.String(), `<link rel="icon" href="/logo?v=`+before+`">`)

	require.NoError(t, os.WriteFile(logo, append(webp, 'x'), 0o600))
	assert.NotEqual(t, before, r.s.Build(), "a new logo file is a new build number")
	r.s.Version = "v9"
	assert.NotEqual(t, before, r.s.Build())
}
