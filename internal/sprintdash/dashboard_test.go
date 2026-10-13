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
	assert.Contains(t, page, `<img id="logo" class="logo-tile" src="`+liveLogoData+`" width="120" height="120" alt="nova-sprint">`, "no --logo: the live page's logo inline")
	assert.Contains(t, page, `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=`+r.s.Build()+`">`)
	assert.Equal(t, r.s.Build(), r.api()["build"])
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

// keyframeDeclRe finds a declaration's property inside a @keyframes block: an identifier, optionally
// with the two leading dashes of a custom property, before its colon. Percent stops ("0%,") carry no
// colon, so only properties match.
var keyframeDeclRe = regexp.MustCompile(`(-{0,2}[a-zA-Z][a-zA-Z0-9-]*)\s*:`)

// keyframes returns each @keyframes block's name and the property names it declares, de-duplicated.
func keyframes(t *testing.T, css string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	rest := css
	for {
		i := strings.Index(rest, "@keyframes")
		if i < 0 {
			return out
		}
		open := strings.IndexByte(rest[i:], '{')
		if open < 0 {
			return out
		}
		name := strings.TrimSpace(rest[i+len("@keyframes") : i+open])
		block, after := braceBlock(t, rest[i+open:])
		for _, m := range keyframeDeclRe.FindAllStringSubmatch(block, -1) {
			if !contains(out[name], m[1]) {
				out[name] = append(out[name], m[1])
			}
		}
		rest = after
	}
}

// braceBlock returns the balanced {...} at the start of s (deepest included) and the remainder.
func braceBlock(t *testing.T, s string) (string, string) {
	t.Helper()
	if len(s) == 0 || s[0] != '{' {
		t.Fatalf("braceBlock: %q does not start with {", s)
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1], s[i+1:]
			}
		}
	}
	t.Fatalf("braceBlock: unbalanced braces in %q", s)
	return "", ""
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// functionBody returns the { ... } body of the named JS function, by balanced braces.
func functionBody(t *testing.T, src, name string) (string, bool) {
	t.Helper()
	i := strings.Index(src, "function "+name+"(")
	if i < 0 {
		return "", false
	}
	open := strings.IndexByte(src[i:], '{')
	if open < 0 {
		return "", false
	}
	body, _ := braceBlock(t, src[i+open:])
	return body, true
}

// TestThePulseAnimatesOpacityOnTheCompositor pins the owner's scroll fix (this card): the top bar's
// pulse is a compositor animation of opacity itself, not a main-thread animation of the registered
// custom property --pulse. A custom-property animation runs on the main thread, so every frame
// re-styles and repaints all 151 in-flight cells and starves the browser's smooth scroll; opacity
// runs on the compositor. The pulse keeps the same 2.4 s cycle, the same negative delays per state,
// and still stops when the sprint is stopped or motion is reduced. Any @keyframes block that
// animates a custom property fails here, as does a one-shot animation of anything but opacity,
// transform or filter.
func TestThePulseAnimatesOpacityOnTheCompositor(t *testing.T) {
	t.Parallel()
	css := string(file("index.html"))
	blocks := keyframes(t, css)
	pulse, ok := blocks["pulse"]
	require.True(t, ok, "the page carries @keyframes pulse")
	assert.Equal(t, []string{"opacity"}, pulse, "the pulse animates opacity itself (a compositor property)")
	assert.NotContains(t, css, "@property --pulse", "no registered custom property")
	assert.NotContains(t, css, "var(--pulse)", "no cell reads the custom property")
	for name, props := range blocks {
		for _, p := range props {
			assert.False(t, strings.HasPrefix(p, "--"),
				"@keyframes %s animates the custom property %q; a custom property animates on the main thread", name, p)
			if infiniteAnimation(css, name) {
				assert.Contains(t, []string{"opacity", "transform", "filter"}, p,
					"@keyframes %s runs infinitely and animates %q; only opacity, transform or filter compose", name, p)
			}
		}
	}
	assert.Regexp(t, `#overall \.cell\.merging[^}]*animation:\s*pulse 2\.4s ease-in-out infinite`, css,
		"merging keeps the 2.4 s cycle")
	assert.Regexp(t, `#overall \.cell\.review[^}]*animation-delay:\s*-2\.25s`, css, "review keeps its negative delay")
	assert.Regexp(t, `#overall \.cell\.working[^}]*animation-delay:\s*-2\.1s`, css, "working keeps its negative delay")
	assert.Contains(t, css, "#overall.stopped .cell", "a stopped sprint still stops the pulse")
	assert.Contains(t, css, "prefers-reduced-motion", "reduced motion still stops the pulse")
}

