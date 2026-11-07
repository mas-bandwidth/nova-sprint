package dash

import (
	"context"
	"errors"
	"github.com/mas-bandwidth/nova-sprint/store"
	"reflect"
	"testing"
	"time"
)

type interveningStore struct {
	store.Store
	injected bool
}

func (s *interveningStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" && !s.injected {
		s.injected = true
		r, e := s.Store.Apply(ctx, store.Op{ID: "intervening", Epoch: 1, Actor: "test", Kind: "change", At: time.Now(), Writes: []store.Write{store.SetCardReason{Card: "p", Reason: "intervening"}}})
		if e != nil || !r.Accepted {
			panic("intervening write failed")
		}
	}
	return s.Store.Apply(ctx, op)
}
func TestCursorIncludesInterveningEvent(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"})
	s := &interveningStore{Store: f}
	c, e := New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	apply(t, f, "change", 1, store.SetCardState{Card: "p", State: "ready"})
	if e = c.Advance(ctx, s); e != nil {
		t.Fatal(e)
	}
	fresh, e := f.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if fresh.Cards["p"].Reason != "intervening" || !reflect.DeepEqual(c.state, fresh) {
		t.Fatalf("cursor skipped an intervening event: cached=%+v fresh=%+v", c.state.Cards["p"], fresh.Cards["p"])
	}
	before := fresh.Seq
	if e = c.Advance(ctx, s); e != nil {
		t.Fatal(e)
	}
	fresh, e = f.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if fresh.Seq != before {
		t.Fatalf("idle advance wrote another cursor: %d to %d", before, fresh.Seq)
	}
}

type cursorFaultStore struct {
	store.Store
	clear, lose bool
	replayed    bool
}

func (s *cursorFaultStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" && s.clear {
		s.clear = false
		_, e := s.Store.Clear(ctx, store.Op{ID: "clear-race", Epoch: 1, Actor: "test", Kind: "clear", At: time.Now()})
		if e != nil {
			return store.Receipt{}, e
		}
	}
	r, e := s.Store.Apply(ctx, op)
	if op.Kind == "cursor" && s.lose && e == nil && r.Accepted {
		s.lose = false
		return store.Receipt{}, store.ErrUnknown
	}
	return r, e
}
func (s *cursorFaultStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	s.replayed = true
	return s.Store.Replay(ctx, id)
}
func TestCursorUnknownReplay(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"})
	s := &cursorFaultStore{Store: f, lose: true}
	c, e := New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	apply(t, f, "change", 1, store.SetStreamHeld{Stream: "s", Held: true})
	if e = c.Advance(ctx, s); e != nil {
		t.Fatal(e)
	}
	snap, e := f.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !s.replayed || !reflect.DeepEqual(c.state, snap) {
		t.Fatal("lost cursor reply was not replayed and folded")
	}
}
func TestCursorClearRaceRecovers(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	apply(t, f, "seed", 1, store.PutStream{Name: "s"})
	s := &cursorFaultStore{Store: f, clear: true}
	c, e := New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	before := c.pageSnapshot().Data.(envelope)
	apply(t, f, "change", 1, store.SetStreamHeld{Stream: "s", Held: true})
	if e = c.Advance(ctx, s); e == nil {
		t.Fatal("stale epoch cursor was not refused")
	}
	after := c.pageSnapshot().Data.(envelope)
	if after.At != before.At || after.Epoch != before.Epoch {
		t.Fatal("failed advance changed last-good view")
	}
	if e = c.Advance(ctx, s); e != nil {
		t.Fatal(e)
	}
	snap, e := f.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(c.state, snap) || snap.Epoch != 2 {
		t.Fatal("failed to recover clear race")
	}
}
func TestCursorColdStartIdle(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	c, e := New(ctx, f)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Advance(ctx, f); e != nil {
		t.Fatal(e)
	}
	snap, e := f.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := snap.Cursors[CursorName]; !ok {
		t.Fatal("missing cold cursor")
	}
	before := snap.Seq
	if e = c.Advance(ctx, f); e != nil {
		t.Fatal(e)
	}
	snap, e = f.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if snap.Seq != before {
		t.Fatal("idle poll wrote cursor again")
	}
}

type blockingStore struct{ store.Store }

func (s blockingStore) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestAdvanceDeadline(t *testing.T) {
	defer func(d time.Duration) { AdvanceDeadline = d }(AdvanceDeadline)
	AdvanceDeadline = 500 * time.Millisecond
	ctx := context.Background()
	f := store.NewFake()
	c, e := New(ctx, f)
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	e = c.Advance(ctx, blockingStore{f})
	elapsed := time.Since(start)
	if !errors.Is(e, context.DeadlineExceeded) || elapsed > time.Second {
		t.Fatalf("error=%v elapsed=%s", e, elapsed)
	}
}
