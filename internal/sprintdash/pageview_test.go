package sprintdash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live page's files as the Python server serves them (dashboard/live/, read 2026-10-11 at
// 03:28Z, its comments since naming the owner as "the owner" there and here alike, a
// comment-only change): the page is that page, byte for byte (pageview.go).
const (
	liveIndexSHA256 = "dfd644d676c948bacc1f32461897d699b8a3b09ce64a594abc525eb7446be30c"
	liveAppSHA256   = "1421928a0c96ac5fa6179a7bf5638199dc1c04843e479619dec3afa327738067"
	liveTile192     = "3b63e9101ce10848200a71aeccaf30135ff1ce684c1b49f794653bfcfa399b10"
	liveTile384     = "8d534cd3563dd5073b4193c6a91c87c29ad7dae05a41a3d824441b11d9107809"
)

// get is one answer of the page's handler at path.
func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, w.Code, path)
	return w
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// buildQuery is the cache-busting query a served page carries.
var buildQuery = regexp.MustCompile(`\?v=[0-9a-f]+`)

// The served index.html and app.js are the live page's files byte for byte: app.js as it is,
// index.html with the live server's three fills (server.py index_page: the script versioned, the
// robot tile in the logo slot, its favicon links), the build in place of its version.
func TestThePageIsTheLivePageByteForByte(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	app, err := os.ReadFile("page/app.js")
	require.NoError(t, err)
	index, err := os.ReadFile("page/index.html")
	require.NoError(t, err)
	assert.Equal(t, liveAppSHA256, sha(app), "page/app.js is the live app.js")
	assert.Equal(t, liveIndexSHA256, sha(index), "page/index.html is the live index.html")
	assert.Equal(t, app, file("app.js"), "the embedded app.js is page/app.js")
	assert.Equal(t, index, file("index.html"), "the embedded index.html is page/index.html")

	assert.Equal(t, app, get(t, r.s, "/app.js").Body.Bytes(), "/app.js is the live app.js, byte for byte")
	const v = "?v=X"
	// server.py index_page, the tile branch, its version written v
	slot := `<img id="logo" class="logo-tile" src="/logo-tile-192.png` + v + `" srcset="/logo-tile-192.png` + v + ` 1x, /logo-tile-384.png` + v + ` 2x" alt="">`
	icon := `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png` + v + `"><link rel="apple-touch-icon" href="/logo-tile-192.png` + v + `"><link rel="preload" as="image" href="/logo-tile-192.png` + v + `" imagesrcset="/logo-tile-192.png` + v + ` 1x, /logo-tile-384.png` + v + ` 2x">`
	want := strings.NewReplacer(`src="app.js"`, `src="app.js`+v+`"`, "<!--LOGO-->", slot, "<!--FAVICON-->", icon).Replace(string(index))
	served := get(t, r.s, "/").Body.String()
	assert.Contains(t, served, `src="app.js?v=`+r.s.Build()+`"`, "the script is versioned by the build")
	assert.Equal(t, want, buildQuery.ReplaceAllString(served, v), "/ is the live index.html as the live server fills it")

	assert.Equal(t, liveTile192, sha(get(t, r.s, "/logo-tile-192.png").Body.Bytes()))
	assert.Equal(t, liveTile384, sha(get(t, r.s, "/logo-tile-384.png").Body.Bytes()))
	assert.Equal(t, "image/png", get(t, r.s, "/logo-tile-192.png").Header().Get("Content-Type"))
}

// liveEnvelope is the keys the live server's /api/sprint carries (server.py: _state and build).
var liveEnvelope = []string{"ok", "data", "fetchedAt", "dataAgeSeconds", "attemptAt", "error", "readSeconds", "minInterval", "throughput", "throughputMinutes", "build"}

