package verbs

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strings"
	"testing"
	"time"
)

func TestRemainingHoldRowDryRun(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed-hold", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "worker", Session: "fixture", Role: "worker"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, Preview: func(context.Context, string) error { return nil }}
	args := []string{"worker", "--reason", "pause"}
	r := e.hold(ctx, "hold", append(args, "--dry-run"))
	if r.Exit != 0 || !strings.Contains(r.Output, "withdraw=-") {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Rows["worker"].Held {
		t.Fatal(s.Rows)
	}
	r = e.hold(ctx, "hold", args)
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(ctx)
	if !s.Rows["worker"].Held {
		t.Fatal(s.Rows)
	}
	r = e.hold(ctx, "unhold", args)
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(ctx)
	if s.Rows["worker"].Held {
		t.Fatal(s.Rows)
	}
	r = e.hold(ctx, "hold", []string{"missing", "--reason", "pause"})
	if r.Output != "SPRINT REFUSED hold: unknown name missing: nova-sprint show\n" {
		t.Fatal(r)
	}
}

func TestRemainingMixedHoldKeepsDealtWork(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed-live", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutRow{Name: "worker", Session: "fixture", Role: "worker"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "brief"}}})
	applyPlan := func(id string, p card.Plan) {
		applyTest(t, f, store.Op{ID: id, Epoch: 1, Actor: "test", Kind: id, At: at, Expect: p.Expect, Writes: p.Writes})
	}
	s, _ := f.Snapshot(ctx)
	p, w := card.MoveRow(card.RowFromStore(s.Rows["worker"], 2, []string{"pro"}), card.Seen{})
	if w != nil {
		t.Fatal(w)
	}
	applyPlan("seen-live", p)
	s, _ = f.Snapshot(ctx)
	p, w = card.Move(card.FromStore(s.Cards["p"]), nil, nil, card.StreamFromStore(s.Streams["s"]), nil, card.Resolve{})
	if w != nil {
		t.Fatal(w)
	}
	applyPlan("resolve-live", p)
	s, _ = f.Snapshot(ctx)
	p, w = card.Move(card.FromStore(s.Cards["p"]), nil, map[string]card.Row{"worker": card.RowFromStore(s.Rows["worker"], 2, []string{"pro"})}, card.StreamFromStore(s.Streams["s"]), nil, card.Start{Row: "worker", Branch: "b", Base: strings.Repeat("a", 40)})
	if w != nil {
		t.Fatal(w)
	}
	applyPlan("start-live", p)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, Preview: func(context.Context, string) error { return nil }}
	r := e.hold(ctx, "hold", []string{"worker", "s", "--reason", "pause"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(ctx)
	if !s.Rows["worker"].Held || !s.Streams["s"].Held || s.Cards["p.w1"].State != "ready" || s.Cards["p"].State != "working" {
		t.Fatal(s)
	}
}

func TestRemainingHoldStreamParksReady(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed-stream-hold", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "brief"}}})
	s, _ := f.Snapshot(ctx)
	plan, why := card.Move(card.FromStore(s.Cards["p"]), nil, nil, card.StreamFromStore(s.Streams["s"]), nil, card.Resolve{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "resolve-stream-hold", Epoch: 1, Actor: "test", Kind: "resolve", At: at, Expect: plan.Expect, Writes: plan.Writes})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, Preview: func(context.Context, string) error { return nil }}
	r := e.hold(ctx, "hold", []string{"s", "--reason", "pause"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(ctx)
	if !s.Streams["s"].Held || s.Cards["p"].State != "waiting" {
		t.Fatal(s)
	}
	r = e.hold(ctx, "unhold", []string{"s", "--reason", "resume"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
}

func TestRemainingHoldPreviewBeforeApply(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "preview-seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "r", Session: "fixture", Role: "worker"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }}
	args := []string{"r", "--reason", "pause"}
	if got := e.hold(ctx, "hold", args); got.Exit != 2 {
		t.Fatal(got)
	}
	called := false
	e.Preview = func(ctx context.Context, text string) error {
		called = true
		s, _ := f.Snapshot(ctx)
		if s.Rows["r"].Held || !strings.Contains(text, "withdraw=-") {
			t.Fatalf("preview after mutation or incomplete: %s", text)
		}
		return fmt.Errorf("output failed")
	}
	if got := e.hold(ctx, "hold", args); got.Exit != 2 || !called {
		t.Fatal(got, called)
	}
	s, _ := f.Snapshot(ctx)
	if s.Rows["r"].Held {
		t.Fatal(s.Rows)
	}
	e.Preview = func(ctx context.Context, text string) error {
		s, _ := f.Snapshot(ctx)
		if s.Rows["r"].Held {
			t.Fatal("preview after apply")
		}
		return nil
	}
	if got := e.hold(ctx, "hold", args); got.Exit != 0 || strings.Contains(got.Output, "withdraw=") {
		t.Fatal(got)
	}
	s, _ = f.Snapshot(ctx)
	if !s.Rows["r"].Held {
		t.Fatal(s.Rows)
	}
}

func TestRemainingMixedStreamHoldAndWithdrawal(t *testing.T) {
	ctx := context.Background()
	f, at := releaseRowFixture(t)
	s, _ := f.Snapshot(ctx)
	hold, why := card.MoveStream(card.StreamFromStore(s.Streams["s"]), card.HoldStream{Reason: "pause", Seq: s.Seq})
	if why != nil {
		t.Fatal(why)
	}
	c := s.Cards["p.w1"]
	p := s.Cards["p"]
	withdrawal, why := card.MoveConsumer(card.FromStore(c), card.FromStore(p), card.RowFromStore(s.Rows["w"], 2, []string{"pro"}), card.Withdraw{Reason: "pause", Stream: card.StreamFromStore(s.Streams["s"])})
	if why != nil {
		t.Fatal(why)
	}
	merged, why := card.Merge(hold, withdrawal)
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "mixed-hold-withdraw", Epoch: s.Epoch, Actor: "test", Kind: "test", At: at, Expect: merged.Expect, Writes: merged.Writes})
	s, _ = f.Snapshot(ctx)
	if !s.Streams["s"].Held || s.Cards["p"].State != "waiting" || s.Cards["p.w1"].State != "fail" {
		t.Fatal(s)
	}
}
