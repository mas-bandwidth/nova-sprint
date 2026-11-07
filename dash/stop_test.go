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
	write("start", store.SetStopped{Stopped: false, Reason: "go"})
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
	got := store.Fold(s, events)
	if !got.Stopped || got.StopReason != "pause" || got.Cursors["reader"] != s.Seq {
		t.Fatalf("snapshot: %+v", got)
	}
}

// The board of a fresh store shows STOPPED with its reason, never started,
// until the coordinator's first start (Glenn, 2026-11-07).
func TestFreshStoreShowsStoppedNeverStarted(t *testing.T) {
	s, err := store.NewFake().Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := machineLine(s); got != "machine: STOPPED never started" || !s.Stopped || s.StopReason != store.NeverStarted {
		t.Fatalf("a fresh store's board: %q (stopped %v, reason %q)", got, s.Stopped, s.StopReason)
	}
}
