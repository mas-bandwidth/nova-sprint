package dash

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestAdvanceSnapshotKeepsStopAcrossOrdinaryEvent(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	write := func(id string, w store.Write) {
		t.Helper()
		r, err := f.Apply(ctx, store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "test", At: time.Unix(1, 0), Writes: []store.Write{w}})
		if err != nil || !r.Accepted {
			t.Fatalf("%s: %+v %v", id, r, err)
		}
	}
	write("stop", store.SetStopped{Stopped: true, Reason: "pause"})
	s, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	write("cursor", store.SetCursor{Name: "reader", Seq: s.Seq})
	events, err := f.Events(ctx, s.Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := advanceSnapshot(s, events)
	if !got.Stopped || got.StopReason != "pause" || got.Cursors["reader"] != s.Seq {
		t.Fatalf("snapshot: %+v", got)
	}
}
