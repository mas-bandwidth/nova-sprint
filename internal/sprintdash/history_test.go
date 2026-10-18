package sprintdash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clock0 is the tests' clock start (1000.0 s, never 0, so a zero time reads as long ago); the
// history's window is set well after it.
var clock0 = time.Unix(1000, 0).UTC()

// histNow is the history's clock in these tests: 50 h 30 min after clock0, so the window runs
// from the hour that begins 23 h before now's to the end of now's hour.
var histNow = clock0.Add(50*time.Hour + 30*time.Minute)

// at is clock0 plus h hours, as a stamp.
func at(h float64) time.Time { return clock0.Add(time.Duration(h * float64(time.Hour))) }

func stampText(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// fixtureRows is where --json --rows's rows for the history tests: a stream "a.b" of three cards
// and a sentinel, with consumer records of each kind, one still out.
func fixtureRows(t *testing.T) []byte {
	t.Helper()
	rec := func(v map[string]string) string { b, _ := json.Marshal(v); return string(b) }
	rows := []histRow{
		{ID: "a.b-1", Stream: "a.b", Column: "landed", Tier: "pro", Fields: map[string]string{
			"kind": "work", "admitted": stampText(at(40)), "dealt_at": stampText(at(41)), "taken_at": stampText(at(41.5)), "landed": stampText(at(45.25)),
			"consumer:21:w1":   rec(map[string]string{"key": "w1", "kind": "work", "who": "amd-1", "route": "pro-x", "model": "m1", "taken": stampText(at(41.5)), "ended": stampText(at(42.2)), "outcome": "failed"}),
			"consumer:21:w2":   rec(map[string]string{"key": "w2", "kind": "work", "who": "friend.stella", "route": "pro-x", "model": "m1", "taken": stampText(at(43)), "ended": stampText(at(44)), "outcome": "ok"}),
			"consumer:21:r2":   rec(map[string]string{"key": "r2", "kind": "read", "who": "reader-hulk", "taken": stampText(at(44.1)), "ended": stampText(at(44.5)), "outcome": "ok"}),
			"consumer:21:land": rec(map[string]string{"key": "land", "kind": "land", "taken": stampText(at(45.25)), "outcome": "ok"}),
		}},
		{ID: "a.b-2", Stream: "a.b", Column: "working", Tier: "flash", Fields: map[string]string{
			"kind": "work", "needs": "a.b-1, other-9", "admitted": stampText(at(44)), "dealt_at": stampText(at(50)), "taken_at": stampText(at(50.1)),
			"consumer:21:w1": rec(map[string]string{"key": "w1", "kind": "work", "who": "amd-1", "route": "flash-y", "model": "m2", "taken": stampText(at(50.1))}),
		}},
		{ID: "a.b-3", Stream: "a.b", Column: "waiting", Fields: map[string]string{"kind": "work", "needs": "a.b-2", "admitted": stampText(at(44))}},
		{ID: "a.b-old", Stream: "a.b", Column: "landed", Fields: map[string]string{"kind": "work", "admitted": stampText(at(1)), "landed": stampText(at(2))}},
		{ID: "sent", Stream: "a.b", Column: "landed", Fields: map[string]string{"kind": "sentinel", "landed": stampText(at(45))}},
		{ID: "other-9", Stream: "z", Column: "review", Fields: map[string]string{"kind": "work", "consumer:21:x": "not json"}},
	}
	b, err := json.Marshal(rows)
	require.NoError(t, err)
	return b
}

func chartByID(t *testing.T, cs []Chart, id string) Chart {
	t.Helper()
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no chart %q", id)
	return Chart{}
}

func TestHistCardsOfReadsStampsNeedsAndConsumers(t *testing.T) {
	t.Parallel()
	cards := HistCardsOf(fixtureRows(t))
	require.Len(t, cards, 6)
	c := cards[0]
	assert.Equal(t, "a.b-1", c.ID)
	assert.Equal(t, at(40), c.Admitted)
	assert.Equal(t, at(45.25), c.Landed)
	require.Len(t, c.Spans, 4)
	assert.Equal(t, []string{"w1", "w2", "r2", "land"}, []string{c.Spans[0].Key, c.Spans[1].Key, c.Spans[2].Key, c.Spans[3].Key}, "oldest taken first")
	assert.Equal(t, []string{"a.b-1", "other-9"}, cards[1].Needs, "needs split on commas, trimmed")
	assert.True(t, cards[1].Spans[0].Ended.IsZero(), "a consumer still out has no end")
	assert.Empty(t, cards[5].Spans, "a consumer record that is no JSON is skipped")
	assert.Nil(t, HistCardsOf(nil))
	assert.Nil(t, HistCardsOf([]byte(`{"no":"list"}`)))
}

// The window is the 24 UTC hours ending with now's: an hour's first instant is in it, the
// instant before the window and any time after now are not.
func TestHistoryWindowEdges(t *testing.T) {
	t.Parallel()
	w := windowAt(histNow)
	assert.Equal(t, clock0.Truncate(time.Hour).Add(51*time.Hour-24*time.Hour), w.start, "the window starts 23 h before now's hour")
	assert.Equal(t, 0, w.bucket(w.start))
	assert.Equal(t, -1, w.bucket(w.start.Add(-time.Second)))
	assert.Equal(t, 23, w.bucket(histNow))
	assert.Equal(t, -1, w.bucket(histNow.Add(time.Second)), "after now is not yet")
	assert.Equal(t, -1, w.bucket(time.Time{}))
	assert.Equal(t, 1, w.bucket(w.start.Add(time.Hour)), "an hour's first instant is that hour's")
	labels := w.labels()
	assert.Len(t, labels, 24)
	assert.Equal(t, w.start.Format("15"), labels[0])
}

func TestHistoryCountsTheLast24Hours(t *testing.T) {
	t.Parallel()
	h := History(HistCardsOf(fixtureRows(t)), histNow)
	w := windowAt(histNow)
	assert.Equal(t, w.start.Unix(), h.Start)
	assert.Equal(t, 24, h.Hours)
	// a.b-1 landed in the window; a.b-old (at 2 h) is outside it; the sentinel is no work
	assert.Equal(t, HistoryTotals{Landed: 1, Started: 3, Finished: 2, OK: 1, Reads: 1, Broken: 0, Workers: 3}, h.Totals)
	landings := chartByID(t, h.Charts, "landings")
	b := w.bucket(at(45.25))
	bars := make([]string, 24)
	for i := range bars {
		bars[i] = "0"
	}
	bars[b] = "1"
	assert.Contains(t, landings.Mermaid, "  bar ["+strings.Join(bars, ", ")+"]\n")
	assert.True(t, landings.HourAxis)
	// the still-out attempt is started, not finished, and its worker counts
	outcomes := chartByID(t, h.Charts, "work-outcomes")
	assert.Contains(t, outcomes.Mermaid, "  bar [")
	flow := chartByID(t, h.Charts, "flow")
	assert.Equal(t, "---\nconfig:\n  sankey:\n    width: 1200\n    height: 460\n---\nsankey\n\nWork attempts,Finished ok,1\nWork attempts,Failed,1\nWork attempts,Still working,1\nFinished ok,Read ok,1\nFailed,Reworked,1\nRead ok,Landed,1\n", flow.Mermaid)
	ttl := chartByID(t, h.Charts, "time-to-land")
	assert.Contains(t, ttl.Mermaid, "5.3", "admitted at 40 h, landed at 45.25 h: 5.25 h, one decimal")
	assert.Empty(t, chartByID(t, h.Charts, "rates").Empty)
	assert.Equal(t, `pie
  "pro-x" : 2
`, chartByID(t, h.Charts, "routes").Mermaid)
}

func TestHistoryOfNothingSaysSo(t *testing.T) {
	t.Parallel()
	h := History(nil, histNow)
	require.NotEmpty(t, h.Charts)
	for _, c := range h.Charts {
		assert.Equal(t, nothing24h, c.Empty, c.ID)
		assert.Empty(t, c.Mermaid, c.ID)
		assert.NotEmpty(t, c.Title, c.ID)
		assert.NotEmpty(t, c.Says, c.ID)
	}
}

func TestXYChartGolden(t *testing.T) {
	t.Parallel()
	src, legend := xyChart(false, []string{"00", `a"b`}, "cards", "", []xySeries{{"landed", colGreen, false, []float64{1, 2.25}}, {"mean", colYellow, true, []float64{0.5, 3}}})
	assert.Equal(t, `---
config:
  xyChart:
    width: 900
    height: 400
---
xychart
  x-axis ["00", "a'b"]
  y-axis "cards" 0 --> 5
  bar [1, 2.3]
  line [0.5, 3]
`, src)
	assert.Equal(t, []LegendItem{{"landed", colGreen}, {"mean", colYellow}}, legend)
	h, _ := xyChart(true, []string{"x"}, "n", "0 --> 100", []xySeries{{"n", colGreen, false, []float64{4}}})
	assert.Contains(t, h, "    height: 134\n---\nxychart horizontal\n")
	assert.Contains(t, h, `  y-axis "n" 0 --> 100`)
}

func TestPieChartKeepsSevenAndOther(t *testing.T) {
	t.Parallel()
	counts := map[string]int{}
	for i := range 9 {
		counts[fmt.Sprintf("r%d", i)] = 10 + i
	}
	c := pieChart("p", "P", "says", counts)
	assert.Equal(t, `pie
  "r8" : 18
  "r7" : 17
  "r6" : 16
  "r5" : 15
  "r4" : 14
  "r3" : 13
  "r2" : 12
  "other" : 21
`, c.Mermaid)
	assert.Len(t, c.Legend, 8)
	assert.Equal(t, nothing24h, pieChart("p", "P", "s", nil).Empty)
}

func TestGanttChartGolden(t *testing.T) {
	t.Parallel()
	from, now := at(10), at(12)
	cards := []HistCard{{ID: "s:1#x", Spans: []Span{
		{Key: "w1", Kind: "work", Who: "amd-1", Taken: at(9), Ended: at(10.5)},
		{Key: "r1", Kind: "read", Who: "amd-1", Taken: at(10.75), Ended: at(11)},
		{Key: "w2", Kind: "work", Who: "hulk", Taken: at(11.5)},
		{Key: "w0", Kind: "work", Who: "gone", Taken: at(1), Ended: at(2)},
	}}}
	c := ganttChart("busy", "B", "S", cards, from, now, 10, 6, nil)
	ms := func(t time.Time) int64 { return t.UnixMilli() }
	assert.Equal(t, fmt.Sprintf(`gantt
  dateFormat x
  axisFormat %%H:%%M
  section amd-1
  s 1 x w1 : s0, %d, %d
  s 1 x read : done, s1, %d, %d
  section hulk
  s 1 x w2 : s2, %d, %d
`, ms(from), ms(at(10.5)), ms(at(10.75)), ms(at(11)), ms(at(11.5)), ms(now)), c.Mermaid, "a span before the window is left out, one begun before it starts at it, one still out ends at now")
}

func TestMermaidEscaping(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `"a'b c"`, quoteLabel("a\"b\nc"))
	assert.Equal(t, `"rocketnet.client-io-12"`, quoteLabel("rocketnet.client-io-12"))
	assert.Equal(t, `"a,b"`, sankeyField("a,b"))
	assert.Equal(t, `"a""b"`, sankeyField(`a"b`))
	assert.Equal(t, "plain", sankeyField("plain"))
	assert.Equal(t, "x y z w", ganttText("x:y#z;w"))
}

