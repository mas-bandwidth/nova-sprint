package bus

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Cold read 2, 2026-11-06, at ef4df30c (Rowan's cold reader). One failing
// probe per confirmed defect.

type cr2CountingStore struct {
	store.Store
	calls int
}

func (s *cr2CountingStore) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	s.calls++
	if s.calls > 10 {
		return nil, errors.New("probe bound: Events called more than 10 times")
	}
	return s.Store.Events(ctx, after, max)
}

// Round 2 ruling (bus): "The 4 KB cap works (the loop stops)" and "a
// recipient's cursor advances past events that carry nothing for it, so a
// window of irrelevant events never strands anyone". The store logs refused
// ops as events with Accepted=false (store/apply.go:100). A full window of
// them gives maxAcceptedSeq_internal == cursor (bus.go:389-398), so the
// cursor does not move, yet pushWindow_internal reports more=true because the
// window was full (bus.go:429): PushRecipient re-reads the same window forever.
func TestColdread2DefectPushLoopsForeverOnRefusedWindow(t *testing.T) {
	t.Parallel()
	st, t0 := crSeed_internal(t, "r1")
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		rec, err := st.Apply(ctx, store.Op{ID: fmt.Sprintf("op-stale-%d", i), Epoch: 99, Actor: "sprint", Kind: "deal", At: t0,
			Writes: []store.Write{store.PutCard{ID: fmt.Sprintf("x%d", i), Kind: "producer", Stream: "s1", Tier: "pro"}}})
		if err != nil || rec.Accepted {
			t.Fatalf("setup: want a refused op, got %+v err=%v", rec, err)
		}
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := st.Events(ctx, 0, 0)
	cur := evs[0].Seq // past the seed op; only the two refused events remain
	_, err = st.Apply(ctx, store.Op{ID: "op-cur", Epoch: snap.Epoch, Actor: "sprint", Kind: "cursor", At: t0, Writes: []store.Write{store.SetCursor{Name: CursorName("r1"), Seq: cur}}})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ = st.Snapshot(ctx)
	cs := &cr2CountingStore{Store: st}
	e := New(Config{Transport: NewFake(), Window: 2})
	_, _ = e.PushRecipient(ctx, cs, snap, "r1", "p", t0)
	if cs.calls > 3 {
		t.Errorf("PushRecipient read the same window %d times without moving the cursor: the push loop never stops on a window of refused events", cs.calls)
	}
}
