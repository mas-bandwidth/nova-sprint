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

func releaseRowFixture(t *testing.T) (*store.Fake, time.Time) {
	t.Helper()
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "batch-seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutRow{Name: "w", Kind: "worker", Width: 2, Tiers: []string{"pro"}}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "brief"}, store.PutCard{ID: "q", Kind: "producer", Stream: "s", Tier: "pro", Brief: "brief"}}})
	applyPlan := func(id string, p card.Plan) {
		applyTest(t, f, store.Op{ID: id, Epoch: 1, Actor: "test", Kind: id, At: at, Expect: p.Expect, Writes: p.Writes})
	}
	s, _ := f.Snapshot(ctx)
	p, why := card.MoveRow(card.RowFromStore(s.Rows["w"]), card.Seen{})
	if why != nil {
		t.Fatal(why)
	}
	applyPlan("batch-seen", p)
	s, _ = f.Snapshot(ctx)
	p, why = card.Move(card.FromStore(s.Cards["p"]), nil, nil, card.StreamFromStore(s.Streams["s"]), nil, card.Resolve{})
	if why != nil {
		t.Fatal(why)
	}
	applyPlan("batch-resolve", p)
	s, _ = f.Snapshot(ctx)
	p, why = card.Move(card.FromStore(s.Cards["p"]), nil, map[string]card.Row{"w": card.RowFromStore(s.Rows["w"])}, card.StreamFromStore(s.Streams["s"]), nil, card.Start{Row: "w", Branch: "b", Base: strings.Repeat("a", 40)})
	if why != nil {
		t.Fatal(why)
	}
	applyPlan("batch-start", p)
	s, _ = f.Snapshot(ctx)
	p, why = card.Move(card.FromStore(s.Cards["q"]), nil, nil, card.StreamFromStore(s.Streams["s"]), nil, card.Resolve{})
	if why != nil {
		t.Fatal(why)
	}
	applyPlan("batch-resolve-q", p)
	s, _ = f.Snapshot(ctx)
	p, why = card.Move(card.FromStore(s.Cards["q"]), nil, map[string]card.Row{"w": card.RowFromStore(s.Rows["w"])}, card.StreamFromStore(s.Streams["s"]), nil, card.Start{Row: "w", Branch: "b", Base: strings.Repeat("a", 40)})
	if why != nil {
		t.Fatal(why)
	}
	applyPlan("batch-start-q", p)

	return f, at
}
func TestRemainingReleaseRow(t *testing.T) {
	ctx := context.Background()
	f, at := releaseRowFixture(t)

	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }}
	args := []string{"--row", "w", "--all", "--reason", "pause"}
	e.Preview = func(context.Context, string) error { return fmt.Errorf("failed") }
	if r := e.releaseRow(ctx, args); r.Exit != 2 {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Cards["p.w1"].State != "ready" {
		t.Fatal(s.Cards)
	}
	e.Preview = func(ctx context.Context, text string) error {
		s, _ := f.Snapshot(ctx)
		if s.Cards["p.w1"].State != "ready" || !strings.Contains(text, "p.w1@0") || !strings.Contains(text, "--epoch 1") {
			t.Fatal(text)
		}
		return nil
	}
	if r := e.releaseRow(ctx, args); r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(ctx)
	if s.Cards["p.w1"].State != "fail" || s.Cards["p"].State != "ready" || s.Cards["q.w1"].State != "fail" {
		t.Fatal(s.Cards)
	}
	events, _ := f.Events(ctx, 0, 100)
	count := 0
	for _, event := range events {
		if event.Kind == "release row" {
			count++
		}
	}
	if count != 2 {
		t.Fatal(count)
	}

}

func TestRemainingReleaseRowReportsPartialProgress(t *testing.T) {
	ctx := context.Background()
	f, at := releaseRowFixture(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }}
	e.Preview = func(context.Context, string) error {
		applyTest(t, f, store.Op{ID: "concurrent-change", Epoch: 1, Actor: "test", Kind: "change", At: at, Writes: []store.Write{store.SetCardReason{Card: "q.w1", Reason: "changed"}}})
		return nil
	}
	r := e.releaseRow(ctx, []string{"--row", "w", "--all", "--reason", "pause"})
	if r.Exit != 2 || !strings.Contains(r.Output, "released 1 of 2") || !strings.Contains(r.Output, "operation failed") {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Cards["p.w1"].State != "fail" || s.Cards["q.w1"].State != "ready" {
		t.Fatal(s.Cards)
	}
}
