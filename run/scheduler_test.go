package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type fakeClock struct {
	at       time.Time
	ticks    chan time.Time
	interval time.Duration
}

func (c *fakeClock) Now() time.Time               { return c.at }
func (c *fakeClock) Every(d time.Duration) Ticker { c.interval = d; return fakeTicker{c.ticks} }

type fakeTicker struct{ ticks chan time.Time }

func (t fakeTicker) C() <-chan time.Time { return t.ticks }
func (t fakeTicker) Stop()               {}

type fakePresence struct {
	calls int
	err   error
}

func (p *fakePresence) Who(context.Context) ([]string, error) {
	p.calls++
	return []string{"alice", "sprint"}, p.err
}

type fakePublisher struct{ snapshots []store.Snapshot }

func (p *fakePublisher) Offer(s store.Snapshot, _ time.Time) { p.snapshots = append(p.snapshots, s) }

type fakeLogger struct{ ticks []TickMetrics }

func (l *fakeLogger) Log(m TickMetrics) { l.ticks = append(l.ticks, m) }

type fakeDeadline struct{}

func (fakeDeadline) Begin(ctx context.Context) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}

type fakeResults struct{}

func (fakeResults) Drain() []EdgeResult { return nil }

type policyStore struct {
	store.Store
	reads int
}

func (p *policyStore) Policy(ctx context.Context, v int) (store.PolicyVersion, error) {
	p.reads++
	return p.Store.Policy(ctx, v)
}

type observeSteps struct {
	inputs []TickInput
	views  []store.Snapshot
}

