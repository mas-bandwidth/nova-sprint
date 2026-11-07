package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type tickGuard struct{ err error }

func (g *tickGuard) Check(context.Context) error { return g.err }

type tickSteps struct {
	plans  []TickPlan
	inputs []TickInput
	views  []store.Snapshot
}

func (s *tickSteps) Tick(_ context.Context, v store.Snapshot, in TickInput) (TickPlan, error) {
	s.inputs = append(s.inputs, in)
	s.views = append(s.views, v)
	if len(s.plans) == 0 {
		return TickPlan{}, nil
	}
	p := s.plans[0]
	s.plans = s.plans[1:]
	return p, nil
}

type tickEdges struct {
	plans    []TickPlan
	receipts [][]store.Receipt
}

func (e *tickEdges) Committed(_ context.Context, p TickPlan, r []store.Receipt) error {
	e.plans = append(e.plans, p)
	e.receipts = append(e.receipts, r)
	return nil
}
func op(id string, writes ...store.Write) store.Op {
	return store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "tick", At: time.Unix(1000, 0), Writes: writes}
}

func TestTickFoldCarryAndRefusalSuppressesEdges(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	guard := &tickGuard{}
	edges := &tickEdges{}
	good := op("good", store.PutStream{Name: "s"})
	bad := op("bad", store.PutStream{Name: "s"})
	bad.Expect = []store.Expect{store.StreamHeld{Stream: "s", Held: true}}
	last := op("never", store.PutStream{Name: "other"})
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{good, bad, last}, Carry: []EdgeResult{{ID: "carry", Epoch: 1}}, Edges: []EdgeRequest{{ID: "yes", After: []string{"good"}}, {ID: "no", After: []string{"bad"}}, {ID: "not-run", After: []string{"never"}}}}}}
	e, err := NewEngine(ctx, f, guard, steps, edges)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, []EdgeResult{{ID: "incoming", Epoch: 1}}); err != nil {
		t.Fatal(err)
	}
	if len(edges.plans) != 1 || len(edges.plans[0].Edges) != 2 || edges.plans[0].Edges[0].ID != "yes" || edges.plans[0].Edges[1].ID != "not-run" {
		t.Fatalf("edges=%+v", edges.plans)
	}
	if err = e.Tick(ctx, []EdgeResult{{ID: "later", Epoch: 1}}); err != nil {
		t.Fatal(err)
	}
	if len(steps.inputs) != 2 || len(steps.inputs[1].Refused) != 1 || steps.inputs[1].Refused[0].Op.ID != "bad" {
		t.Fatalf("inputs=%+v", steps.inputs)
	}
	if got := steps.inputs[1].Inbox; len(got) != 3 || got[0].ID != "carry" || got[1].ID != "incoming" || got[2].ID != "later" {
		t.Fatalf("carry=%+v", got)
	}
	if steps.views[1].Streams["s"].Name != "s" || steps.views[1].Seq != 3 {
		t.Fatalf("view=%+v", steps.views[1])
	}
}

type unknownStore struct {
	store.Store
	once    bool
	replays int
}

func (s *unknownStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	r, err := s.Store.Apply(ctx, o)
	if err == nil && !s.once {
		s.once = true
		return store.Receipt{}, store.ErrUnknown
	}
	return r, err
}
func (s *unknownStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	s.replays++
	return s.Store.Replay(ctx, id)
}
func TestTickUnknownPinsOpUntilReplay(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	s := &unknownStore{Store: base}
	guard := &tickGuard{}
	edges := &tickEdges{}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("one", store.PutStream{Name: "s"})}, Edges: []EdgeRequest{{ID: "one", After: []string{"one"}}}}}}
	e, err := NewEngine(ctx, s, guard, steps, edges)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); !errors.Is(err, store.ErrUnknown) {
		t.Fatalf("err=%v", err)
	}
	if len(edges.plans) != 0 || len(steps.inputs) != 1 {
		t.Fatal("unknown dispatched or replanned")
	}
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if s.replays != 1 || len(steps.inputs) != 1 || len(edges.plans) != 1 || len(edges.plans[0].Edges) != 1 {
		t.Fatalf("replay=%d steps=%d edges=%+v", s.replays, len(steps.inputs), edges.plans)
	}
	events, err := base.Events(ctx, 0, store.MaxEvents)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
}

type countStore struct {
	store.Store
	snapshots, applies int
}

