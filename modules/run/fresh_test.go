package run

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// started is a fresh store after the coordinator's first start: a fresh
// store is stopped, never started (store.FreshStop), and moves nothing
// until then.
func started(t *testing.T) *store.Fake {
	t.Helper()
	f := store.NewFake()
	must(t, f, "start", store.SetStopped{Stopped: false, Reason: "test"})
	must(t, f, "policy", activePolicy()...)
	return f
}

// activePolicy is v1 stored and active: the run deals nothing without an
// active policy (defect 53).
func activePolicy() []store.Write {
	return []store.Write{store.PutPolicy{Kind: "script", Name: "test.star", Body: "# test"}, store.SetActivePolicy{Version: 1}}
}

// benchCatalog is a catalog naming a bench model for every tier (defect
// 57: a bench deal names its model).
func benchCatalog() policy.Catalog {
	c := policy.Catalog{Bench: map[string]string{}}
	for _, t := range store.Tiers {
		c.Bench[t] = "test/" + t
	}
	return c
}

// A fresh store is stopped, never started, and a run that comes up on it
// (up starts the run service at step 4, before the walk is done) ticks
// nothing: no card moves, no question to policy, no push. The first start
// lets the next tick deal (Run.tla NeverStartedMovesNothing). A hooked
// row's presence is not a move: it comes up while stopped
// (TestStoppedTickRaisesHookedRow).
func TestFreshStoreTicksNothingUntilStart(t *testing.T) {
	t.Parallel()
	f := store.NewFake()
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "alice", Session: "sess-1", Role: "worker"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	must(t, f, "policy", activePolicy()...)
	ro := roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: []string{"flash", "pro", "heavy", "frontier"}}}}
	p := &answering{}
	tk, err := NewTicker(context.Background(), f, &healthy{}, p, t0)
	if err != nil {
		t.Fatal(err)
	}
	if s := tk.Snapshot(); !s.Stopped || s.StopReason != store.NeverStarted {
		t.Fatalf("a fresh store is stopped %v (%q), want stopped (%q)", s.Stopped, s.StopReason, store.NeverStarted)
	}
	before := tk.Snapshot().Seq
	tickN(t, tk, ro, 3)
	b := &fakeBus{}
	tk.Push(context.Background(), b, t0)
	if moved := cardsMoved(t, f, before); moved != 0 || p.asks != 0 || len(b.sent) != 0 {
		t.Fatalf("a never-started sprint moved: %d card events, %d asks, %d sends", moved, p.asks, len(b.sent))
	}
	must(t, f, "start", store.SetStopped{Stopped: false, Reason: "go"})
	tickN(t, tk, ro, 3)
	dealt := 0
	for _, c := range tk.Snapshot().Cards {
		if c.Kind == "work" && c.Holder == "alice" {
			dealt++
		}
	}
	if dealt != 1 || p.asks == 0 {
		t.Fatalf("after the start: %d dealt to alice, %d asks", dealt, p.asks)
	}
}

// cardsMoved counts the events after seq that wrote a card or a judgement.
func cardsMoved(t *testing.T, f *store.Fake, seq int64) int {
	t.Helper()
	events, err := f.Events(context.Background(), seq, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.After != nil && len(e.After.Cards)+len(e.After.Judgements) > 0 {
			n++
		}
	}
	return n
}

// up waits at steps 7-8 for every worker bench hooked and up in the store,
// on a fresh store that is stopped until the coordinator starts it after
// up: a row present on the bus comes up while stopped (presence deals
// nothing), and nothing else moves.
func TestStoppedTickRaisesHookedRow(t *testing.T) {
	t.Parallel()
	f := store.NewFake()
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "alice", Session: "sess-1", Role: "worker"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	must(t, f, "policy", activePolicy()...)
	ro := roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: []string{"flash", "pro", "heavy", "frontier"}}}}
	p := &answering{}
	tk, err := NewTicker(context.Background(), f, &healthy{}, p, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !tk.Snapshot().Stopped || tk.Snapshot().Rows["alice"].State != "down" {
		t.Fatalf("want a stopped store with alice down: %+v", tk.Snapshot())
	}
	before := tk.Snapshot().Seq
	tickN(t, tk, ro, 2)
	if got := tk.Snapshot().Rows["alice"].State; got != "up" {
		t.Fatalf("alice hooked on a stopped sprint is %q, want up", got)
	}
	if moved := cardsMoved(t, f, before); moved != 0 || p.asks != 0 || !tk.Snapshot().Stopped {
		t.Fatalf("the raise moved more than the row: %d card events, %d asks, stopped %v", moved, p.asks, tk.Snapshot().Stopped)
	}
}