func (s *observeSteps) Tick(_ context.Context, view store.Snapshot, in TickInput) (TickPlan, error) {
	s.views = append(s.views, view)
	copyIn := in
	if in.Policy != nil {
		value := *in.Policy
		copyIn.Policy = &value
	}
	s.inputs = append(s.inputs, copyIn)
	if in.Policy != nil {
		in.Policy.Body = "mutated"
	}
	return TickPlan{}, nil
}
func TestSchedulerInputsAndPolicyIsolation(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	r, err := base.Apply(ctx, op("policy", store.PutPolicy{Kind: "script", Name: "p", Body: "original"}))
	if err != nil || !r.Accepted {
		t.Fatalf("policy %v %+v", err, r)
	}
	r, err = base.Apply(ctx, op("row", store.PutRow{Name: "alice"}))
	if err != nil || !r.Accepted {
		t.Fatalf("row %v %+v", err, r)
	}
	r, err = base.Apply(ctx, op("activate", store.SetActivePolicy{Version: 1}))
	if err != nil || !r.Accepted {
		t.Fatalf("activate %v %+v", err, r)
	}
	storeView := &policyStore{Store: base}
	steps := &observeSteps{}
	engine, err := NewEngine(ctx, storeView, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{at: time.Unix(1000, 0)}
	presence := &fakePresence{}
	pub := &fakePublisher{}
	log := &fakeLogger{}
	sched, err := NewScheduler(engine, clock, presence, pub, log, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if i == 1 {
			applyOK(t, base, op("row-up", store.SetRowState{Row: "alice", State: "up"}))
		}
		if err = sched.Step(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if steps.views[0].Rows["alice"].State != "down" || steps.views[1].Rows["alice"].State != "up" {
		t.Fatalf("folded row states: %s, %s", steps.views[0].Rows["alice"].State, steps.views[1].Rows["alice"].State)
	}
	if presence.calls != 3 || storeView.reads != 1 {
		t.Fatalf("presence=%d policy=%d", presence.calls, storeView.reads)
	}
	for _, in := range steps.inputs {
		if !in.At.Equal(clock.at) || len(in.Presence) != 2 || in.Policy.Body != "original" {
			t.Fatalf("input=%+v", in)
		}
	}
	if len(pub.snapshots) != 3 || len(log.ticks) != 3 || log.ticks[2].Number != 3 {
		t.Fatalf("publish=%d logs=%+v", len(pub.snapshots), log.ticks)
	}
}
func TestSchedulerRetriesFailedPresenceAndKeepsResults(t *testing.T) {
	ctx := context.Background()
	steps := &tickSteps{}
	engine, err := NewEngine(ctx, store.NewFake(), &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{at: time.Unix(1000, 0)}
	presence := &fakePresence{err: errors.New("offline")}
	sched, err := NewScheduler(engine, clock, presence, &fakePublisher{}, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if err = sched.Step(ctx, []EdgeResult{{ID: "edge", Epoch: 1}}); err == nil {
		t.Fatal("expected presence error")
	}
	presence.err = nil
	if err = sched.Step(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if presence.calls != 2 || len(steps.inputs) != 1 || len(steps.inputs[0].Inbox) != 1 || steps.inputs[0].Inbox[0].ID != "edge" {
		t.Fatalf("calls=%d input=%+v", presence.calls, steps.inputs)
	}
}
func TestSchedulerStopsOnObservedLockLoss(t *testing.T) {
	ctx := context.Background()
	guard := &tickGuard{}
	engine, err := NewEngine(ctx, store.NewFake(), guard, &tickSteps{}, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{at: time.Unix(1000, 0), ticks: make(chan time.Time, 1)}
	clock.ticks <- clock.at
	guard.err = ErrLockLost
	sched, err := NewScheduler(engine, clock, &fakePresence{}, &fakePublisher{}, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if err = sched.Run(ctx); !errors.Is(err, ErrLockLost) || clock.interval != time.Second {
		t.Fatalf("err=%v interval=%s", err, clock.interval)
	}
}

func TestSchedulerChecksLockBeforePresence(t *testing.T) {
	ctx := context.Background()
	guard := &tickGuard{}
	engine, err := NewEngine(ctx, store.NewFake(), guard, &tickSteps{}, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	guard.err = ErrLockLost
	presence := &fakePresence{err: errors.New("offline")}
	pub := &fakePublisher{}
	sched, err := NewScheduler(engine, &fakeClock{at: time.Unix(1000, 0)}, presence, pub, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if err = sched.Step(ctx, nil); !errors.Is(err, ErrLockLost) || presence.calls != 0 || len(pub.snapshots) != 0 {
		t.Fatalf("err=%v who=%d publish=%d", err, presence.calls, len(pub.snapshots))
	}
}

type policyRaceStore struct {
	store.Store
	changed bool
}

func (s *policyRaceStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	if o.ID == "planned" && !s.changed {
		s.changed = true
		_, err := s.Store.Apply(ctx, op("switch", store.SetActivePolicy{Version: 2}))
		if err != nil {
			return store.Receipt{}, err
		}
	}
	return s.Store.Apply(ctx, o)
}
func TestPlannedAndCommittedPolicyAreDistinct(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	applyOK(t, base, op("p1", store.PutPolicy{Kind: "script", Name: "one", Body: "one"}))
	applyOK(t, base, op("p2", store.PutPolicy{Kind: "script", Name: "two", Body: "two"}))
	applyOK(t, base, op("activate", store.SetActivePolicy{Version: 1}))
	source := &policyRaceStore{Store: base}
	planned := op("planned", store.PutStream{Name: "s"})
	planned.Note = `[{"kind":"work","cards":["p"]}]`
	steps := &tickSteps{plans: []TickPlan{{Ops: []store.Op{planned}}}}
	engine, err := NewEngine(ctx, source, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	events, err := base.Events(ctx, 0, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Policy != 2 || last.Receipt.Note != planned.Note || engine.plannedPolicy() != 1 {
		t.Fatalf("event=%+v planned=%d", last, engine.plannedPolicy())
	}
}

type absentSprint struct{}

func (absentSprint) Who(context.Context) ([]string, error) { return []string{"alice"}, nil }
func TestSchedulerStopsIfSprintHookNotPresent(t *testing.T) {
	ctx := context.Background()
	steps := &tickSteps{}
	engine, err := NewEngine(ctx, store.NewFake(), &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	sched, err := NewScheduler(engine, &fakeClock{at: time.Unix(1000, 0)}, absentSprint{}, &fakePublisher{}, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if err = sched.Step(ctx, nil); !errors.Is(err, ErrLockLost) || len(steps.inputs) != 0 {
		t.Fatalf("err=%v planned=%d", err, len(steps.inputs))
	}
}
