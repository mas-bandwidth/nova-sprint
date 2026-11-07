package dash

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestOlderLogTakesNewSnapshot checks that when the store's log is restored behind the dashboard's
// position, the dashboard takes a new Snapshot.
func TestOlderLogTakesNewSnapshot(t *testing.T) {
	t.Parallel()
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

// TestChangedHistoryFailsClosed checks that a store whose history differs at the dashboard's anchor fails
// the read and reports unhealthy.
func TestChangedHistoryFailsClosed(t *testing.T) {
	t.Parallel()
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
