package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprintdash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The dashboard ticks every second from its cache independently of how long a read takes.
// A slow read is logged when it exceeds 1s, and during the slow read, the served snapshot's
// dataAgeSeconds tracks the read's age.
func TestTheDashboardBeatsEverySecondWhileAReadIsSlow(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 10, 13, 14, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	fastData := `{"at":"2026-10-13T14:00:00Z","summary":"1/10 10.0% -> ETA 1h","landed":1,"all":10,"tables":{"work":{"s1":{"waiting":"5","landed":"1"}},"fleet":{"m1":{"status":"up","working":"0","width":"16"}}}}`
	slowData := `{"at":"2026-10-13T14:00:08Z","summary":"2/10 20.0% -> ETA 45m","landed":2,"all":10,"tables":{"work":{"s1":{"waiting":"4","landed":"2"}},"fleet":{"m1":{"status":"up","working":"0","width":"16"}}}}`

	var log bytes.Buffer
	readCount := 0
	slowDone := make(chan struct{})
	readRecorded := make(chan struct{}, 1)

	srv := &sprintdash.Server{
		Now:     clock,
		Every:   time.Second,
		Version: "v1.2.18",
		Log:     &log,
		Command: "nova-sprint where --json --dashboard",
		ReadTimer: func(d time.Duration) (<-chan time.Time, func()) {
			ch := make(chan time.Time, 1)
			if d <= time.Second {
				ch <- time.Time{} // the 300ms wait returns immediately with no real time spent
			}
			return ch, func() {}
		},
		Read: func() ([]byte, error) {
			mu.Lock()
			readCount++
			n := readCount
			mu.Unlock()
			if n == 1 {
				return []byte(fastData), nil
			}
			<-slowDone
			return []byte(slowData), nil
		},
	}

	// First tick: initial read completes immediately
	srv.Tick()
	require.Equal(t, 1, srv.PageTicks())

	var snap struct {
		OK             bool     `json:"ok"`
		DataAgeSeconds *float64 `json:"dataAgeSeconds"`
	}
	require.NoError(t, json.Unmarshal(srv.PageSnapshot(), &snap))
	assert.True(t, snap.OK)
	require.NotNil(t, snap.DataAgeSeconds)
	assert.InDelta(t, 0.0, *snap.DataAgeSeconds, 0.1)

	// Next ticks: read takes 7 fake seconds. Across 10 fake seconds, srv.Tick() ticks
	// 10 times without stalling, and the clock shows the read's age.
	srv.OnRecord = func() {
		select {
		case readRecorded <- struct{}{}:
		default:
		}
	}
	for i := 1; i <= 9; i++ {
		mu.Lock()
		now = now.Add(time.Second)
		mu.Unlock()

		if i == 8 {
			close(slowDone) // read finishes at 7s
			<-readRecorded
		}
		srv.Tick()

		require.NoError(t, json.Unmarshal(srv.PageSnapshot(), &snap))
		assert.True(t, snap.OK)
		require.NotNil(t, snap.DataAgeSeconds)
		if i < 8 {
			assert.InDelta(t, float64(i), *snap.DataAgeSeconds, 0.1, "clock shows read's age during slow read (tick %d)", i)
		}
	}

	assert.Equal(t, 10, srv.PageTicks(), "the page tick count is 10 across 10 fake seconds")
	assert.Contains(t, log.String(), "slow read: took 7.000s: nova-sprint where --json --dashboard")
}

