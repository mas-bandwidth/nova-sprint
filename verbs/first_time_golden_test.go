package verbs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Deal planning is faked here; real push, deal and landing remain L3 gates.
func TestConstructibleFirstTimeGolden(t *testing.T) {
	ctx := context.Background()
	var out strings.Builder
	cold := Engine{Env: fakeEnv{detail: "nova-fleet check reports tailscale, redis, postgres missing"}}
	goldenCommand(&out, "nova-sprint", cold.Execute(ctx, nil))
	cold.Env = fakeEnv{green: true}
	cold.Actor = "rowan"
	goldenCommand(&out, "nova-sprint", cold.Execute(ctx, nil))
	goldenSetup(t, &out)
	goldenCards(t, &out)
	want, err := os.ReadFile("testdata/first-time-constructible.golden")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("first-time transcript differs:\n%s", out.String())
	}
}
func goldenCommand(out *strings.Builder, command string, r Result) {
	fmt.Fprintf(out, "$ %s\n%s", command, r.Output)
}
func goldenSetup(t *testing.T, out *strings.Builder) {
	db := store.NewFake()
	choices := Choices{Store: db, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }}
	cmd := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		switch name {
		case "nova-version":
			return []byte("nova-version v2.0.0 linux/amd64"), nil
		case "nova-bus":
			return []byte("WHO OK present=0\n"), nil
		}
		return nil, nil
	}
	runtime := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: choices, Command: cmd, Getenv: func(string) string { return "" }}, Pin: "v2.0.0"}, Store: &runtimeStore{ready: true}}
	r := (&Engine{Actor: "rowan", Env: runtime.Prerequisites.Env, Setup: runtime, SetupLock: setupLeaseFake{}}).Execute(context.Background(), []string{"up", "--as", "rowan"})
	if r.Exit != 1 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint up --as rowan", r)
}
func goldenCards(t *testing.T, out *strings.Builder) {
	ctx := context.Background()
	at := time.Unix(1000, 0)
	f := store.NewFake()
	applyTest(t, f, store.Op{ID: "rows", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutRow{Name: "worker", Session: "fixture", Role: "worker"}, store.PutRow{Name: "reader", Session: "fixture", Role: "worker"}}})
	applyTest(t, f, store.Op{ID: "up", Epoch: 1, Actor: "test", Kind: "up", At: at, Writes: []store.Write{store.SetRowState{Row: "worker", State: "up"}, store.SetRowState{Row: "reader", State: "up"}}})
	e := Engine{Store: f, Env: fakeEnv{green: true, schema: true, hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, Repositories: repoFake{}, Briefs: briefsFake{"card": {ID: "fix-typos", Kind: "producer", Tier: "pro", Brief: "Fix the typos."}}, Reports: CardReportPlanner{}, Heads: headFake{}}
	wireFixture(t, &e, f, "rowan", "worker", "reader")
	r := e.Execute(ctx, []string{"stream", "add", "docs", "--repo", "owner/repo"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint stream add docs --repo owner/repo", r)
	r = e.Execute(ctx, []string{"add", "--stream", "docs", "--brief-file", "card"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint add --stream docs --brief-file card", r)
	goldenDeal(t, f, at, "worker", card.Resolve{}, card.Start{Row: "worker", Branch: "sprint/docs/fix-typos", Base: strings.Repeat("a", 40)})
	e.Actor = "worker"
	r = e.Execute(ctx, []string{"take", "fix-typos.w1@0", "--as", "worker", "--epoch", "1"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint take fix-typos.w1@0 --as worker --epoch 1", r)
	head := strings.Repeat("b", 40)
	r = e.Execute(ctx, []string{"finish", "fix-typos.w1@1", "--head", head, "--as", "worker", "--epoch", "1"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint finish fix-typos.w1@1 --head "+head+" --as worker --epoch 1", r)
	goldenRead(t, f, at, &e, out)
}

func goldenDeal(t *testing.T, f *store.Fake, at time.Time, row string, events ...card.Event) {
	for i, event := range events {
		snap, _ := f.Snapshot(context.Background())
		p := snap.Cards["fix-typos"]
		plan, why := card.Move(card.FromStore(p), consumers(snap, p.ID), map[string]card.Row{row: card.RowFromStore(snap.Rows[row], 2, []string{"pro"})}, card.StreamFromStore(snap.Streams[p.Stream]), nil, event)
		if why != nil {
			t.Fatal(why)
		}
		applyTest(t, f, store.Op{ID: fmt.Sprintf("deal-%s-%d", row, i), Epoch: snap.Epoch, Actor: "test", Kind: "deal", At: at, Expect: plan.Expect, Writes: plan.Writes})
	}
}
func goldenRead(t *testing.T, f *store.Fake, at time.Time, e *Engine, out *strings.Builder) {
	goldenDeal(t, f, at, "reader", card.AskRead{Row: "reader"})
	e.Actor = "reader"
	ctx := context.Background()
	r := e.Execute(ctx, []string{"take", "fix-typos.r1.1@0", "--as", "reader", "--epoch", "1"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint take fix-typos.r1.1@0 --as reader --epoch 1", r)
	r = e.Execute(ctx, []string{"read", "fix-typos.r1.1@1", "yes", "--as", "reader", "--epoch", "1"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	goldenCommand(out, "nova-sprint read fix-typos.r1.1@1 yes --as reader --epoch 1", r)
}