func historyRig(t *testing.T) (*rig, *int) {
	r := newRig(t)
	r.now = histNow
	rows := fixtureRows(t)
	n := new(int)
	r.next = func() ([]byte, error) {
		*n++
		return []byte(fmt.Sprintf(`{"at":%q,"landed":%d,"all":9,"summary":"x","tables":{"work":{"a.b":{"landed":"1"}}},"rows":%s}`, r.now.Format(time.RFC3339), *n, rows)), nil
	}
	return r, n
}

func getBody(t *testing.T, s *Server, path string) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, w.Code, path)
	return w.Body.Bytes()
}

// /api/history is computed from the copy held, at most once a minute: a new copy in the same
// minute answers the memo; the next minute, with a new copy, computes again.
func TestHistoryIsMemoizedAMinute(t *testing.T) {
	t.Parallel()
	r, _ := historyRig(t)
	first := getBody(t, r.s, "/api/history")
	var h HistoryView
	require.NoError(t, json.Unmarshal(first, &h))
	assert.Equal(t, 1, h.Totals.Landed)
	assert.Equal(t, histNow.Unix(), h.At)

	r.advance(10 * time.Second)
	reads := r.reads
	again := getBody(t, r.s, "/api/history")
	assert.Greater(t, r.reads, reads, "the request read a new copy")
	assert.Equal(t, first, again, "the same minute: the memo")

	r.advance(time.Minute)
	later := getBody(t, r.s, "/api/history")
	require.NoError(t, json.Unmarshal(later, &h))
	assert.Equal(t, histNow.Add(70*time.Second).Unix(), h.At, "a new minute and a new copy: computed again")
}

