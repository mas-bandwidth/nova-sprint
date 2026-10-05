package sprintdash

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Python server.py the live page ran under served these paths; the dashboard serves
// each of them the same way (docs/SPEC-SPRINT-DASHBOARD.md, Serving and publishing).

// serverPyKeys are /api/sprint's keys as server.py wrote them, in its order, then stale,
// the freshness alarm (fresh.go), the one key the verb adds.
var serverPyKeys = []string{"ok", "data", "fetchedAt", "attemptAt", "error", "readSeconds", "minInterval", "throughput", "throughputMinutes", "build", "stale"}

// isoRe is a time as server.py wrote one: Python's isoformat in UTC.
var isoRe = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d{6})?\+00:00$`)

// keysOf is a JSON object's keys in the order they are written.
func keysOf(t *testing.T, body []byte) []string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(body))
	tok, err := d.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok)
	var keys []string
	for d.More() {
		k, err := d.Token()
		require.NoError(t, err)
		keys = append(keys, k.(string))
		var skip json.RawMessage
		require.NoError(t, d.Decode(&skip))
	}
	return keys
}

func (r *rig) get(method, path string) *httptest.ResponseRecorder {
	r.t.Helper()
	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	assert.Equal(r.t, "no-store, max-age=0", w.Header().Get("Cache-Control"), path)
	assert.Equal(r.t, "no-cache", w.Header().Get("Pragma"), path)
	assert.Equal(r.t, "0", w.Header().Get("Expires"), path)
	return w
}

// /api/sprint is server.py's object: its keys in its order, its times in its form, and
// data the sprint as `where --json` prints it (the keys --cards adds, cardsOnly, are the
// pull routes' alone).
func TestAPISprintIsServerPysJSON(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	before := r.get(http.MethodGet, "/healthz") // no read yet: server.py's first answer
	assert.Equal(t, "ok\n", before.Body.String())

	full := fixture(t)
	r.next = func() ([]byte, error) {
		r.advance(11 * time.Millisecond) // the read's own wall time
		return full, nil
	}
	w := r.get(http.MethodGet, "/api/sprint")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, serverPyKeys, keysOf(t, w.Body.Bytes()))

	var v map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	assert.Equal(t, true, v["ok"])
	assert.Nil(t, v["error"])
	assert.Regexp(t, isoRe, v["fetchedAt"])
	assert.Equal(t, v["fetchedAt"], v["attemptAt"], "a good read: fetched when attempted")
	assert.Equal(t, "2026-10-02T19:00:00.011000+00:00", v["attemptAt"])
	assert.InDelta(t, 0.011, v["readSeconds"], 0)
	assert.InDelta(t, 1.0, v["minInterval"], 0)
	assert.Nil(t, v["throughput"])
	assert.InDelta(t, 0, v["throughputMinutes"], 0)
	assert.Equal(t, r.s.Build(), v["build"])

	var where map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(full, &where))
	require.Contains(t, where, "cards")
	for _, k := range cardsOnly {
		delete(where, k)
	}
	want, err := json.Marshal(where)
	require.NoError(t, err)
	data, err := json.Marshal(v["data"])
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(data), "the page's data is where --json's")

	// the pull routes' /api/sprint keeps the copy whole
	var whole struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(httptestGet(r.s.Pull(), "/api/sprint").Body.Bytes(), &whole))
	assert.JSONEq(t, string(full), string(whole.Data))

	// a failed read: ok false, the error, attemptAt moves and fetchedAt holds
	r.advance(time.Second)
	r.next = func() ([]byte, error) { return nil, errors.New("where exited 2") }
	var f map[string]any
	require.NoError(t, json.Unmarshal(r.get(http.MethodGet, "/api/sprint").Body.Bytes(), &f))
	assert.Equal(t, false, f["ok"])
	assert.Equal(t, "where exited 2", f["error"])
	assert.Equal(t, v["fetchedAt"], f["fetchedAt"])
	assert.Equal(t, "2026-10-02T19:00:01.011000+00:00", f["attemptAt"])
	assert.InDelta(t, 0, f["readSeconds"], 0)
}

// Before the first read /api/sprint is server.py's starting object: nothing read, nulls.
func TestAPISprintBeforeAnyRead(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	assert.JSONEq(t, `{"ok":false,"data":null,"fetchedAt":null,"attemptAt":null,"error":null,"readSeconds":null,"minInterval":1,"throughput":null,"throughputMinutes":0,"build":"`+r.s.Build()+`","stale":false}`,
		string(r.s.Snapshot()))
}

func httptestGet(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// png is a PNG's first bytes, webp a WebP's.
var (
	png  = []byte("\x89PNG\r\n\x1a\nimage")
	webp = []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
)

// logoRig is a rig whose logo directory holds files, its image tool a fake that writes
// a PNG to the argv's output and records each run.
func logoRig(t *testing.T, files ...string) (*rig, string, *[][]string) {
	r := newRig(t)
	dir := t.TempDir()
	for _, n := range files {
		body := png
		if strings.HasSuffix(n, ".webp") {
			body = webp
		}
		if n == "logo.svg" {
			body = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><path d="M0 0h64v64z"/></svg>`)
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), body, 0o600))
	}
	var runs [][]string
	r.s.LogoDir = dir
	r.s.Convert = func(argv []string) error {
		runs = append(runs, argv)
		return os.WriteFile(argv[len(argv)-1], png, 0o600)
	}
	return r, dir, &runs
}

