package run

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The ticker keeps the board's stats mark (store.StatsCursor): none until
// it is set; then each consumer done after it, as the cold read finds them
// and as the fold sees them; a later reset counts from itself.
func TestTickerKeepsTheStatsMark(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _ := seeded(t)
	work := func(id string) {
		must(t, f, "put-"+id, store.PutCard{ID: id, Kind: "work", Of: "p1", Stream: "s", Holder: "alice", Tier: "pro"})
	}
	finish := func(id string) { must(t, f, "done-"+id, store.SetCardState{Card: id, State: "done"}) }
	reset := func(id string, at time.Time) {
		s, err := f.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		r, err := f.Apply(ctx, store.Op{ID: id, Epoch: 1, Actor: "carol", Kind: "stats-reset", At: at, Writes: []store.Write{store.SetCursor{Name: store.StatsCursor, Seq: s.Seq}}})
		if err != nil || !r.Accepted {
			t.Fatalf("%s: %+v %v", id, r, err)
		}
	}
	for _, id := range []string{"w0", "w1", "w2", "w3"} {
		work(id)
	}
	finish("w0")
	tk, err := NewTicker(ctx, f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if m := tk.Stats(); m != nil {
		t.Fatalf("no mark: %+v", m)
	}
	first := t0.Add(time.Minute)
	reset("reset-1", first)
	finish("w1")
	if err := tk.Fold(ctx); err != nil {
		t.Fatal(err)
	}
	m := tk.Stats()
	if m == nil || !m.At.Equal(first) || !sameKeys(m.Done, "w1") {
		t.Fatalf("after the reset: %+v, want w1 since %s", m, first)
	}
	finish("w2")
	if err := tk.Fold(ctx); err != nil {
		t.Fatal(err)
	}
	if m := tk.Stats(); !sameKeys(m.Done, "w1", "w2") {
		t.Fatalf("the fold adds w2: %+v", m)
	}
	cold, err := NewTicker(ctx, f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if m := cold.Stats(); m == nil || !m.At.Equal(first) || !sameKeys(m.Done, "w1", "w2") {
		t.Fatalf("a cold ticker reads the same mark: %+v", m)
	}
	second := t0.Add(2 * time.Minute)
	reset("reset-2", second)
	finish("w3")
	if err := tk.Fold(ctx); err != nil {
		t.Fatal(err)
	}
	if m := tk.Stats(); m == nil || !m.At.Equal(second) || !sameKeys(m.Done, "w3") {
		t.Fatalf("a second reset counts from itself: %+v", m)
	}
	m = tk.Stats()
	m.Done["x"] = 1
	if _, ok := tk.Stats().Done["x"]; ok {
		t.Fatal("Stats is the caller's to keep")
	}
}

func sameKeys(m map[string]int64, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