// /api/sprint on the page's listeners carries the live server's keys and no other: every key
// app.js reads of the envelope (accept: data, build, throughput, throughputMinutes), and none of
// the release switch (renderRelease draws the picker from releases; the live server serves none,
// so it stays hidden), whatever the query; /events sends the same envelope.
func TestThePageIsTheLivePageEnvelope(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.next = func() ([]byte, error) { return twoReleases(8), nil }
	js := string(file("app.js"))
	for _, k := range []string{"data", "build", "throughput", "throughputMinutes"} {
		assert.Regexp(t, `\bj\.`+k+`\b`, js, "app.js reads the envelope's %s", k)
	}
	var whole struct {
		Streams []struct{ Stream string }
		Tables  struct{ Work map[string]any }
	}
	require.NoError(t, json.Unmarshal(twoReleases(8), &whole))
	for _, path := range []string{"/api/sprint", "/api/sprint?release=v1.1.0", "/api/sprint?release=all"} {
		var v map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(get(t, r.s, path).Body.Bytes(), &v))
		var keys []string
		for k := range v {
			keys = append(keys, k)
		}
		assert.ElementsMatch(t, liveEnvelope, keys, path)
		var d struct {
			Streams []struct{ Stream string }
			Tables  struct{ Work map[string]any }
		}
		require.NoError(t, json.Unmarshal(v["data"], &d))
		assert.Equal(t, whole.Streams, d.Streams, "%s: every stream, whatever the release", path)
		assert.Equal(t, pageKeys(whole.Tables.Work), pageKeys(d.Tables.Work), "%s: every stream's row", path)
		assert.Greater(t, len(d.Tables.Work), 2, "the copy has streams of both releases and none")
	}
	var env map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r.s.PageSnapshot(), &env))
	for _, k := range []string{"release", "releases", "current", "releaseStreams", "stale"} {
		assert.NotContains(t, env, k, "the live envelope has no %s: renderRelease draws no picker", k)
	}
}

// fleetReads is a where --json --cards --rows --archived copy whose reads run on reader rows:
// reader-space holds 3 reading cards, reader-space-2 one more, reader-hetzner2 one (hetzner2's,
// never hetzner's), and a friend's reader row (reader-rowan-next) one, which names no fleet row.
const fleetReads = `{"at":"2026-10-11T03:23:00Z","landed":1,"all":3,"summary":"1/3",
"streams":[{"Stream":"live","Release":"v1"},{"Stream":"old","Release":"v1"}],
"archived":{"streams":["old"],"cards":1,"landed":1,"cost":"$1.00"},
"stream_costs":{"live":{"total_cost":"$2.00","cost_by_tier":{"flash":"$2.00"},"tiers":{"flash":2}}},
"tables":{
 "work":{"live":{"waiting":"1","working":"1","landed":"1","cost":"$2.00"},"old":{"landed":"1"}},
 "merge":{"live":{"queued":"0"},"old":{"queued":"0"}},
 "fleet":{"space":{"working":"1","reads_working":"0","width":"16","status":"up"},
          "hetzner":{"working":"0","reads_working":"0","width":"0","status":"held"},
          "hetzner2":{"working":"0","reads_working":"0","width":"16","status":"up"}},
 "friends":{"rowan-next":{"working":"0","reads_working":"0","width":"8"}},
 "readers":{"reader-space":{"reading":"3","ok":"9"},"reader-space-2":{"reading":"1"},
            "reader-hetzner2":{"reading":"1"},"reader-hetzner":{"reading":"0"},"reader-rowan-next":{"reading":"1"}}},
"cards":[{"id":"c1.w2","stream":"live","member":"space","state":"working"}],
"merging":[{"id":"m","stream":"live","head":"abc"}],"judgments":[],"lanes":[{"machine":"space"}],"holds":[{"name":"m1"}],
"merge_row":{"merging":1,"oldest_merging_min":7},
"rows":[{"id":"c1","stream":"live"}]}`

// pageData of the copy the dashboard read.
func pageDataOf(t *testing.T, r *rig) map[string]json.RawMessage {
	t.Helper()
	var v struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(get(t, r.s, "/api/sprint").Body.Bytes(), &v))
	return v.Data
}

type fleetRow struct {
	Working      string `json:"working"`
	ReadsWorking string `json:"reads_working"`
}

// The orange read cells (the owner, 2026-10-11: "you forgot to display the orange read cards"):
// each fleet row's reads_working, and its working, gain its machine's reader rows' reading, so
// the space row draws 4 orange cells and says 5 working; a friend's reader row adds to no row.
func TestThePageIsTheLivePageReadCells(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.next = func() ([]byte, error) { return []byte(fleetReads), nil }
	d := pageDataOf(t, r)
	var tables struct {
		Fleet   map[string]fleetRow `json:"fleet"`
		Friends map[string]fleetRow `json:"friends"`
	}
	require.NoError(t, json.Unmarshal(d["tables"], &tables))
	assert.Equal(t, fleetRow{Working: "5", ReadsWorking: "4"}, tables.Fleet["space"], "reader-space 3 and reader-space-2 1")
	assert.Equal(t, fleetRow{Working: "1", ReadsWorking: "1"}, tables.Fleet["hetzner2"], "reader-hetzner2 is hetzner2's")
	assert.Equal(t, fleetRow{Working: "0", ReadsWorking: "0"}, tables.Fleet["hetzner"])
	assert.Equal(t, fleetRow{Working: "0", ReadsWorking: "0"}, tables.Friends["rowan-next"], "a friend's reader row folds into no row")

	// one space row of reader-space with 3 reading: reads_working 3
	r2 := newRig(t)
	r2.next = func() ([]byte, error) {
		return []byte(`{"at":"2026-10-11T03:23:00Z","tables":{"work":{},"fleet":{"space":{"working":"0","reads_working":"0"}},"readers":{"reader-space":{"reading":"3"}}}}`), nil
	}
	require.NoError(t, json.Unmarshal(pageDataOf(t, r2)["tables"], &tables))
	assert.Equal(t, "3", tables.Fleet["space"].ReadsWorking, "reader-space holding 3 reading cards: the space row's reads_working is 3")
}

