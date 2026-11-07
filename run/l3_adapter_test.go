package run

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type l3TestHeads struct{}

func (l3TestHeads) Heads(context.Context, store.Snapshot) (map[string]string, error) {
	return map[string]string{}, nil
}

type l3TestAsker struct{}

func (l3TestAsker) ForTick(context.Context, TickInput) (steps.Asker, error) {
	return steps.Default{}, nil
}

func TestL3ViewFoldsWithoutResettingSince(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	clock := &fakeClock{at: time.Unix(1000, 0)}
	a := &L3Adapter{Clock: clock, Heads: l3TestHeads{}, Asker: l3TestAsker{}, Edges: &tickEdges{}}
	e, err := NewEngine(ctx, f, &tickGuard{}, a, a)
	if err != nil {
		t.Fatal(err)
	}
	applyOK(t, f, op("stream", store.PutStream{Name: "s"}))
	clock.at = clock.at.Add(time.Minute)
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if a.view.Seq != 1 || a.view.Streams["s"].Name != "s" || !a.view.Since.Equal(time.Unix(1000, 0)) {
		t.Fatalf("view=%+v", a.view)
	}
}

func TestL3InflightWaitsForConsumedReceipt(t *testing.T) {
	a := &L3Adapter{Edges: &tickEdges{}, view: &steps.View{Epoch: 1}, planEpoch: 1, inflight: map[string]steps.EdgeRequest{"req": {ID: "req"}}, items: map[string]steps.InboxItem{"item": {ID: "item", Request: "req"}}}
	a.last = steps.Plan{Carry: []steps.InboxItem{{ID: "item"}}}
	if err := a.Committed(context.Background(), TickPlan{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.inflight["req"]; !ok {
		t.Fatal("carried result removed request")
	}
	a.last = steps.Plan{}
	if err := a.Committed(context.Background(), TickPlan{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.inflight["req"]; ok {
		t.Fatal("consumed result retained request")
	}
}

type clearAfterApply struct{ store.Store; cleared bool }

func (s *clearAfterApply) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	r, err := s.Store.Apply(ctx, o)
	if err == nil && !s.cleared {
		s.cleared = true
		clear := op("clear")
		clear.Kind = "clear"
		_, err = s.Store.Clear(ctx, clear)
	}
	return r, err
}

func TestClearDiscardsPendingIndependentEdgeAndOldResults(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	s := &clearAfterApply{Store: f}
	edges := &tickEdges{}
	planner := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("write", store.PutStream{Name: "s"})}, Edges: []EdgeRequest{{ID: "old-land"}}}}}
	e, err := NewEngine(ctx, s, &tickGuard{}, planner, edges)
	if err != nil { t.Fatal(err) }
	if err = e.Tick(ctx, []EdgeResult{{ID: "old-result", Epoch: 1}}); err != nil { t.Fatal(err) }
	if e.state.Epoch != 2 || e.pending != nil || len(edges.plans) != 0 || len(e.inbox) != 0 { t.Fatalf("epoch=%d pending=%+v edges=%+v inbox=%+v", e.state.Epoch, e.pending, edges.plans, e.inbox) }
	items, err := l3Inbox([]EdgeResult{{ID: "stale", Epoch: 1}, {ID: "new", Epoch: 2, Payload: steps.InboxItem{ID: "new", Kind: "land"}}}, 2)
	if err != nil || len(items) != 1 || items[0].ID != "new" { t.Fatalf("items=%+v err=%v", items, err) }
}
