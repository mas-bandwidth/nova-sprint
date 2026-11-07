package run

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func applyOK(t *testing.T, s store.Store, o store.Op) {
	t.Helper()
	r, err := s.Apply(context.Background(), o)
	if err != nil || !r.Accepted {
		t.Fatalf("op %s: %+v %v", o.ID, r, err)
	}
}
func TestPushHistoryMatchesRebuildAcrossClear(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	applyOK(t, f, op("seed", store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutPolicy{Kind: "script", Name: "p", Body: "x"}))
	applyOK(t, f, op("activate", store.SetActivePolicy{Version: 1}))
	if _, err := f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	next := op("next", store.PutStream{Name: "new"})
	next.Epoch = 2
	applyOK(t, f, next)
	events, err := f.Events(ctx, 0, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	view := store.Rebuild(nil)
	for i, e := range events {
		applyHistory(&view, e)
		want := store.Rebuild(events[:i+1])
		if !reflect.DeepEqual(view, want) {
			t.Fatalf("seq=%d view=%+v want=%+v", e.Seq, view, want)
		}
	}
}
func TestPushStitchUsesHistoricalLandingState(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	applyOK(t, f, op("seed", store.PutStream{Name: "s"}, store.PutCard{ID: "a", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutCard{ID: "b", Kind: "producer", Tier: "pro", Stream: "s"}))
	applyOK(t, f, op("land-a", store.SetCardState{Card: "a", State: "landed"}))
	applyOK(t, f, op("land-b", store.SetCardState{Card: "b", State: "landed"}))
	applyOK(t, f, op("later", store.PutCard{ID: "c", Kind: "producer", Tier: "pro", Stream: "s"}))
	snap, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	render := func(cursor int64) string {
		p := NewPusher(f, EventNotices{Coordinator: "rowan"}, &testSender{})
		view, err := p.prepare(ctx, "rowan", cursor)
		if err != nil {
			t.Fatal(err)
		}
		_, batches, _, err := p.collect(ctx, "rowan", view, snap.Seq, snap.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range batches {
			if b.subject == "STREAM LANDED" {
				return strings.Join(b.lines, "\n")
			}
		}
		return ""
	}
	a, b := render(0), render(2)
	if a == "" || a != b || !strings.Contains(a, "s 2 landed") || !strings.Contains(a, "show s --stitch") {
		t.Fatalf("cursor0=%q cursor2=%q", a, b)
	}
}
func TestPushTerminalStreamWithFailure(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	applyOK(t, f, op("seed", store.PutStream{Name: "s"}, store.PutCard{ID: "a", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutCard{ID: "b", Kind: "producer", Tier: "pro", Stream: "s"}))
	applyOK(t, f, op("land", store.SetCardState{Card: "b", State: "landed"}))
	applyOK(t, f, op("fail", store.SetCardState{Card: "a", State: "fail"}))
	snap, _ := f.Snapshot(ctx)
	p := NewPusher(f, EventNotices{Coordinator: "rowan"}, &testSender{})
	view, err := p.prepare(ctx, "rowan", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, batches, _, err := p.collect(ctx, "rowan", view, snap.Seq, snap.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range batches {
		if b.subject == "STREAM LANDED" {
			t.Fatal("failed producer reported as landed")
		}
		if b.subject == "STREAM DONE WITH FAILURES" {
			found = strings.Contains(b.lines[0], "1 landed 1 failed ids=a")
		}
	}
	if !found {
		t.Fatalf("missing terminal failure: %+v", batches)
	}
}

func TestPushRetryStitchStableAfterLaterMutation(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	applyOK(t, f, op("seed", store.PutStream{Name: "s"}, store.PutCard{ID: "a", Kind: "producer", Tier: "pro", Stream: "s"}))
	applyOK(t, f, op("land", store.SetCardState{Card: "a", State: "landed"}))
	snap, _ := f.Snapshot(ctx)
	sender := &testSender{failAt: 2}
	p := NewPusher(f, EventNotices{Coordinator: "rowan"}, sender)
	if err := p.Push(ctx, "rowan", snap, time.Unix(1000, 0)); err == nil {
		t.Fatal("expected second batch to fail")
	}
	if len(sender.sends) != 2 || !strings.HasPrefix(sender.sends[1], "STREAM LANDED:") {
		t.Fatalf("first=%v", sender.sends)
	}
	first := sender.sends[1]
	applyOK(t, f, op("later", store.PutCard{ID: "b", Kind: "producer", Tier: "pro", Stream: "s"}))
	snap, _ = f.Snapshot(ctx)
	sender.failAt = 0
	if err := p.Push(ctx, "rowan", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sends) != 4 || sender.sends[3] != first {
		t.Fatalf("first=%q retry=%v", first, sender.sends)
	}
}

func TestPushClearDropsOldEpochBacklog(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	applyOK(t, f, op("seed", store.PutStream{Name: "s"}, store.PutRow{Name: "alice", Kind: "worker", Width: 1, Tiers: []string{"pro"}}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutCard{ID: "w", Kind: "work", Of: "p", Tier: "pro", Stream: "s", Holder: "alice"}, store.SetCardState{Card: "w", State: "ready"}))
	if _, err := f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	next := op("new", store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutCard{ID: "w", Kind: "work", Of: "p", Tier: "pro", Stream: "s", Holder: "alice"}, store.SetCardState{Card: "w", State: "ready"})
	next.Epoch = 2
	applyOK(t, f, next)
	snap, _ := f.Snapshot(ctx)
	sender := &testSender{}
	p := NewPusher(f, EventNotices{Coordinator: "rowan"}, sender)
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sends) != 2 || !strings.Contains(sender.sends[0], "epoch 1 cleared: 1 pending notices dropped (some may have reached you)") || !strings.Contains(sender.sends[1], "--epoch 2") || strings.Contains(sender.sends[1], "--epoch 1") {
		t.Fatalf("sends=%v", sender.sends)
	}
	fresh, _ := f.Snapshot(ctx)
	if fresh.Cursors["push:alice"] != 3 {
		t.Fatalf("cursor=%d", fresh.Cursors["push:alice"])
	}
}
func TestPushClearAfterPartialSendDoesNotResendOld(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	pushSeed(t, f, "a", "old")
	snap, _ := f.Snapshot(ctx)
	sender := &testSender{failAt: 2}
	notices := noticeMap{"old": {{Subject: "DEAL", Line: "old deal"}, {Subject: "NEEDS YOU", Line: "old judgement"}}}
	p := NewPusher(f, notices, sender)
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err == nil {
		t.Fatal("expected partial failure")
	}
	if _, err := f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	snap, _ = f.Snapshot(ctx)
	sender.failAt = 0
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sends) != 3 || !strings.Contains(sender.sends[2], "epoch 1 cleared: 2 pending notices dropped (some may have reached you)") {
		t.Fatalf("sends=%v", sender.sends)
	}
}