// infiniteAnimation reports whether some animation shorthand in css names keyframe and runs infinite.
func infiniteAnimation(css, keyframe string) bool {
	name := regexp.QuoteMeta(keyframe) + `([^\w-]|$)`
	re := regexp.MustCompile(`animation[^;}]*` + name + `[^;}]*infinite`)
	return re.MatchString(css)
}

// TestRenderFitsOnlyWhenTheLayoutCanChange pins the owner's scroll fix (this card): render() no
// longer calls fitTables() on every poll (POLL_MS = 1000), which removed .compact from four panels
// and read every row and cell's scrollWidth once a second, a forced synchronous layout during the
// scroll. The fit runs on load, on resize, and from render() only when a panel's data-derived fit
// key changes.
func TestRenderFitsOnlyWhenTheLayoutCanChange(t *testing.T) {
	t.Parallel()
	app := string(file("app.js"))
	body, ok := functionBody(t, app, "render")
	require.True(t, ok, "app.js carries render(d)")
	assert.NotContains(t, body, "fitTables();", "render must not fit tables unconditionally on every poll")
	assert.Contains(t, body, "fitIfChanged(", "render fits only when the layout key changes")
	assert.Contains(t, app, "function fitIfChanged(", "the conditional fit lives in fitIfChanged")
	assert.Contains(t, app, "function fitKey(", "the fit key is computed from the data")
	assert.Contains(t, app, `window.addEventListener("resize", fitTables)`, "the fit still runs on resize")
}

// TestTheFitMeasuresAVisibleNaturalWidthCopy pins the reader's fix (this card, attempt 4): the
// off-screen copy is measured with both .compact and .folded removed. A folded panel's .scroll
// is display:none (index.html: `.panel.folded > .scroll`), so its rows read zero widths and the
// measure would wrongly clear .compact on a live panel; a folded panel's own class must never
// steer the fit, and an originally noncompact folded panel must still be measured. And because a
// fold button toggles .folded on a panel the last fit may have measured hidden, a fold or unfold
// schedules a fresh fit rather than waiting for a resize or a change of the data-derived fit key.
func TestTheFitMeasuresAVisibleNaturalWidthCopy(t *testing.T) {
	t.Parallel()
	app := string(file("app.js"))
	html := string(file("index.html"))
	fit, ok := functionBody(t, app, "fitTables")
	require.True(t, ok, "app.js carries fitTables()")
	assert.Regexp(t, `copy\.classList\.remove\("compact"\)`, fit, "the copy drops .compact (the full form is what is measured)")
	assert.Regexp(t, `copy\.classList\.remove\("folded"\)`, fit, "the copy drops .folded, so its rows are visible to the measure")
	assert.Contains(t, fit, "cloneNode(true)", "every panel is measured on a visible copy, folded or not")
	assert.Contains(t, html, ".panel.folded > .scroll", "a folded panel hides its scroll body: the reason folded must come off the copy")

	fold, ok := functionBody(t, app, "fitOnFold")
	require.True(t, ok, "app.js carries fitOnFold(e)")
	assert.Contains(t, fold, "setTimeout(fitTables", "a fold click schedules a fresh fit after the toggle lands")
	assert.Contains(t, app, `addEventListener("click", fitOnFold)`, "every fold button is wired to it")
}

// TestTheOffscreenFitCopyKeepsThePanelWidth pins the owner's scroll fix (this card, stella's hold):
// when a compact panel is measured, the full form is laid out on an off-screen copy. An absolutely
// positioned copy shrinks to fit its content, so without the panel's own width it measures text
// against the wrong box and the fit answers for a page no one sees. The copy carries the panel's
// width, so the measure reads the layout the panel would have and the live panel is never touched.
func TestTheOffscreenFitCopyKeepsThePanelWidth(t *testing.T) {
	t.Parallel()
	app := string(file("app.js"))
	body, ok := functionBody(t, app, "fitTables")
	require.True(t, ok, "app.js carries fitTables()")
	assert.Contains(t, body, "cloneNode(true)", "a compact panel's full form is measured on a copy")
	assert.Regexp(t, `copy\.style\.width\s*=\s*panel\.offsetWidth`, body,
		"the copy is laid out at the panel's width, not the shrink-to-fit width of an absolute box")
}

