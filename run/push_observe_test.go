package run

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestCollectionObserverCannotMutateDelivery(t *testing.T) {
	ctx := context.Background()
	s := store.NewFake()
	pushSeed(t, s, "a", "deal")
	snap, _ := s.Snapshot(ctx)
	sender := &testSender{}
	p := NewPusher(s, noticeMap{"deal": {{Subject: "DEAL", Line: "original"}}}, sender)
	called := false
	p.observeCollect = func(o pushCollection) {
		called = true
		if o.From != 0 || o.End != 1 || o.Through != 1 || len(o.Batches) != 1 {
			t.Fatalf("observation %+v", o)
		}
		o.Batches[0].lines[0] = "changed"
	}
	if err := p.Push(ctx, "alice", snap, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if !called || len(sender.sends) != 1 || sender.sends[0] != "DEAL:original" {
		t.Fatalf("called=%v sends=%v", called, sender.sends)
	}
}