// When client sends Accept-Encoding: gzip, responses on both --listen and --pull
// are gzip-encoded (the page, app.js, /api/sprint, pull routes).
func TestTheDashboardGzipsWhenAsked(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	ta.ok("add --stream s1 --count 3")

	srv := ta.a.dashboardServer("", false, "", time.Second, "", nil)
	srv.Tick()

	gunzip := func(b []byte) []byte {
		gr, err := gzip.NewReader(bytes.NewReader(b))
		require.NoError(t, err)
		defer gr.Close()
		out, err := io.ReadAll(gr)
		require.NoError(t, err)
		return out
	}

	// Test --listen routes
	listenPaths := []string{"/", "/app.js", "/api/sprint"}
	for _, p := range listenPaths {
		// With Accept-Encoding: gzip
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "%s status", p)
		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"), "%s content-encoding", p)
		assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"), "%s vary", p)

		decompressed := gunzip(w.Body.Bytes())
		assert.NotEmpty(t, decompressed, "%s decompressed body not empty", p)

		// Without Accept-Encoding: gzip
		plainReq := httptest.NewRequest(http.MethodGet, p, nil)
		plainW := httptest.NewRecorder()
		srv.ServeHTTP(plainW, plainReq)

		assert.Equal(t, http.StatusOK, plainW.Code)
		assert.Empty(t, plainW.Header().Get("Content-Encoding"))
		assert.Equal(t, plainW.Body.Bytes(), decompressed, "%s gzip matches plain", p)
	}

	// Test --pull routes
	pullPaths := []string{"/api/sprint", "/friend/amy"}
	for _, p := range pullPaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		srv.Pull().ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"), "pull %s content-encoding", p)
			decompressed := gunzip(w.Body.Bytes())
			assert.NotEmpty(t, decompressed)
		}
	}
}

// where --json --dashboard folds stream_costs into tables.work, reader rows into fleet
// reads_working/working, sentinel-free x/y and ETA in summary, and excludes archived streams
// without needing --archived --rows.
func TestWhereDashboardCarriesWhatThePageReads(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.m.SetRoutes(costRoutes())
	ta.readersReadPro()
	ta.ok("add --stream s1 --count 2 --brief-file " + proBriefFile(t))
	for _, id := range []string{"s1-1", "s1-2"} {
		ta.tierNow(id, "pro")
	}
	ta.ok("start")
	ta.landStream("s1", []string{"input=10 actual_usd=1 actual_by=harness", "input=10 actual_usd=2 actual_by=harness"}, []string{"", ""}, []string{"", ""})

	// where --dashboard without --json is refused
	code, _, errs := ta.do("where --dashboard")
	require.NotEqual(t, 0, code)
	assert.Contains(t, errs, "--dashboard is a field of the JSON view: give --json with it")

	got := ta.ok("where --json --dashboard")

	var v struct {
		Summary     string                               `json:"summary"`
		Landed      int64                                `json:"landed"`
		All         int64                                `json:"all"`
		Tables      map[string]map[string]map[string]any `json:"tables"`
		StreamCosts map[string]any                       `json:"stream_costs"`
		Rows        any                                  `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &v))

	// Rows are not loaded
	assert.Nil(t, v.Rows, "--dashboard does not include rows")

	// Tables contain work and fleet
	assert.Contains(t, v.Tables, "work")
	assert.Contains(t, v.Tables, "fleet")
	assert.Contains(t, v.Tables["work"], "s1")

	// tables.work has stream_costs fields folded into the work row
	workRow := v.Tables["work"]["s1"]
	assert.Contains(t, workRow, "cost_by_tier")
	assert.Contains(t, workRow, "total_cost")

	// Compare with sprintdash.PageData
	pageExpected := sprintdash.PageData([]byte(ta.ok("where --json --archived --rows")), ta.a.now())
	var exp struct {
		Summary string                               `json:"summary"`
		Landed  int64                                `json:"landed"`
		All     int64                                `json:"all"`
		Tables  map[string]map[string]map[string]any `json:"tables"`
	}
	require.NoError(t, json.Unmarshal(pageExpected, &exp))

	assert.Equal(t, exp.Landed, v.Landed)
	assert.Equal(t, exp.All, v.All)
	assert.Equal(t, exp.Tables["work"]["s1"]["cost_by_tier"], v.Tables["work"]["s1"]["cost_by_tier"])
	assert.Equal(t, exp.Tables["fleet"]["m1"]["working"], v.Tables["fleet"]["m1"]["working"])
}

func TestTheDashboardServerUsesCheapDashboardRead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	ta.ok("add --stream s1 --count 3")

	srv := ta.a.dashboardServer("", false, "", time.Second, "", nil)
	assert.Equal(t, "nova-sprint where --json --dashboard", srv.Command)

	srvRedis := ta.a.dashboardServer("127.0.0.1:6381", true, "", time.Second, "", nil)
	assert.Equal(t, "nova-sprint where --json --dashboard --redis 127.0.0.1:6381", srvRedis.Command)

	body, err := srv.Read()
	require.NoError(t, err)
	assert.JSONEq(t, ta.ok("where --json --dashboard"), string(body))
}