// TestTheStreamsFitKeyFollowsTheRenderedText pins stella's hold (this card): the fit key follows
// what the rows actually draw, from the data (never the DOM). A stream row draws its display name
// (states[k].Display || k), its status pill and its tag line (archived, stuck, ci red), so the key
// must carry those; a poll whose rows read the same text must not measure widths again. The figures
// are keyed by their width, not their value (shape folds every digit to one character), so a count
// that keeps its digits — the common poll — leaves the key alone.
func TestTheStreamsFitKeyFollowsTheRenderedText(t *testing.T) {
	t.Parallel()
	app := string(file("app.js"))
	body, ok := functionBody(t, app, "fitKey")
	require.True(t, ok, "app.js carries fitKey(d)")
	assert.Contains(t, body, ".Display", "the key follows a stream's rendered display name")
	assert.Contains(t, body, "streamStatus(", "the key follows a stream's rendered status")
	assert.Contains(t, body, `"ci red"`, "the key follows a stream's rendered tag line")
	assert.Contains(t, app, "function shape(", "figures are keyed by their width, not their value")
	assert.Regexp(t, `shape\(s\)\s*\{[^}]*replace\(/\[0-9\]/g`, app, "shape folds every digit to one character")
	assert.NotContains(t, body, "querySelector", "the key is derived from the data, never the DOM")
}

// TestAStablePollDoesNotMeasureWidths pins stella's hold (this card): a poll whose table text has
// not changed does no width measurement at all. fitIfChanged computes the key and returns before it
// touches a panel; fitTables, the only place that reads scrollWidth, is reached from render() only
// through it (and from load and resize).
func TestAStablePollDoesNotMeasureWidths(t *testing.T) {
	t.Parallel()
	app := string(file("app.js"))
	body, ok := functionBody(t, app, "fitIfChanged")
	require.True(t, ok, "app.js carries fitIfChanged(d)")
	assert.Regexp(t, `if\s*\(k === lastFitKey\)\s*return`, body,
		"an unchanged key returns before any panel is measured")
	assert.Contains(t, body, "fitTables()", "a changed key still fits")
	fit, ok := functionBody(t, app, "fitTables")
	require.True(t, ok, "app.js carries fitTables()")
	assert.Contains(t, fit, "scrollWidth", "the width reads live in fitTables")
	render, ok := functionBody(t, app, "render")
	require.True(t, ok, "app.js carries render(d)")
	assert.NotContains(t, render, "scrollWidth", "render itself reads no width")
	assert.NotContains(t, render, "clientWidth", "render itself reads no clientWidth")
}

// TestThePollReadsNoLayoutForWidths pins the rest of this card's Done list (the reader's finding on
// attempt 7, this attempt): moving fitTables out of the poll left other layout-forcing reads in
// render's path. renderStreams read the landed header's offsetWidth after its own DOM writes,
// renderOverall counted cells from the overall box's clientWidth, and renderTopStreams read the root
// font size with getComputedStyle and the pie's clientWidth; each forces a synchronous layout once a
// POLL_MS, a page-wide reflow during the scroll. They now read a width measured once per layout by
// measureMetrics — load, resize, and the end of fitTables — and cached, so a poll reads no layout.
// The one read that stays (flash's void t.offsetWidth, restarting a flash animation in place) is
// outside this test's scope and is named with its reason and cost in RESULT.md.
func TestThePollReadsNoLayoutForWidths(t *testing.T) {
	t.Parallel()
	app := string(file("app.js"))
	measure, ok := functionBody(t, app, "measureMetrics")
	require.True(t, ok, "app.js carries measureMetrics()")
	for _, read := range []string{"clientWidth", "offsetWidth", "getComputedStyle"} {
		assert.Contains(t, measure, read, "measureMetrics is the one place a poll's widths are read (%s)", read)
	}
	for _, fn := range []string{"renderStreams", "renderTopStreams"} {
		body, ok := functionBody(t, app, fn)
		require.True(t, ok, "app.js carries "+fn)
		for _, read := range []string{"offsetWidth", "clientWidth", "offsetHeight", "getBoundingClientRect", "getComputedStyle"} {
			assert.NotContains(t, body, read, "%s reads no layout property (%s); it uses the cached width", fn, read)
		}
	}
	overall, ok := functionBody(t, app, "renderOverall")
	require.True(t, ok, "app.js carries renderOverall")
	assert.Contains(t, overall, "METRICS.overall", "renderOverall counts cells from the cached overall width")
	streams, ok := functionBody(t, app, "renderStreams")
	require.True(t, ok, "app.js carries renderStreams")
	assert.NotContains(t, streams, "METRICS.landedW", "renderStreams leaves the landed-header width to measureMetrics")
	fit, ok := functionBody(t, app, "fitTables")
	require.True(t, ok, "app.js carries fitTables")
	assert.Contains(t, fit, "measureMetrics()", "the fit refreshes the width cache")
	assert.Contains(t, app, `window.addEventListener("resize", measureMetrics)`, "a resize refreshes the cache before the renderers read it")
	assert.Contains(t, app, "measureMetrics(); //", "the cache is primed at load, before the first poll")
}
