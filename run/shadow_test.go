package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestShadowPlansWithoutApplying(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	applyOK(t, base, op("seed", store.PutStream{Name: "s"}))
	spy := &countStore{Store: base}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("move", store.PutStream{Name: "another"})}, Edges: []EdgeRequest{{ID: "would-send"}}}}}
	result, err := Shadow(ctx, spy, steps, TickInput{At: time.Unix(1000, 0)})
	if err != nil || spy.snapshots != 1 || spy.applies != 0 || result.Seq != 1 || len(result.Plan.Ops) != 1 || !result.Valid || !strings.Contains(result.Validation, "IDs used before the snapshot") {
		t.Fatalf("result=%+v err=%v snapshots=%d applies=%d", result, err, spy.snapshots, spy.applies)
	}
	fresh, _ := base.Snapshot(ctx)
	if fresh.Seq != 1 || len(fresh.Streams) != 1 {
		t.Fatalf("state changed: %+v", fresh)
	}
}

func TestShadowValidatesSequentialOperations(t *testing.T) {
	ctx := context.Background()
	s := store.NewFake()
	second := op("two", store.PutStream{Name: "s"})
	second.Expect = []store.Expect{store.AtSeq{Seq: 0}}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("one", store.PutStream{Name: "s"}), second}}}}
	result, err := Shadow(ctx, s, steps, TickInput{})
	if err != nil || result.Valid || result.FailedOp != "two" || len(result.Receipts) != 2 || !result.Receipts[0].Accepted || result.Receipts[1].Accepted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	snap, _ := s.Snapshot(ctx)
	if snap.Seq != 0 || len(snap.Streams) != 0 {
		t.Fatalf("live store mutated: %+v", snap)
	}
}