// The page's data is where --json as the live feed prints it: what --cards, --rows and
// --archived add is taken off, stream_costs is folded into the work rows, and no fix mark is
// added (the feed adds none: the page draws where's own counts).
func TestThePageIsTheLivePageData(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.next = func() ([]byte, error) { return []byte(fleetReads), nil }
	d := pageDataOf(t, r)
	for _, k := range pageOnly {
		assert.NotContains(t, d, k)
	}
	assert.NotContains(t, d, "fix", "no fix mark of the view's own")
	assert.JSONEq(t, `{"merging":1,"oldest_merging_min":7}`, string(d["merge_row"]), "merge_row as where --rows prints it, as the live feed reads it")
	var tables struct {
		Work  map[string]map[string]any `json:"work"`
		Merge map[string]any            `json:"merge"`
	}
	require.NoError(t, json.Unmarshal(d["tables"], &tables))
	assert.Equal(t, []string{"live"}, pageKeys(tables.Work), "an archived stream's row is where --json --archived's alone")
	assert.Equal(t, []string{"live"}, pageKeys(tables.Merge))
	assert.Equal(t, map[string]any{"flash": "$2.00"}, tables.Work["live"]["cost_by_tier"], "stream_costs folded into the work row")
	assert.Equal(t, "$2.00", tables.Work["live"]["total_cost"])
	assert.Equal(t, "1", tables.Work["live"]["working"], "the work row's working as where printed it: no fix taken off")
	assert.Contains(t, d, "archived", "where --json's own archived summary stays")

	var snap struct {
		FetchedAt      string   `json:"fetchedAt"`
		DataAgeSeconds *float64 `json:"dataAgeSeconds"`
	}
	require.NoError(t, json.Unmarshal(r.s.PageSnapshot(), &snap))
	assert.Equal(t, "2026-10-11T03:23:00Z", snap.FetchedAt, "fetchedAt is the data's own time")
	require.NotNil(t, snap.DataAgeSeconds)
}

// A puller's copy is its upstream's page copy, served as it came: folded once, never twice.
func TestThePageIsTheLivePageOnAPuller(t *testing.T) {
	t.Parallel()
	once := pageData(json.RawMessage(fleetReads), time.Date(2026, 10, 11, 3, 23, 1, 0, time.UTC))
	c := pageCopyOf(once, true, time.Date(2026, 10, 11, 3, 23, 1, 0, time.UTC))
	assert.JSONEq(t, string(once), string(c.data))
}

func pageKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// whereLanded is a where --json object with landed and archived_landed cards.
func whereLanded(landed, archived int) []byte {
	return []byte(`{"at":"2026-10-02T19:00:00Z","landed":` + itoa(landed) + `,"archived_landed":` + itoa(archived) + `,"all":100,"summary":"x","tables":{"work":{}}}`)
}

