package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestPriorityOnFake(t *testing.T) {
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "brief"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "worker", Now: func() time.Time { return at }}
	wireFixture(t, &e, f, "worker")
	r := e.Execute(context.Background(), []string{"priority", "p", "high", "--reason", "urgent"})
	if r.Exit != 0 || r.Output != "PRIORITY p high\nnext: nothing; wait for the bus\n" {
		t.Fatal(r)
	}
	snap, _ := f.Snapshot(context.Background())
	if snap.Cards["p"].Priority != "high" {
		t.Fatal(snap.Cards["p"])
	}
	bad := e.Execute(context.Background(), []string{"priority", "p", "wrong", "--reason", "urgent"})
	if bad.Exit != 1 || bad.Output != "SPRINT REFUSED priority: priority wrong is not one of low, normal, high, critical or blocker: nova-sprint show p\n" {
		t.Fatal(bad)
	}
}
