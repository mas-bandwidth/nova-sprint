package verbs

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/card"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type replayStore struct {
	receipt   store.Receipt
	snapshots int
}

func (s *replayStore) Snapshot(context.Context) (store.Snapshot, error) {
	s.snapshots++
	return store.Snapshot{}, nil
}
func (s *replayStore) Replay(context.Context, string) (store.Receipt, bool, error) {
	return s.receipt, true, nil
}
func (s *replayStore) Apply(context.Context, store.Op) (store.Receipt, error) {
	panic("replay must not apply")
}
func TestTakeReplayBeforeStateAndActorPrivacy(t *testing.T) {
	st := &replayStore{receipt: store.Receipt{Actor: "worker", Kind: "take", Accepted: true, Seq: 8}}
	e := Engine{Store: st, Env: fakeEnv{hooked: true}, Roles: testRoles{role: "coordinator"}, Actor: "worker", Now: func() time.Time { return time.Unix(1000, 0) }}
	got := e.Execute(context.Background(), []string{"take", "p.w1@0"})
	if got.Exit != 0 || st.snapshots != 1 {
		t.Fatalf("%+v snapshots=%d", got, st.snapshots)
	}
	got = e.Execute(context.Background(), []string{"take", "p.w1@0", "--as", "other"})
	if got.Exit != 1 || got.Output != "SPRINT REFUSED take: operation cannot be performed: nova-sprint next --as other\n" {
		t.Fatal(got)
	}
}

func TestTakeAndReleaseOnFake(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	seed := store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{
		store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1},
		store.PutRow{Name: "worker", Session: "fixture", Role: "worker"},
		store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "make it"},
	}}
	applyTest(t, f, seed)
	rowPlan, why := card.MoveRow(card.Row{Name: "worker", State: "down", Width: 2, Tiers: []string{"pro"}}, card.Seen{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "seen", Epoch: 1, Actor: "test", Kind: "seen", At: at, Expect: rowPlan.Expect, Writes: rowPlan.Writes})
	snap, _ := f.Snapshot(ctx)
	p := card.FromStore(snap.Cards["p"])
	stream := card.StreamFromStore(snap.Streams["s"])
	resolve, why := card.Move(p, nil, nil, stream, nil, card.Resolve{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "resolve", Epoch: 1, Actor: "test", Kind: "resolve", At: at, Expect: resolve.Expect, Writes: resolve.Writes})
	snap, _ = f.Snapshot(ctx)
	p = card.FromStore(snap.Cards["p"])
	row := card.RowFromStore(snap.Rows["worker"], 2, []string{"pro"})
	start, why := card.Move(p, nil, map[string]card.Row{"worker": row}, stream, nil, card.Start{Row: "worker", Branch: "sprint/p", Base: strings.Repeat("a", 40)})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "start", Epoch: 1, Actor: "test", Kind: "start", At: at, Expect: start.Expect, Writes: start.Writes})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "worker", Now: func() time.Time { return at }}
	wireFixture(t, &e, f, "worker")
	first := e.Execute(ctx, []string{"take", "p.w1@0"})
	again := e.Execute(ctx, []string{"take", "p.w1@0"})
	if first.Exit != 0 || first != again || !strings.Contains(first.Output, "make it") {
		t.Fatal(first, again)
	}
	released := e.Execute(ctx, []string{"release", "p.w1@1", "--reason", "unavailable"})
	if released.Exit != 0 {
		t.Fatal(released)
	}
	snap, _ = f.Snapshot(ctx)
	if snap.Cards["p"].State != "ready" || snap.Cards["p.w1"].State != "fail" {
		t.Fatal(snap.Cards)
	}
}

func applyTest(t *testing.T, f *store.Fake, op store.Op) {
	t.Helper()
	r, err := f.Apply(context.Background(), op)
	if err != nil || !r.Accepted {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestUnknownRetriesIdenticalOperation(t *testing.T) {
	for _, landed := range []bool{false, true} {
		t.Run(fmt.Sprint(landed), func(t *testing.T) {
			f := store.NewFake()
			f.Drop("unknown", landed)
			op := store.Op{ID: "unknown", Epoch: 1, Actor: "row", Kind: "seed", At: time.Unix(1000, 0), Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}}}
			r, err := applyExact(context.Background(), f, op)
			if err != nil || !r.Accepted {
				t.Fatalf("%+v %v", r, err)
			}
			events, err := f.Events(context.Background(), 0, 100)
			if err != nil || len(events) != 1 {
				t.Fatalf("%d %v", len(events), err)
			}
		})
	}
}

func TestOldEpochRefusesBeforeReplay(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: consumerOpID("take", 1, "p.w1", 0), Epoch: 1, Actor: "worker", Kind: "take", At: at})
	r, err := f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: at})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	recreateWork(t, f, at)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "worker", Now: func() time.Time { return at }}
	wireFixture(t, &e, f, "worker")
	got := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "1"})
	want := "SPRINT REFUSED take: epoch 1 is over: this card was recreated in epoch 2: run nova-sprint next\n"
	if got.Exit != 1 || got.Output != want {
		t.Fatalf("%+v", got)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Cards["p.w1"].State != "ready" {
		t.Fatal(snap.Cards["p.w1"])
	}
}

func recreateWork(t *testing.T, f *store.Fake, at time.Time) {
	ctx := context.Background()
	applyTest(t, f, store.Op{ID: "reseed", Epoch: 2, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutRow{Name: "worker", Session: "fixture", Role: "worker"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "make it"}}})
	snap, _ := f.Snapshot(ctx)
	rp, why := card.MoveRow(card.RowFromStore(snap.Rows["worker"], 2, []string{"pro"}), card.Seen{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "reseen", Epoch: 2, Actor: "test", Kind: "seen", At: at, Expect: rp.Expect, Writes: rp.Writes})
	snap, _ = f.Snapshot(ctx)
	stream := card.StreamFromStore(snap.Streams["s"])
	p := card.FromStore(snap.Cards["p"])
	plan, why := card.Move(p, nil, nil, stream, nil, card.Resolve{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "reresolve", Epoch: 2, Actor: "test", Kind: "resolve", At: at, Expect: plan.Expect, Writes: plan.Writes})
	snap, _ = f.Snapshot(ctx)
	p = card.FromStore(snap.Cards["p"])
	row := card.RowFromStore(snap.Rows["worker"], 2, []string{"pro"})
	plan, why = card.Move(p, nil, map[string]card.Row{"worker": row}, stream, nil, card.Start{Row: "worker", Branch: "sprint/p", Base: strings.Repeat("a", 40)})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "restart", Epoch: 2, Actor: "test", Kind: "start", At: at, Expect: plan.Expect, Writes: plan.Writes})
}
