package run

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStoppedFoldsAndRetainsIncoming(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	applyOK(t, f, op("stop", store.SetStopped{Stopped: true, Reason: "pause"}))
	steps, edges := &tickSteps{}, &tickEdges{}
	e, err := NewEngine(ctx, f, &tickGuard{}, steps, edges)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, []EdgeResult{{ID: "result", Epoch: 1}}); err != nil {
		t.Fatal(err)
	}
	if len(steps.inputs) != 0 || len(edges.plans) != 0 || !e.state.Stopped {
		t.Fatal("stopped engine planned or dispatched")
	}
	applyOK(t, f, op("cursor", store.SetCursor{Name: "test", Seq: 1}))
	if err = e.Tick(ctx, nil); err != nil || !e.state.Stopped || e.state.StopReason != "pause" || len(steps.inputs) != 0 {
		t.Fatalf("stopped fold: state=%+v steps=%d err=%v", e.state, len(steps.inputs), err)
	}
	applyOK(t, f, op("start", store.SetStopped{Stopped: false, Reason: "resume"}))
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(steps.inputs) != 1 || len(steps.inputs[0].Inbox) != 1 || steps.inputs[0].Inbox[0].ID != "result" {
		t.Fatalf("inputs=%+v", steps.inputs)
	}
}

func TestStoppedBoardRefreshesWhenBusWhoFails(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	steps := &tickSteps{}
	e, err := NewEngine(ctx, f, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	applyOK(t, f, op("stop", store.SetStopped{Stopped: true, Reason: "pause"}))
	pub := &fakePublisher{}
	who := &fakePresence{err: errors.New("bus unavailable")}
	s, err := NewScheduler(e, &fakeClock{at: time.Unix(1000, 0)}, who, pub, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Step(ctx, []EdgeResult{{ID: "later", Epoch: 1}}); err == nil {
		t.Fatal("missing bus failure")
	}
	if len(pub.snapshots) != 1 || !pub.snapshots[0].Stopped || pub.snapshots[0].StopReason != "pause" || len(steps.inputs) != 0 || len(s.queued) != 1 {
		t.Fatalf("published=%+v steps=%d queued=%+v", pub.snapshots, len(steps.inputs), s.queued)
	}
}

type unknownBeforeApply struct {
	store.Store
	ops []store.Op
}

func (s *unknownBeforeApply) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	s.ops = append(s.ops, o)
	if len(s.ops) == 1 {
		return store.Receipt{}, store.ErrUnknown
	}
	return s.Store.Apply(ctx, o)
}

func TestUnknownRetryKeepsFenceAcrossStop(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	s := &unknownBeforeApply{Store: f}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("deal", store.PutStream{Name: "s"})}}}}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); !errors.Is(err, store.ErrUnknown) {
		t.Fatalf("first apply: %v", err)
	}
	applyOK(t, f, op("stop", store.SetStopped{Stopped: true, Reason: "pause"}))
	if err = e.Tick(ctx, nil); err != nil || len(s.ops) != 1 {
		t.Fatalf("stopped retry: %v, applies=%d", err, len(s.ops))
	}
	applyOK(t, f, op("resume", store.SetStopped{Stopped: false, Reason: "resume"}))
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.ops) != 2 || !reflect.DeepEqual(s.ops[0], s.ops[1]) || len(e.refused) != 1 || len(e.state.Streams) != 0 {
		t.Fatalf("ops=%+v refusals=%+v state=%+v", s.ops, e.refused, e.state)
	}
}

type stopBeforeApply struct {
	store.Store
	stopped bool
}

func (s *stopBeforeApply) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	if !s.stopped {
		s.stopped = true
		if _, err := s.Store.Apply(ctx, op("stop-race", store.SetStopped{Stopped: true, Reason: "pause"})); err != nil {
			return store.Receipt{}, err
		}
	}
	return s.Store.Apply(ctx, o)
}

func TestStopBetweenFoldAndApplyRefusesTickWrite(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	s := &stopBeforeApply{Store: f}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("deal", store.PutStream{Name: "s"})}}}}
	edges := &tickEdges{}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, edges)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !e.state.Stopped || len(e.refused) != 1 || len(e.state.Streams) != 0 || len(edges.plans) != 0 {
		t.Fatalf("state=%+v refusals=%+v edges=%d", e.state, e.refused, len(edges.plans))
	}
}

type stopAfterApply struct {
	store.Store
	stopped bool
}

func (s *stopAfterApply) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	r, err := s.Store.Apply(ctx, o)
	if err == nil && !s.stopped {
		s.stopped = true
		_, err = s.Store.Apply(ctx, op("stop-after", store.SetStopped{Stopped: true, Reason: "pause"}))
	}
	return r, err
}

func TestStopBetweenOpsRetainsPendingAndEdges(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	s := &stopAfterApply{Store: f}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("first", store.PutStream{Name: "a"}), op("second", store.PutStream{Name: "b"})}, Edges: []EdgeRequest{{ID: "edge", After: []string{"second"}}}}}}
	edges := &tickEdges{}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, edges)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !e.state.Stopped || e.pending == nil || e.pending.next != 1 || len(edges.plans) != 0 || len(e.state.Streams) != 1 {
		t.Fatalf("pending=%+v edges=%+v state=%+v", e.pending, edges.plans, e.state)
	}
	applyOK(t, f, op("resume", store.SetStopped{Stopped: false, Reason: "resume"}))
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if e.pending != nil || len(e.state.Streams) != 2 || len(edges.plans) != 1 || len(edges.plans[0].Edges) != 1 {
		t.Fatalf("pending=%+v edges=%+v state=%+v", e.pending, edges.plans, e.state)
	}
}
