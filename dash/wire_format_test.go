package dash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestMissingCellsAreAbsentNotZero checks that values the store does not carry are absent or "-" in the
// envelope, never a fabricated 0.
func TestMissingCellsAreAbsentNotZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var env map[string]any
	if err = json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]any)
	// "machine", "coordinator" and "summary" are filled.
	for _, k := range []string{"cleared", "landedSeries", "fleet_work", "friends_work", "providers"} {
		if _, ok := data[k]; ok {
			t.Errorf("MISSING key %s present", k)
		}
	}
	tables := data["tables"].(map[string]any)
	row := tables["fleet"].(map[string]any)["amd-1"].(map[string]any)
	for _, k := range []string{"load", "window"} {
		if _, ok := row[k]; ok {
			t.Errorf("fleet row carries MISSING %s=%v", k, row[k])
		}
	}
	if row["okpct"] != "-" {
		t.Errorf("okpct with no finished work = %v", row["okpct"])
	}
	work := tables["work"].(map[string]any)["s00"].(map[string]any)
	if work["cost"] != "-" {
		t.Errorf("cost with no runs = %v", work["cost"])
	}
	m := tables["merge"].(map[string]any)["s00"].(map[string]any)
	for _, k := range []string{"stuck", "ci", "state"} {
		if _, ok := m[k]; ok {
			t.Errorf("merge carries MISSING %s", k)
		}
	}
}

// TestEnvelopeTopLevelTypesMatchReference checks the envelope's top-level value types against
// testdata/reference-envelope.json.
func TestEnvelopeTopLevelTypesMatchReference(t *testing.T) {
	t.Parallel()
	ref := readJSON(t, "testdata/reference-envelope.json")
	c := &Cache{at: t1000, now: at1000, view: mapSnapshot(store.Snapshot{Epoch: 1}, testRoster, t1000)}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	kind := func(x any) string { return fmt.Sprintf("%T", x) }
	for k, a := range ref {
		if k == "data" {
			continue
		}
		if kind(a) != kind(got[k]) {
			t.Errorf("%s: reference %s (%v), served %s (%v)", k, kind(a), a, kind(got[k]), got[k])
		}
	}
}

// TestStreamStateIsNeverBlank checks that every stream line carries a state: the reference envelope never has
// a blank one, so a stream that is not held must not send "".
func TestStreamStateIsNeverBlank(t *testing.T) {
	t.Parallel()
	ref := readJSON(t, "testdata/reference-envelope.json")
	states := map[string]int{}
	for _, s := range ref["data"].(map[string]any)["streams"].([]any) {
		states[fmt.Sprint(s.(map[string]any)["State"])]++
	}
	d := mapSnapshot(store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"a": {Name: "a"}}}, testRoster, t1000)
	if states[""] == 0 && d.Streams[0].State == "" {
		t.Errorf("reference stream States %v (none blank); served State=%q", states, d.Streams[0].State)
	}
}

// TestTableCellsAreStringsLikeReference checks that every Work and Fleet cell has the same JSON type as
// the reference envelope: a string.
func TestTableCellsAreStringsLikeReference(t *testing.T) {
	t.Parallel()
	ref := readJSON(t, "testdata/reference-envelope.json")["data"].(map[string]any)["tables"].(map[string]any)
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}},
		Rows: map[string]store.Row{"r": {Name: "r", State: "up"}},
		Cards: map[string]store.Card{
			"p": {ID: "p", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
			"w": {ID: "w", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Holder: "r", State: "working", Cost: 1, Priced: true},
		}}
	b, err := json.Marshal(mapSnapshot(s, testRoster, time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	gotTables := got["tables"].(map[string]any)
	first := func(m map[string]any) map[string]any {
		for _, v := range m {
			return v.(map[string]any)
		}
		return nil
	}
	for _, table := range []string{"work", "fleet"} {
		a, c := first(ref[table].(map[string]any)), first(gotTables[table].(map[string]any))
		for k, x := range c {
			y, ok := a[k]
			if !ok {
				continue
			}
			if fmt.Sprintf("%T", x) != fmt.Sprintf("%T", y) {
				t.Errorf("tables.%s.%s: reference %T (%v), served %T (%v)", table, k, y, y, x, x)
			}
		}
	}
}

// TestReadersTableHasNoUnnamedRow checks that a read with no holder does not make a Readers row named "".
func TestReadersTableHasNoUnnamedRow(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "review", Tier: "pro"},
		"r": {ID: "r", Kind: "read", Of: "p", Stream: "s", State: "waiting", Tier: "pro"},
	}}
	readers := mapSnapshot(s, testRoster, time.Time{}).Tables["readers"].(map[string]numberCells)
	if _, ok := readers[""]; ok {
		t.Errorf("readers table has an unnamed row: %v", readers)
	}
}

