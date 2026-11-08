package dash

// Cold read by Rowan’s reviewer (2026-11-06), retained with adaptations for
// the agreed 30-second health grace, controlled clock, and revised cost rule.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func mustApply(t *testing.T, s store.Store, id string, epoch int, w ...store.Write) {
	t.Helper()
	if r := apply(t, s, id, epoch, w...); !r.Accepted {
		t.Fatalf("%s refused: %s", id, r.Reason)
	}
}

// seedSprint writes n producers in streams, two rows, and a consumer pair for every tenth producer.
func seedSprint(t *testing.T, s store.Store, n int) {
	t.Helper()
	w := []store.Write{
		store.PutRow{Name: "amd-1"},
		store.PutRow{Name: "stella"},
	}
	for i := 0; i < 50; i++ {
		w = append(w, store.PutStream{Name: fmt.Sprintf("s%02d", i)})
	}
	for i := 0; i < n; i++ {
		w = append(w, store.PutCard{ID: fmt.Sprintf("p%05d", i), Kind: "producer", Tier: "pro", Stream: fmt.Sprintf("s%02d", i%50)})
	}
	mustApply(t, s, "seed", 1, w...)
	w = nil
	for i := 0; i < n; i += 10 {
		p, st := fmt.Sprintf("p%05d", i), fmt.Sprintf("s%02d", i%50)
		w = append(w, store.PutCard{ID: "w" + p, Kind: "work", Of: p, Stream: st, Tier: "pro", Holder: "amd-1"},
			store.PutCard{ID: "r" + p, Kind: "read", Of: p, Stream: st, Tier: "pro", Holder: "stella"})
	}
	mustApply(t, s, "consumers", 1, w...)
}

// 2: a clear empties the view and moves the epoch; an op kind the fold has never seen folds fine.
func TestColdreadClearAndUnknownKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	r, err := f.Apply(ctx, store.Op{ID: "odd", Epoch: 1, Actor: "x", Kind: "frobnicate", At: t1000, Writes: []store.Write{store.SetCardState{Card: "p00001", State: "landed"}}})
	if err != nil || !r.Accepted {
		t.Fatalf("odd op: %v %+v", err, r)
	}
	c := boardOf(t, f)
	if d := c.pageSnapshot().Data; d.Landed != 1 || d.All != 20 {
		t.Fatalf("after odd kind landed=%d all=%d", d.Landed, d.All)
	}
	if _, err = f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "x", Kind: "clear", At: t1000}); err != nil {
		t.Fatal(err)
	}
	c = boardOf(t, f)
	d := c.pageSnapshot().Data
	if d.Epoch != 2 || d.Landed != 0 || d.All != 0 || len(d.Tables["work"].(map[string]*workRow)) != 0 {
		t.Fatalf("after clear epoch=%d landed=%d all=%d", d.Epoch, d.Landed, d.All)
	}
}

// 4: MISSING cells come out absent or null, never a fabricated 0.
func TestColdreadMissingCellsBlank(t *testing.T) {
	t.Parallel()
	f := store.NewFake()
	seedSprint(t, f, 20)
	c := boardOf(t, f)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]any)
	// "machine", "coordinator" and "summary" are filled (drift row 4; the top strip, 2026-11-07).
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

// 3: the throughput tile is the store's landings over the last hour, whatever run's uptime.
func TestColdreadDefectThroughputNeverComputed(t *testing.T) {
	t.Parallel()
	var landings []time.Time
	for i := range 5 {
		landings = append(landings, t1000.Add(-time.Duration(i)*3*time.Minute))
	}
	c := NewFromFeed(Feed{At: t1000, Landings: landings}, at1000)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var env PageEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Throughput != 5 || env.ThroughputMinutes != 60 {
		t.Errorf("throughput %v over %v min after 5 landings in the last hour, want 5 over 60", env.Throughput, env.ThroughputMinutes)
	}
}

// 3: a priced sum remains visible when a stream also has unpriced work.
func TestColdreadDefectUnpricedBlanksV1Costs(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {ID: "a", Kind: "work", Stream: "s", Tier: "pro", Cost: 1200000, Priced: true}, "b": {ID: "b", Kind: "work", Stream: "s", Tier: "pro", Priced: false}}}
	d := mapSnapshot(s, testRoster, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if w.Cost == nil || w.Cost.Int64() != 120 {
		t.Fatalf("priced sum lost: %+v", w)
	}
	js, _ := page.ReadFile("page/app.js")
	if strings.Contains(string(js), "unpriced ? \"\" : money(recorded)") {
		t.Fatal("unpriced run hides priced total")
	}
}

// 3: the page links every stream name to /stream/<key> (charts.js); the shim answers 404.
func TestColdreadDefectStreamPagesDead(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/charts.js")
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(&Cache{at: t1000, now: at1000})
	for _, p := range []string{"/stream/s00", "/api/stream/s00"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code == 404 && strings.Contains(string(js), `"/stream/"`) {
			t.Errorf("%s -> 404 while charts.js still wires stream-name clicks to /stream/", p)
		}
	}
}
