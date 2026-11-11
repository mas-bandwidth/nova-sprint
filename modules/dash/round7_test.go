package dash

import (
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestRound7GzipNegotiation(t *testing.T) {
	t.Parallel()
	c := &Cache{at: t1000, now: at1000, view: mapSnapshot(store.Snapshot{}, testRoster, t1000, nil)}
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
func TestRound7ExactWorkCostWire(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {Kind: "work", Stream: "s", Tier: "pro", Priced: true, Cost: 9_000_000_000_000_010_000}}}
	d := mapSnapshot(s, testRoster, time.Time{}, nil)
	w := d.Tables["work"].(map[string]*workRow)["s"]
	want := big.NewInt(900_000_000_000_001)
	if w.Cost.Cmp(want) != 0 {
		t.Fatalf("cents %v", w.Cost)
	}
	if got := moneyCell(w.Cost); got != "$9,000,000,000,000.01" {
		t.Fatal(got)
	}
}
