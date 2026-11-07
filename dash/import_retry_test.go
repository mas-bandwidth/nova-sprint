package dash

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type importRetryStore struct {
	store.Store
	failCursor    bool
	failIdentity  bool
	identityReads int
	importReplays int
}

func (s *importRetryStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" && s.failCursor {
		s.failCursor = false
		return store.Receipt{}, context.DeadlineExceeded
	}
	return s.Store.Apply(ctx, op)
}
func (s *importRetryStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	if id == "ordinary-import" {
		s.importReplays++
		return store.Receipt{}, false, errors.New("unexpected import image replay")
	}
	return s.Store.Replay(ctx, id)
}
func (s *importRetryStore) EventIdentity(ctx context.Context, seq int64) (string, int, bool, error) {
	s.identityReads++
	if s.failIdentity {
		s.failIdentity = false
		return "", 0, false, context.DeadlineExceeded
	}
	return s.Store.EventIdentity(ctx, seq)
}
func TestImportCursorRetryUsesIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	writes := []store.Write{store.PutStream{Name: "example"}}
	for i := 0; i < 399; i++ {
		writes = append(writes, store.PutCard{ID: fmt.Sprintf("card-%03d", i), Kind: "producer", Stream: "example", Tier: "pro"})
	}
	r, err := f.Apply(ctx, store.Op{ID: "ordinary-import", Epoch: 1, Actor: "test", Kind: "import", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Writes: writes})
	if err != nil || !r.Accepted {
		t.Fatalf("import accepted=%v error=%v", r.Accepted, err)
	}
	s := &importRetryStore{Store: f, failCursor: true}
	if err = c.Advance(ctx, s); !errors.Is(err, context.DeadlineExceeded) || c.view.All != 399 || !c.pageSnapshot().OK {
		t.Fatalf("first poll err=%v view=%+v", err, c.view)
	}
	s.failIdentity = true
	if err = c.Advance(ctx, s); !errors.Is(err, context.DeadlineExceeded) || c.view.All != 399 {
		t.Fatalf("timed-out identity did not retain the last view: %v", err)
	}
	if err = c.Advance(ctx, s); err != nil {
		t.Fatalf("retry: %v", err)
	}
	snap, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cursors[CursorName] < r.Seq || s.identityReads < 2 || s.importReplays != 0 {
		t.Fatalf("cursor=%d import=%d identity=%d replay=%d", snap.Cursors[CursorName], r.Seq, s.identityReads, s.importReplays)
	}
	before := snap.Seq
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	snap, err = f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Seq != before {
		t.Fatal("idle poll rewrote cursor")
	}
}