// The page's throughput is counted as the live server counts it (server.py sample_landed): where's
// landed alone, null until ten minutes of samples, and an archive (landed falls, archived_landed
// rises) starts the samples again, as on the live page; the pull routes keep the epoch's count,
// which an archive does not restart.
func TestThePageIsTheLivePageThroughput(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	landed, archived := 10, 0
	r.next = func() ([]byte, error) { return whereLanded(landed, archived), nil }
	page := func() (any, any) {
		var v map[string]any
		require.NoError(t, json.Unmarshal(get(t, r.s, "/api/sprint").Body.Bytes(), &v))
		return v["throughput"], v["throughputMinutes"]
	}
	rate, minutes := page()
	assert.Nil(t, rate, "one sample: no rate")
	assert.InDelta(t, 0, minutes, 0)
	r.advance(10 * time.Minute)
	landed = 20
	rate, minutes = page()
	assert.InDelta(t, 60.0, rate, 0, "10 cards in 10 minutes: 60 an hour")
	assert.InDelta(t, 10.0, minutes, 0)

	// an archive: 5 landed cards move to the archived streams
	r.advance(time.Minute)
	landed, archived = 15, 5
	rate, minutes = page()
	assert.Nil(t, rate, "landed fell: the live page starts its samples again")
	assert.InDelta(t, 0, minutes, 0)
	assert.NotNil(t, r.pullAPI()["throughput"], "the pull routes count the epoch's landed, which did not fall")

	for f, want := range map[float64]float64{0.25: 0.2, 0.75: 0.8, 2.25: 2.2, 59.96: 60.0, 1.0 / 3: 0.3} {
		assert.InDelta(t, want, pyRound1(f), 0, "Python's round(%v, 1)", f)
	}
}

// A puller pointed at a view that is not the page's (a pull listener's /api/sprint, its release
// switch and fix marks) holds no copy: the read fails, saying so, and nothing of that view is
// drawn; pointed at a page's listener it serves that page's copy as it came.
func TestThePageIsTheLivePagePullerReadsOnlyAPage(t *testing.T) {
	t.Parallel()
	up := newRig(t)
	up.next = func() ([]byte, error) { return twoReleases(8), nil }
	up.s.Tick()
	var mu sync.Mutex
	serve := func(h http.Handler) *httptest.Server {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			h.ServeHTTP(w, req)
		}))
		t.Cleanup(ts.Close)
		return ts
	}
	pull, pageTS := serve(up.s.Pull()), serve(up.s)

	p := newRig(t)
	p.s.From = &Upstream{URL: pull.URL + "/api/sprint"}
	p.s.Tick()
	assert.Contains(t, p.log.String(), "the upstream dashboard answered a view that is not the page's")
	var v map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(p.s.PageSnapshot(), &v))
	assert.Equal(t, "false", string(v["ok"]))
	assert.Equal(t, "null", string(v["data"]), "nothing of the pull view is drawn")

	q := newRig(t)
	q.s.From = &Upstream{URL: pageTS.URL + "/api/sprint"}
	q.s.Tick()
	mu.Lock()
	var theirs map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(up.s.PageSnapshot(), &theirs))
	mu.Unlock()
	require.NoError(t, json.Unmarshal(q.s.PageSnapshot(), &v))
	assert.Equal(t, "true", string(v["ok"]))
	assert.JSONEq(t, string(theirs["data"]), string(v["data"]), "a page's copy, as it came")
}

// Sentinels are not work (the owner, 2026-10-11: "sentinel cards should not be counted when we
// are estimating time remaining, or cards per-hour"): the page's landed, all and summary are the
// live feed's second jq's, case for case. testdata/sentinel_fold.json holds the cases with the
// jq's own answers, made by running that jq with its now pinned: the last hour's landings, a sprint never cleared (where's zero time), an
// offset clear the jq does not read, a days-long ETA, a summary of another shape.
func TestThePageIsTheLivePageSentinelsAreNotWork(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("testdata/sentinel_fold.json")
	require.NoError(t, err)
	var f struct {
		Now   time.Time `json:"now"`
		Cases map[string]struct {
			Input json.RawMessage `json:"input"`
			Want  struct {
				Landed  int    `json:"landed"`
				All     int    `json:"all"`
				Summary string `json:"summary"`
			} `json:"want"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(b, &f))
	require.Len(t, f.Cases, 5)
	for name, c := range f.Cases {
		var got struct {
			Landed  int             `json:"landed"`
			All     int             `json:"all"`
			Summary string          `json:"summary"`
			Rows    json.RawMessage `json:"rows"`
		}
		require.NoError(t, json.Unmarshal(PageData(c.Input, f.Now), &got), name)
		assert.Equal(t, c.Want.Landed, got.Landed, "%s: landed", name)
		assert.Equal(t, c.Want.All, got.All, "%s: all", name)
		assert.Equal(t, c.Want.Summary, got.Summary, "%s: summary", name)
		assert.Nil(t, got.Rows, "%s: the rows are dropped", name)
	}
	// no rows: as where printed it
	plain := `{"landed":3,"all":5,"summary":"3/5 60.0% -> ETA 1h0m","tables":{"work":{}}}`
	assert.JSONEq(t, plain, string(PageData([]byte(plain), f.Now)))
}
