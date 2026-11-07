package dash

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestIdleFirstPollOnEmptyStoreWritesNoCursor checks that the first poll of an empty store, with no
// events to consume, writes no cursor event.
func TestIdleFirstPollOnEmptyStoreWritesNoCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.NewFake()
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Seq != 0 {
		t.Fatalf("idle first poll on empty store created event(s): got store Seq=%d, want 0", snap.Seq)
	}
}

// hookApplyStore runs beforeCursorApply right before the cursor write.
type hookApplyStore struct {
	store.Store
	beforeCursorApply func()
}

func (s *hookApplyStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" && s.beforeCursorApply != nil {
		fn := s.beforeCursorApply
		s.beforeCursorApply = nil
		fn()
	}
	return s.Store.Apply(ctx, op)
}

// TestCursorCoversEventAppliedDuringCursorWrite checks that when another writer's event lands between
// the dashboard's last read and its cursor write, the saved cursor still covers every event the view
// consumed.
//
// Sequence: the dashboard reads e2 and saves a cursor at 2; before that write executes another writer
// applies e3, so the cursor lands after it. The fold then holds e3, so the durable cursor must be at least
// e3's sequence after the following poll.
func TestCursorCoversEventAppliedDuringCursorWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	mustApply(t, f, "e1", 1,
		store.PutStream{Name: "s1"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
	)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	// First Advance saves initial cursor at 1 (lands at Seq=2).
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	// Apply e2 (lands at Seq=3).
	mustApply(t, f, "e2", 1, store.SetCardState{Card: "p1", State: "ready"})

	hs := &hookApplyStore{
		Store: f,
		beforeCursorApply: func() {
			// Concurrent writer applies e3 (lands at Seq=4) right before SetCursor{Seq: 3} (lands at Seq=5).
			mustApply(t, f, "e3", 1, store.SetCardState{Card: "p1", State: "landed"})
		},
	}
	if err := c.Advance(ctx, hs); err != nil {
		t.Fatal(err)
	}
	// Follow-up Advance poll after the concurrent write:
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	snap, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Event e3 (at Seq=4) was consumed into c.state (c.view.Landed == 1),
	// so the saved dashboard cursor watermark must be >= 4!
	if c.view.Landed != 1 {
		t.Fatalf("expected c.view.Landed == 1 after folding e3, got %d", c.view.Landed)
	}
	if got := snap.Cursors[CursorName]; got < 4 {
		t.Fatalf("dashboard consumed e3 at seq 4 (view.Landed=1, state.Seq=%d), but durable cursor watermark is stuck at %d (< 4)", c.state.Seq, got)
	}
}

// cursorUnavailable fails every cursor write.
type cursorUnavailable struct{ store.Store }

func (s cursorUnavailable) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" {
		return store.Receipt{}, errors.New("cursor unavailable")
	}
	return s.Store.Apply(ctx, op)
}

// TestCursorFailureStillPublishesRead checks that a failed cursor write still publishes the read view
// and leaves the cursor pending until a later advance saves it.
func TestCursorFailureStillPublishesRead(t *testing.T) {
	t.Parallel()
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
