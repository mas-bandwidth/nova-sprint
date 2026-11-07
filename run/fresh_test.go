package run

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
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

// activePolicy is policy version 1, stored and active: the run deals nothing without an
// active policy.
func activePolicy() []store.Write {
	return []store.Write{store.PutPolicy{Kind: "script", Name: "test.star", Body: "# test"}, store.SetActivePolicy{Version: 1}}
}

// benchCatalog is a catalog naming a bench model for every tier: a bench
// deal names its model.
func benchCatalog() policy.Catalog {
	c := policy.Catalog{Bench: map[string]string{}}
	for _, t := range store.Tiers {
		c.Bench[t] = "test/" + t
	}
	return c
}

// A fresh store is stopped, never started, and a run that comes up on it
// (up starts the run service at step 4, before the walk is done) ticks
// nothing: no op, no question to policy, no push. The first start lets the
// next tick deal (Run.tla NeverStartedMovesNothing).
func TestFreshStoreTicksNothingUntilStart(t *testing.T) {
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
	if tk.Snapshot().Seq != before || p.asks != 0 || len(b.sent) != 0 {
		t.Fatalf("a never-started sprint moved: seq %d -> %d, %d asks, %d sends", before, tk.Snapshot().Seq, p.asks, len(b.sent))
	}
	if tk.Snapshot().Rows["alice"].State != "down" {
		t.Fatalf("a never-started sprint raised alice: %+v", tk.Snapshot().Rows["alice"])
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
