package dash

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"reflect"
	"testing"
	"time"
)

func apply(t *testing.T, s store.Store, id string, epoch int, w ...store.Write) store.Receipt {
	t.Helper()
	r, e := s.Apply(context.Background(), store.Op{ID: id, Epoch: epoch, Actor: "test", Kind: id, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Writes: w})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestFoldSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.NewFake()
	apply(t, s, "seed", 1, store.PutStream{Name: "s"}, store.PutRow{Name: "r"}, store.PutPolicy{Kind: "data", Name: "p", Body: "x"}, store.SetActivePolicy{Version: 1})
	c, e := New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	apply(t, s, "refuse", 1, store.SetRowState{Row: "r", State: "down"})
	apply(t, s, "card", 1, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro"})
	apply(t, s, "cursor", 1, store.SetCursor{Name: "x", Seq: 1})
	_, e = s.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if e != nil {
		t.Fatal(e)
	}
	apply(t, s, "policy", 2, store.PutPolicy{Kind: "data", Name: "q", Body: "y"}, store.SetActivePolicy{Version: 2})
	if e = c.Advance(ctx, s); e != nil {
		t.Fatal(e)
	}
	fresh, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(c.state, fresh) {
		t.Fatalf("fold=%+v snapshot=%+v", c.state, fresh)
	}
}