// Every path server.py served answers the same: the page, its script, face and licence,
// the health line, and an unknown path's 404.
func TestServesEveryPathServerPyServed(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	for _, tc := range []struct {
		path, ctype string
		body        []byte
	}{
		{"/app.js", "text/javascript; charset=utf-8", file("app.js")},
		{"/nunito-800.woff2", "font/woff2", file("nunito-800.woff2")},
		{"/OFL.txt", "text/plain; charset=utf-8", file("OFL.txt")},
		{"/healthz", "text/plain; charset=utf-8", []byte("ok\n")},
	} {
		w := r.get(http.MethodGet, tc.path)
		assert.Equal(t, http.StatusOK, w.Code, tc.path)
		assert.Equal(t, tc.ctype, w.Header().Get("Content-Type"), tc.path)
		assert.Equal(t, tc.body, w.Body.Bytes(), tc.path)
		h := r.get(http.MethodHead, tc.path)
		assert.Equal(t, http.StatusOK, h.Code, "HEAD %s", tc.path)
	}
	for _, path := range []string{"/", "/index.html"} {
		w := r.get(http.MethodGet, path)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		want := strings.ReplaceAll(string(file("index.html")), `src="app.js"`, `src="app.js?v=`+r.s.Build()+`"`)
		want = strings.ReplaceAll(strings.ReplaceAll(want, "<!--LOGO-->", ""), "<!--FAVICON-->", "")
		assert.Equal(t, want, w.Body.String(), "%s: the live page, its script versioned, no logo", path)
	}
	nf := r.get(http.MethodGet, "/server.py")
	assert.Equal(t, http.StatusNotFound, nf.Code)
	assert.Equal(t, "not found\n", nf.Body.String())

	// no logo directory: each logo path is server.py's 404 with its own words
	for path, why := range map[string]string{
		"/logo-tile-192.png": "no tile logo\n", "/logo-tile-384.png": "no tile logo\n",
		"/logo-icon.png": "no raster logo\n", "/favicon.png": "no raster logo\n",
		"/favicon.svg": "no logo.svg\n", "/logo.webp": "not found\n", "/logo.png": "not found\n",
	} {
		w := r.get(http.MethodGet, path)
		assert.Equal(t, http.StatusNotFound, w.Code, path)
		assert.Equal(t, why, w.Body.String(), path)
	}
}

// A tile logo (the live page's: logo-robot.webp) is the title's image and the favicon
// through its 192 and 384 px copies, made with sips once and remade only when the tile
// changes; the markup is server.py's.
func TestLogoDirTile(t *testing.T) {
	t.Parallel()
	r, dir, runs := logoRig(t, "logo-robot.webp", "logo-stella.png")
	v := r.s.Build()
	page := r.get(http.MethodGet, "/").Body.String()
	assert.Contains(t, page, `<img id="logo" class="logo-tile" src="/logo-tile-192.png?v=`+v+`" srcset="/logo-tile-192.png?v=`+v+` 1x, /logo-tile-384.png?v=`+v+` 2x" alt="">`)
	assert.Contains(t, page, `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=`+v+`"><link rel="apple-touch-icon" href="/logo-tile-192.png?v=`+v+`"><link rel="preload" as="image" href="/logo-tile-192.png?v=`+v+`" imagesrcset="/logo-tile-192.png?v=`+v+` 1x, /logo-tile-384.png?v=`+v+` 2x">`)
	require.Len(t, *runs, 2)
	assert.Equal(t, []string{"/usr/bin/sips", "-s", "format", "png", "-Z", "192", filepath.Join(dir, "logo-robot.webp"), "--out", filepath.Join(dir, "logo-robot-192.png")}, (*runs)[0])

	for _, size := range []string{"192", "384"} {
		w := r.get(http.MethodGet, "/logo-tile-"+size+".png")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.Equal(t, png, w.Body.Bytes())
	}
	assert.Len(t, *runs, 2, "fresh copies are not made again")

	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "logo-robot.webp"), later, later))
	assert.NotEqual(t, v, r.s.Build(), "a new tile is a new build number: open pages reload")
	r.get(http.MethodGet, "/logo-tile-192.png")
	assert.Len(t, *runs, 3, "a changed tile: its copy is made again")
	assert.Equal(t, http.StatusNotFound, r.get(http.MethodGet, "/logo-icon.png").Code, "a tile is no photo")
	assert.Equal(t, http.StatusNotFound, r.get(http.MethodGet, "/logo.webp").Code)
}

