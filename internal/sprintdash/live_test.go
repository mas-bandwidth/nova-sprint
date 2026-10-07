package sprintdash

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live page (docs/SPEC-SPRINT-DASHBOARD.md, "The page is the live page"). The owner,
// 2026-10-05: "The live dashboard is what I am watching and expect not to change (visually)
// ... the one I'm watching is the one I want." The page the owner watched was served by a
// Python server.py outside the repo, from mas-bandwidth/work dashboard/live-2026-10-05/
// (app.js, index.html, OFL.txt and its SHA256SUMS). Its files are embedded here as they are
// there, but for the comments, which say "the owner" where the live files carry the owner's
// name: nothing that renders differs.

// liveSHA256SUMS is the live directory's SHA256SUMS, as it is there.
var liveSHA256SUMS = map[string]string{
	"app.js":     "e3e75f918614879f5639131b1a6111ae27ba654a683cb9239b460853bbd90ac7",
	"index.html": "ea5b523657384900f853343d087c893da0a4ef874b38b5682880481189f49ee9",
	"OFL.txt":    "580df76c95a1ec5ab878ceb25bb3d85c6a076804e9c970c8c6972aea775fdf65",
}

// liveRendered is the sha256 of each live file with its comments stripped (rendered): what
// the browser runs and shows. An embedded file that strips to another hash is a page that
// looks or behaves otherwise than the live one. The friends cost category (the owner,
// 2026-10-04: "i don't want dollar amounts for friends. token counts are fine.") added the
// Cost breakdown's rows, the friends row token counts only: the live page takes the same
// change, its lines moving with the card that does the same in nova-sprint.
var liveRendered = map[string]string{
	"app.js":     "30d78e345096041fd4d8a07fb2a7f3427b2d3b4cf323ad1cace7323730b03255",
	"index.html": "acfdde63dcb872da0766219cdb32a27aff308347dc3ca645b88861d39ab4fcaf",
	"OFL.txt":    "580df76c95a1ec5ab878ceb25bb3d85c6a076804e9c970c8c6972aea775fdf65",
}

// pageSHA256 is the sha256 of each embedded file as it is in the repo, comments and all.
// Changing a page file is changing this table in the same commit: a named change.
var pageSHA256 = map[string]string{
	"app.js":           "c0764268da3c50958de97fde1d97d018b7e2179303d22205515597d17dd8aa42",
	"index.html":       "bad92363bc5c5106f9b60ce17be2679a083088de649bc68060f38d37b4ad92c9",
	"OFL.txt":          "580df76c95a1ec5ab878ceb25bb3d85c6a076804e9c970c8c6972aea775fdf65",
	"nunito-800.woff2": "b42be94a8cf3d5fc7877216cdb8bbfb10d57291b06356b6f3b9e7fbf9742b8da",
}

func sum256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// TestThePageIsTheLivePageByteForByte pins the page: each embedded file, its comments
// stripped, hashes to the live file stripped the same way, and each embedded file hashes
// to the repo's pin. Given NOVA_DASHBOARD_LIVE, a copy of the live directory, it checks the
// pins against it too: its files match its SHA256SUMS and strip to liveRendered.
func TestThePageIsTheLivePageByteForByte(t *testing.T) {
	t.Parallel()
	embedded, err := page.ReadDir("page")
	require.NoError(t, err)
	var names []string
	for _, e := range embedded {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, names, slices.Collect(mapKeys(pageSHA256)), "every page file is pinned, and only they")
	for name, want := range pageSHA256 {
		assert.Equal(t, want, sum256(file(name)), "%s changed: a change to the page is a named change to pageSHA256", name)
	}
	for name, want := range liveRendered {
		assert.Equal(t, want, sum256(rendered(name, file(name))), "%s does not render as the live %s", name, name)
		assert.NotContains(t, string(file(name)), "Glenn", "%s names the owner: comments say the owner", name)
	}

	dir := os.Getenv("NOVA_DASHBOARD_LIVE")
	if dir == "" {
		return
	}
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	require.NoError(t, err)
	listed := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			listed[f[1]] = f[0]
		}
	}
	assert.Equal(t, liveSHA256SUMS, listed, "the live SHA256SUMS")
	for name, want := range liveSHA256SUMS {
		b, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.Equal(t, want, sum256(b), "the live %s is its SHA256SUMS line", name)
		assert.Equal(t, liveRendered[name], sum256(rendered(name, b)), "the live %s renders as liveRendered", name)
	}
}

func mapKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// The stripper keeps what renders and drops only comments.
func TestRenderedDropsOnlyComments(t *testing.T) {
	t.Parallel()
	js := "var a = \"// no\", b = '/* no */'; // yes\nvar r = /\\/\\/[/]x/g; /* yes */ var c = `//${a}`;\nx = a / b / c; // yes"
	assert.Equal(t, "var a = \"// no\", b = '/* no */'; \nvar r = /\\/\\/[/]x/g;  var c = `//${a}`;\nx = a / b / c; ", string(rendered("app.js", []byte(js))))
	html := "<p>it's <!-- a note -->here</p>\n<style>a { b: url(\"/x\"); } /* note */</style><script>var s = \"</p>\"; // note\n</script><!--LOGO-->"
	assert.Equal(t, "<p>it's here</p>\n<style>a { b: url(\"/x\"); } </style><script>var s = \"</p>\"; \n</script>", string(rendered("index.html", []byte(html))))
	assert.Equal(t, []byte("as is /* */"), rendered("OFL.txt", []byte("as is /* */")))
}

// serverPyPaths is every path server.py served, the logo's aside (they are below).
var serverPyPaths = []struct{ path, ctype string }{
	{"/", "text/html; charset=utf-8"},
	{"/index.html", "text/html; charset=utf-8"},
	{"/app.js", "text/javascript; charset=utf-8"},
	{"/nunito-800.woff2", "font/woff2"},
	{"/OFL.txt", "text/plain; charset=utf-8"},
	{"/api/sprint", "application/json"},
	{"/healthz", "text/plain; charset=utf-8"},
}

// TestTheDashboardServesEveryPathServerPyServed: each path server.py answered is answered
// here, with its type and the no-store headers, GET and HEAD; an unknown path is
// "not found"; /api/sprint carries every key server.py's did, each of its type.
func TestTheDashboardServesEveryPathServerPyServed(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.next = func() ([]byte, error) { return withSeries(where(3)), nil }
	r.s.Every = 1500 * time.Millisecond
	for _, p := range serverPyPaths {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			r.s.ServeHTTP(w, httptest.NewRequest(method, p.path, nil))
			assert.Equal(t, http.StatusOK, w.Code, "%s %s", method, p.path)
			assert.Equal(t, p.ctype, w.Header().Get("Content-Type"), p.path)
			assert.Equal(t, "no-store, max-age=0", w.Header().Get("Cache-Control"), p.path)
			assert.Equal(t, "no-cache", w.Header().Get("Pragma"), p.path)
			assert.Equal(t, "0", w.Header().Get("Expires"), p.path)
		}
	}
	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/server.py", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not found\n", w.Body.String())

	assert.Equal(t, file("OFL.txt"), body(t, r.s, "/OFL.txt"))
	assert.Equal(t, "ok\n", string(body(t, r.s, "/healthz")))

	// /api/sprint: server.py's keys, each of its JSON type
	r.advance(250 * time.Millisecond)
	var snap map[string]any
	require.NoError(t, json.Unmarshal(body(t, r.s, "/api/sprint"), &snap))
	kinds := map[string]string{
		"ok": "bool", "data": "object", "fetchedAt": "string", "attemptAt": "string", "error": "null",
		"readSeconds": "number", "minInterval": "number", "throughput": "null", "throughputMinutes": "number", "build": "string",
	}
	for key, kind := range kinds {
		require.Contains(t, snap, key)
		assert.Equal(t, kind, jsonKind(snap[key]), key)
	}
	assert.Equal(t, 1.5, snap["minInterval"], "minInterval is the least time between two reads, in seconds")
	assert.Equal(t, 0.0, snap["readSeconds"], "the read took no time on the rig's clock")
	assert.Equal(t, snap["fetchedAt"], snap["attemptAt"], "a good read: attempted when fetched")

	// a failed read keeps the copy and moves attemptAt alone
	r.advance(2 * time.Second)
	r.next = func() ([]byte, error) { return nil, os.ErrDeadlineExceeded }
	var failed map[string]any
	require.NoError(t, json.Unmarshal(body(t, r.s, "/api/sprint"), &failed))
	assert.Equal(t, false, failed["ok"])
	assert.Equal(t, "string", jsonKind(failed["error"]))
	assert.Equal(t, snap["fetchedAt"], failed["fetchedAt"])
	assert.NotEqual(t, failed["fetchedAt"], failed["attemptAt"])
	assert.Equal(t, snap["data"], failed["data"])
}