// TestOkPctDashWhenNoData checks that a row with no finished work shows "-" for ok%, as the total does.
func TestOkPctDashWhenNoData(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{"friendA": {Name: "friendA", State: "down"}}}
	b, err := json.Marshal(mapSnapshot(s, testRoster, time.Time{}).Tables["friends"])
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string]map[string]string
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if got := rows["friendA"]["okpct"]; got != "-" {
		t.Errorf("zero-denominator ok%%: %q, want \"-\"", got)
	}
}

// TestAllFinalStreamWithFailureShowsFail checks that a stream whose cards are all final, at least one of
// them failed, shows state "fail" in the envelope.
func TestAllFinalStreamWithFailureShowsFail(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{
		Epoch:   1,
		Streams: map[string]store.Stream{"s1": {Name: "s1", Pos: 1}},
		Cards: map[string]store.Card{
			"p1": {ID: "p1", Kind: "producer", Stream: "s1", State: "landed", Tier: "pro"},
			"p2": {ID: "p2", Kind: "producer", Stream: "s1", State: "fail", Tier: "pro"},
		},
	}
	env := mapSnapshot(s, testRoster, time.Time{})
	if len(env.Streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(env.Streams))
	}
	if env.Streams[0].State != "fail" {
		t.Fatalf("all-final stream s1 (1 landed, 1 fail) has envelope Streams[0].State = %q, want %q", env.Streams[0].State, "fail")
	}
}

// TestStreamRoutesDoNotReturn404 checks that the stream routes the page links to are answered (by a
// redirect to the board) rather than 404.
func TestStreamRoutesDoNotReturn404(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/charts.js")
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(&Cache{at: t1000, now: at1000})
	for _, p := range []string{"/stream/s00", "/api/stream/s00", "/api/history"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code == 404 && strings.Contains(string(js), `"/stream/"`) {
			t.Errorf("%s -> 404 while charts.js still wires stream-name clicks to /stream/", p)
		}
	}
}

// TestEventsRouteIsNot404 checks that /events is answered (by a redirect to the board).
func TestEventsRouteIsNot404(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	Handler(&Cache{at: t1000, now: at1000}).ServeHTTP(rec, httptest.NewRequest("GET", "/events", nil))
	if rec.Code == 404 {
		t.Errorf("/events -> 404")
	}
}

// TestGzipNegotiation checks the Accept-Encoding negotiation: gzip is used unless excluded by q=0, and a
// wildcard counts only when it is not zero.
func TestGzipNegotiation(t *testing.T) {
	t.Parallel()
	c := &Cache{at: t1000, now: at1000, view: mapSnapshot(store.Snapshot{}, testRoster, t1000)}
	for _, tc := range []struct {
		h          string
		compressed bool
	}{{"gzip", true}, {"gzip;q=0", false}, {"gzip;q=0, *;q=1", false}, {"br, *;q=0.5", true}, {"br", false}} {
		r := httptest.NewRequest("GET", "/api/sprint", nil)
		r.Header.Set("Accept-Encoding", tc.h)
		w := httptest.NewRecorder()
		Handler(c).ServeHTTP(w, r)
		if got := w.Header().Get("Content-Encoding") == "gzip"; got != tc.compressed {
			t.Errorf("%q gzip=%v", tc.h, got)
		}
	}
}