// A photo logo (logo.webp, no tile) is keyed with ffmpeg into the title's icon and the
// favicon; the photo itself is served at its own path.
func TestLogoDirPhoto(t *testing.T) {
	t.Parallel()
	r, dir, runs := logoRig(t, "logo.webp")
	v := r.s.Build()
	page := r.get(http.MethodGet, "/").Body.String()
	assert.Contains(t, page, `<img id="logo" class="raster" src="/logo-icon.png?v=`+v+`" alt="" height="48">`)
	assert.Contains(t, page, `<link rel="icon" type="image/png" href="/favicon.png?v=`+v+`">`)
	require.Len(t, *runs, 2)
	assert.Equal(t, []string{"ffmpeg", "-loglevel", "error", "-y", "-i", filepath.Join(dir, "logo.webp"), "-vf",
		"crop=1420:660:90:120,colorkey=0xFFFFFF:0.18:0.12,scale=-1:144:flags=lanczos,format=rgba", filepath.Join(dir, "logo-icon.png")}, (*runs)[0])
	for _, path := range []string{"/logo-icon.png", "/favicon.png"} {
		w := r.get(http.MethodGet, path)
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, png, w.Body.Bytes(), path)
	}
	w := r.get(http.MethodGet, "/logo.webp")
	assert.Equal(t, "image/webp", w.Header().Get("Content-Type"))
	assert.Equal(t, webp, w.Body.Bytes())
	assert.Equal(t, http.StatusNotFound, r.get(http.MethodGet, "/logo-tile-192.png").Code)

	// an image tool that fails: no slot, no favicon, a 404
	r2, _, _ := logoRig(t, "logo.png")
	r2.s.Convert = func([]string) error { return errors.New("no ffmpeg") }
	assert.NotContains(t, r2.get(http.MethodGet, "/").Body.String(), `id="logo"`)
	assert.Equal(t, "no raster logo\n", r2.get(http.MethodGet, "/favicon.png").Body.String())
	assert.Equal(t, "image/png", r2.get(http.MethodGet, "/logo.png").Header().Get("Content-Type"))
}

// logo.svg wins over every raster: its drawing is inlined in the title and served as
// /favicon.svg, coloured for the theme.
func TestLogoDirSVG(t *testing.T) {
	t.Parallel()
	r, _, runs := logoRig(t, "logo.svg", "logo-robot.webp")
	page := r.get(http.MethodGet, "/").Body.String()
	assert.Contains(t, page, `<svg id="logo" viewBox="0 0 64 64" width="32" height="32" fill="currentColor" aria-hidden="true"><path d="M0 0h64v64z"/></svg>`)
	assert.Contains(t, page, `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`)
	w := r.get(http.MethodGet, "/favicon.svg")
	assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
	assert.Equal(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" fill="currentColor">`+faviconStyle+`<path d="M0 0h64v64z"/></svg>`, w.Body.String())
	assert.Equal(t, http.StatusNotFound, r.get(http.MethodGet, "/logo-tile-192.png").Code, "logo.svg: no raster")
	assert.Empty(t, *runs)
	assert.Equal(t, "&lt;&gt;&amp;&quot;&#x27;", pyEscape(`<>&"'`))
	assert.Equal(t, "image/jpeg", imageType([]byte("\xff\xd8\xffx")))
	assert.Equal(t, "application/octet-stream", imageType([]byte("x")))
}

// roundTrip is an http.RoundTripper of a function: an upstream with no socket.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A puller (--pull <url>, fresh.go's Upstream) is server.py's DASHBOARD_UPSTREAM: it reads
// another dashboard's /api/sprint and takes its data, and the other dashboard may still be
// server.py, whose times are Python's isoformat.
func TestUpstreamTakesAnotherDashboardsData(t *testing.T) {
	t.Parallel()
	const at = "http://100.64.0.1:7390/api/sprint"
	answer := func(code int, body string) *Upstream {
		return &Upstream{URL: at, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, at, r.URL.String())
			return &http.Response{StatusCode: code, Status: strconv.Itoa(code) + " " + http.StatusText(code), Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})}}
	}
	got, up, err := answer(200, `{"ok":true,"data":{"tables":{"work":{}},"landed":3},"fetchedAt":"2026-10-05T16:51:19.989410+00:00","attemptAt":"2026-10-05T16:51:19.989410+00:00","error":null,"readSeconds":0.412,"minInterval":0,"throughput":12.5,"throughputMinutes":60,"build":"0a1b2c3d"}`).read()
	require.NoError(t, err)
	assert.JSONEq(t, `{"tables":{"work":{}},"landed":3}`, string(got))
	assert.Equal(t, time.Date(2026, 10, 5, 16, 51, 19, 989410000, time.UTC), time.Time(*up.FetchedAt).UTC())
	for _, tc := range []struct {
		code       int
		body, want string
	}{
		{200, `{"ok":false,"data":null,"error":"where exited 1"}`, "holds its last good copy: where exited 1"},
		{200, `{"ok":true,"data":{}}`, "has no fetchedAt"},
		{200, `not json`, "no snapshot JSON"},
		{502, `{"data":{"tables":{}}}`, "answered 502"},
	} {
		_, _, err := answer(tc.code, tc.body).read()
		require.Error(t, err, tc.body)
		assert.Contains(t, err.Error(), tc.want, tc.body)
	}
	down := &Upstream{URL: at, Client: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("refused") })}}
	_, _, err = down.read()
	assert.Error(t, err)
}