func TestHistoryBeforeAnyReadIsUnavailable(t *testing.T) {
	t.Parallel()
	s := &Server{Now: func() time.Time { return histNow }, Every: time.Second}
	assert.JSONEq(t, `{"unavailable":"no good read of the sprint yet"}`, string(s.History()))
}

// roundTrip is a fake transport: no socket.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(q *http.Request) (*http.Response, error) { return f(q) }

// A puller asks its upstream for the chart route, at most once a minute, and says so when
// the upstream serves none.
func TestPullerAsksItsUpstreamForCharts(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	status := http.StatusOK
	var paths []string
	now := histNow
	s := &Server{Now: func() time.Time { return now }, Every: time.Second,
		From: &Upstream{URL: "http://up.example:7390/api/sprint", Client: &http.Client{Transport: roundTrip(func(q *http.Request) (*http.Response, error) {
			calls.Add(1)
			paths = append(paths, q.URL.String())
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"start":1}`)), Header: http.Header{}}, nil
		})}}}
	assert.JSONEq(t, `{"start":1}`, string(s.History()))
	assert.JSONEq(t, `{"start":1}`, string(s.History()))
	assert.Equal(t, int32(1), calls.Load(), "once a minute")
	assert.Equal(t, "http://up.example:7390/api/history", paths[0])
	now = now.Add(time.Minute)
	status = http.StatusNotFound
	assert.Contains(t, string(s.History()), "serves no charts yet")
	assert.Equal(t, int32(2), calls.Load())
}

// The page above the charts is the locked page byte for byte: index.html without the charts'
// panel and script tag is the page as it was before them.
func TestThePageAboveTheChartsIsUnchanged(t *testing.T) {
	t.Parallel()
	index := string(file("index.html"))
	panel := `<section class="panel history" id="history-panel">
  <div class="panel-head"><h2>Last 24 hours</h2><span class="muted history-sub" id="history-sub"></span></div>
  <div class="charts" id="history"></div>
</section>

`
	require.Contains(t, index, panel)
	require.Contains(t, index, "<script src=\"charts.js\"></script>\n")
	before := strings.Replace(strings.Replace(index, panel, "", 1), "<script src=\"charts.js\"></script>\n", "", 1)
	sum := sha256.Sum256([]byte(before))
	assert.Equal(t, "42cc23bc3aaf0804aaf7ba58cd8c15e45c73a8c730c81bf913616d121b72049b", hex.EncodeToString(sum[:]), "the page before the charts, byte for byte")
	app, err := os.ReadFile("page/app.js")
	require.NoError(t, err)
	assert.Equal(t, pageAppSHA256, sha(app), "app.js is untouched")
}

func TestPeleIsPinned(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256(file("pele.js"))
	got := hex.EncodeToString(sum[:])
	assert.Contains(t, string(file("PELE.txt")), "pele.js sha256: "+got, "PELE.txt records the vendored file's digest")
	assert.Contains(t, string(file("PELE.txt")), "version: 1.0.4")
}