func (s *countStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	s.snapshots++
	return s.Store.Snapshot(ctx)
}
func (s *countStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	s.applies++
	return s.Store.Apply(ctx, o)
}
func TestTickIdleNoHeartbeatAndLockLoss(t *testing.T) {
	ctx := context.Background()
	s := &countStore{Store: store.NewFake()}
	guard := &tickGuard{}
	steps := &tickSteps{}
	edges := &tickEdges{}
	e, err := NewEngine(ctx, s, guard, steps, edges)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = e.Tick(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if s.snapshots != 1 || s.applies != 0 {
		t.Fatalf("snapshots=%d applies=%d", s.snapshots, s.applies)
	}
	guard.err = errors.New("LOCK LOST")
	if err = e.Tick(ctx, nil); err == nil {
		t.Fatal("tick continued after lock loss")
	}
	if len(steps.inputs) != 3 || s.applies != 0 {
		t.Fatalf("steps=%d applies=%d", len(steps.inputs), s.applies)
	}
}

type gapStore struct{ store.Store }

func (s *gapStore) Events(context.Context, int64, int) ([]store.Event, error) {
	return []store.Event{{Seq: 2}}, nil
}
func TestTickRejectsEventGapWithoutMutation(t *testing.T) {
	ctx := context.Background()
	s := &gapStore{Store: store.NewFake()}
	steps := &tickSteps{}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err == nil || !strings.Contains(err.Error(), "sequence gap") {
		t.Fatalf("err=%v", err)
	}
	if e.state.Seq != 0 || len(steps.inputs) != 0 {
		t.Fatalf("seq=%d steps=%d", e.state.Seq, len(steps.inputs))
	}
}

type mutatingSteps struct{}

func (mutatingSteps) Tick(_ context.Context, s store.Snapshot, _ TickInput) (TickPlan, error) {
	c := s.Cards["p"]
	c.Needs[0] = "changed"
	s.Cards["p"] = c
	r := s.Rows["r"]
	r.State = "changed"
	s.Rows["r"] = r
	delete(s.Streams, "s")
	return TickPlan{}, nil
}
func TestTickPlannerCannotMutateRetainedSnapshot(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seed := op("seed", store.PutStream{Name: "s"}, store.PutCard{ID: "q", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s", Needs: []string{"q"}}, store.PutRow{Name: "r"})
	if r, err := f.Apply(ctx, seed); err != nil || !r.Accepted {
		t.Fatalf("seed=%+v err=%v", r, err)
	}
	e, err := NewEngine(ctx, f, &tickGuard{}, mutatingSteps{}, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if e.state.Cards["p"].Needs[0] != "q" || e.state.Rows["r"].State != "down" || e.state.Streams["s"].Name != "s" {
		t.Fatalf("state=%+v", e.state)
	}
}

type rejectingEdges struct{ calls int }

func (e *rejectingEdges) Committed(context.Context, TickPlan, []store.Receipt) error {
	e.calls++
	if e.calls == 1 {
		return errors.New("queue full")
	}
	return nil
}
func TestTickEdgeQueueFailureDoesNotPinPlanning(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	steps := &tickSteps{}
	edge := &rejectingEdges{}
	e, err := NewEngine(ctx, f, &tickGuard{}, steps, edge)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err == nil {
		t.Fatal("missing enqueue error")
	}
	if err = e.Tick(ctx, nil); err != nil || len(steps.inputs) != 2 {
		t.Fatalf("err=%v planning=%d", err, len(steps.inputs))
	}
}

type fullPageStore struct {
	store.Store
	calls int
}

func (s *fullPageStore) Events(_ context.Context, after int64, _ int) ([]store.Event, error) {
	s.calls++
	events := make([]store.Event, store.MaxEvents)
	for i := range events {
		events[i].Seq = after + int64(i) + 1
	}
	return events, nil
}
func TestTickBoundsEventCatchup(t *testing.T) {
	ctx := context.Background()
	s := &fullPageStore{Store: store.NewFake()}
	steps := &tickSteps{}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err == nil || !strings.Contains(err.Error(), "catch-up") {
		t.Fatalf("err=%v", err)
	}
	if s.calls != maxFoldPages || len(steps.inputs) != 0 || e.state.Seq != int64(maxFoldPages*store.MaxEvents) {
		t.Fatalf("calls=%d seq=%d steps=%d", s.calls, e.state.Seq, len(steps.inputs))
	}
}

type replayUnavailable struct {
	store.Store
	once bool
}

func (s *replayUnavailable) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	r, err := s.Store.Apply(ctx, o)
	if err == nil && !s.once {
		s.once = true
		return store.Receipt{}, store.ErrUnknown
	}
	return r, err
}
func (s *replayUnavailable) Replay(context.Context, string) (store.Receipt, bool, error) {
	return store.Receipt{}, false, errors.New("offline")
}
func TestTickFoldsWhileReplayUnavailable(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	s := &replayUnavailable{Store: base}
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{op("pending", store.PutStream{Name: "s"})}}}}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); !errors.Is(err, store.ErrUnknown) {
		t.Fatal(err)
	}
	if _, err = base.Apply(ctx, op("external", store.PutStream{Name: "external"})); err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); !errors.Is(err, store.ErrUnknown) {
		t.Fatal(err)
	}
	if e.state.Seq != 2 || e.state.Streams["external"].Name != "external" || len(steps.inputs) != 1 {
		t.Fatalf("state=%+v calls=%d", e.state, len(steps.inputs))
	}
}
