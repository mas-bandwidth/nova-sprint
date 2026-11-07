package dash

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type cursorUnavailable struct{ store.Store }

func (s cursorUnavailable) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" {
		return store.Receipt{}, errors.New("cursor unavailable")
	}
	return s.Store.Apply(ctx, op)
}
func TestRound7CursorFailurePublishesRead(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro"})
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, f, "land", 1, store.SetCardState{Card: "p", State: "landed"})
	if err := c.Advance(ctx, cursorUnavailable{f}); err == nil {
		t.Fatal("missing cursor warning")
	}
	d := c.pageSnapshot().Data
	if !d.OK || d.Landed != 1 || !c.pending {
		t.Fatalf("view=%+v pending=%v", d, c.pending)
	}
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	if c.pending {
		t.Fatal("cursor retry remained pending")
	}
}
func TestRound7OlderLogResnapshots(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro"})
	apply(t, f, "land", 1, store.SetCardState{Card: "p", State: "landed"})
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	older := store.NewFake()
	apply(t, older, "old", 1, store.PutStream{Name: "old"})
	if err := c.Advance(ctx, older); err != nil {
		t.Fatal(err)
	}
	d := c.pageSnapshot().Data
	if !d.OK || d.Landed != 0 || d.All != 0 || len(d.Streams) != 1 || d.Streams[0].Stream != "old" {
		t.Fatalf("view=%+v", d)
	}
}
func TestRound7ChangedHistoryFailsClosed(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"})
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	other := store.NewFake()
	apply(t, other, "other-seed", 1, store.PutStream{Name: "other"})
	apply(t, other, "other-held", 1, store.SetStreamHeld{Stream: "other", Held: true})
	if err := c.Advance(ctx, other); err == nil {
		t.Fatal("changed history accepted")
	}
	if c.pageSnapshot().OK {
		t.Fatal("changed history reported healthy")
	}
}
func TestRound7GzipNegotiation(t *testing.T) {
	c := &Cache{at: time.Now(), view: mapSnapshot(store.Snapshot{}, testRoster, time.Now())}
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
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {Kind: "work", Stream: "s", Tier: "pro", Priced: true, Cost: 9_000_000_000_000_010_000}}}
	d := mapSnapshot(s, testRoster, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	want := big.NewInt(900_000_000_000_001)
	if w.Cost.Cmp(want) != 0 {
		t.Fatalf("cents %v", w.Cost)
	}
	if got := moneyCell(w.Cost); got != "$9,000,000,000,000.01" {
		t.Fatal(got)
	}
}

type unavailableSnapshot struct {
	store.Store
	unavailable bool
}

func (s *unavailableSnapshot) Snapshot(ctx context.Context) (store.Snapshot, error) {
	if s.unavailable {
		return store.Snapshot{}, errors.New("store unavailable")
	}
	return s.Store.Snapshot(ctx)
}
func TestRound7StartupRecovers(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro"})
	s := &unavailableSnapshot{Store: f, unavailable: true}
	c, err := NewRecovering(ctx, s)
	if err == nil || c == nil {
		t.Fatal("expected unavailable cache and startup error")
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 || c.pageSnapshot().OK {
		t.Fatalf("unavailable cache health=%d", rec.Code)
	}
	if err := c.Advance(ctx, s); err == nil {
		t.Fatal("unavailable poll succeeded")
	}
	s.unavailable = false
	if err := c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	d := c.pageSnapshot().Data
	if rec.Code != 200 || !d.OK || d.All != 1 {
		t.Fatalf("recovered health=%d data=%+v", rec.Code, d)
	}
}