// withSeries is a where --json object with a landedSeries, as where --json prints it.
func withSeries(b []byte) []byte {
	friends, fleet := make([]int, 144), make([]int, 144)
	friends[143], fleet[142] = 2, 1
	var top map[string]any
	_ = json.Unmarshal(b, &top)
	top["landedSeries"] = map[string]any{"bucketSeconds": 600, "start": 1791208800, "buckets": 144,
		"friends": friends, "fleet": fleet,
		"totals": map[string]int{"friends": 2, "fleet": 1, "unknown": 0}, "lastHour": map[string]int{"friends": 2, "fleet": 1}}
	out, _ := json.Marshal(top)
	return out
}

func body(t *testing.T, h http.Handler, path string) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, w.Code, path)
	return w.Body.Bytes()
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	}
	return "object"
}

// /landings.json is the Landings panel's series (the page fetches it every 15 s and draws
// it when "generated" changes), from where --json's landedSeries in place of the stopgap
// script that wrote the file: server.py served no such path, so the panel stayed hidden
// on its page. "generated" moves only when the series does.
func TestLandingsJSONIsTheCopysLandedSeries(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/landings.json", nil))
	assert.Equal(t, http.StatusNotFound, w.Code, "no copy yet: the panel stays hidden")

	r.next = func() ([]byte, error) { return withSeries(where(3)), nil }
	r.s.Refresh()
	var v map[string]any
	require.NoError(t, json.Unmarshal(body(t, r.s, "/landings.json"), &v))
	for _, key := range []string{"generated", "generatedEpoch", "bucketSeconds", "start", "buckets", "friends", "fleet", "totals", "lastHour"} {
		assert.Contains(t, v, key)
	}
	assert.Equal(t, 144.0, v["buckets"])
	assert.Len(t, v["friends"], 144)
	assert.Equal(t, map[string]any{"friends": 2.0, "fleet": 1.0, "unknown": 0.0}, v["totals"])
	gen := v["generated"]

	r.advance(time.Second)
	r.s.Refresh()
	require.NoError(t, json.Unmarshal(body(t, r.s, "/landings.json"), &v))
	assert.Equal(t, gen, v["generated"], "the same series: the same generated, no redraw")

	r.advance(time.Second)
	r.next = func() ([]byte, error) {
		return bytes.Replace(withSeries(where(4)), []byte(`"friends":2`), []byte(`"friends":3`), 1), nil
	}
	r.s.Refresh()
	require.NoError(t, json.Unmarshal(body(t, r.s, "/landings.json"), &v))
	assert.NotEqual(t, gen, v["generated"], "a new series: a new generated")

	// a copy with no series (an older where) serves none
	r.advance(time.Second)
	r.next = func() ([]byte, error) { return where(5), nil }
	r.s.Refresh()
	w = httptest.NewRecorder()
	r.s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/landings.json", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// The logo, as server.py drew it from the files beside the page: a logo.svg is drawn in
// the title and is the favicon; a tile (logo-robot.webp, logo-stella.png, any other
// image) is the title's image at 1x and 2x and the favicon, from its 192 and 384 px copies
// beside it when they are there; logo.webp or logo.png is a photo on white, its keyed
// logo-icon.png and favicon.png beside it when they are there.
func TestTheLogoIsDrawnAsServerPyDrewIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(s), 0o600))
		return p
	}
	get := func(s *Server, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	// svg
	r := newRig(t)
	r.s.Logo = write("logo.svg", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24z"/></svg>`)
	page := get(r.s, "/").Body.String()
	assert.Contains(t, page, `<h1><svg id="logo" viewBox="0 0 24 24" width="32" height="32" fill="currentColor" aria-hidden="true"><path d="M0 0h24v24z"/></svg><span class="wordmark">`)
	assert.Contains(t, page, `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`)
	w := get(r.s, "/favicon.svg")
	assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
	assert.Equal(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><style>svg{color:#121417}@media (prefers-color-scheme:dark){svg{color:#eef0f3}}</style><path d="M0 0h24v24z"/></svg>`, w.Body.String())
	assert.Equal(t, http.StatusNotFound, get(r.s, "/logo-tile-192.png").Code)

	// a tile with its copies beside it
	r = newRig(t)
	webp := "RIFF\x00\x00\x00\x00WEBPVP8 tile"
	r.s.Logo = write("logo-robot.webp", webp)
	png192, png384 := "\x89PNG\r\n\x1a\n192", "\x89PNG\r\n\x1a\n384"
	write("logo-robot-192.png", png192)
	write("logo-robot-384.png", png384)
	v := r.s.Build()
	page = get(r.s, "/").Body.String()
	assert.Contains(t, page, `<h1><img id="logo" class="logo-tile" src="/logo-tile-192.png?v=`+v+`" srcset="/logo-tile-192.png?v=`+v+` 1x, /logo-tile-384.png?v=`+v+` 2x" alt=""><span class="wordmark">`)
	assert.Contains(t, page, `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=`+v+`"><link rel="apple-touch-icon" href="/logo-tile-192.png?v=`+v+`"><link rel="preload" as="image" href="/logo-tile-192.png?v=`+v+`" imagesrcset="/logo-tile-192.png?v=`+v+` 1x, /logo-tile-384.png?v=`+v+` 2x">`)
	w = get(r.s, "/logo-tile-192.png")
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, png192, w.Body.String())
	assert.Equal(t, png384, get(r.s, "/logo-tile-384.png").Body.String())
	assert.Equal(t, http.StatusNotFound, get(r.s, "/favicon.svg").Code)
	assert.Equal(t, http.StatusNotFound, get(r.s, "/logo-icon.png").Code)
	assert.Equal(t, webp, get(r.s, "/logo").Body.String(), "the file itself, as before")

	// a tile with no copies: the tile itself at both sizes
	r = newRig(t)
	r.s.Logo = write("logo-stella.png", "RIFF\x00\x00\x00\x00WEBPVP8 stella")
	w = get(r.s, "/logo-tile-384.png")
	assert.Equal(t, "image/webp", w.Header().Get("Content-Type"), "the type from the bytes: Stella's .png is WebP inside")
	assert.Equal(t, "RIFF\x00\x00\x00\x00WEBPVP8 stella", w.Body.String())

	// a photo on white with its keyed copies
	r = newRig(t)
	r.s.Logo = write("logo.png", "\x89PNG\r\n\x1a\nphoto")
	write("logo-icon.png", "\x89PNG\r\n\x1a\nicon")
	write("favicon.png", "\x89PNG\r\n\x1a\nfav")
	v = r.s.Build()
	page = get(r.s, "/").Body.String()
	assert.Contains(t, page, `<img id="logo" class="raster" src="/logo-icon.png?v=`+v+`" alt="" height="48">`)
	assert.Contains(t, page, `<link rel="icon" type="image/png" href="/favicon.png?v=`+v+`">`)
	assert.Equal(t, "\x89PNG\r\n\x1a\nicon", get(r.s, "/logo-icon.png").Body.String())
	assert.Equal(t, "\x89PNG\r\n\x1a\nfav", get(r.s, "/favicon.png").Body.String())
	w = get(r.s, "/logo.png")
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, "\x89PNG\r\n\x1a\nphoto", w.Body.String())
	assert.Equal(t, http.StatusNotFound, get(r.s, "/logo.webp").Code)
	assert.Equal(t, http.StatusNotFound, get(r.s, "/logo-tile-192.png").Code)

	// no logo: nothing in the slot, every logo path not found
	r = newRig(t)
	page = get(r.s, "/").Body.String()
	assert.Contains(t, page, `<h1><span class="wordmark">`)
	assert.NotContains(t, page, "<!--FAVICON-->")
	for _, p := range []string{"/favicon.svg", "/logo-tile-192.png", "/logo-tile-384.png", "/logo-icon.png", "/favicon.png", "/logo.webp", "/logo.png", "/logo"} {
		assert.Equal(t, http.StatusNotFound, get(r.s, p).Code, p)
	}
}
