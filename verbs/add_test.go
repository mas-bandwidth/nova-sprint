package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type briefsFake map[string]card.Add

func (f briefsFake) Parse(_ context.Context, path string, _ store.Stream) (card.Add, error) {
	return f[path], nil
}
func TestAddBatchIsAtomicAndOrdered(t *testing.T) {
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "stream", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}}})
	b := briefsFake{"a": {ID: "a", Kind: "producer", Tier: "pro", Brief: "a", Needs: []string{"b"}}, "b": {ID: "b", Kind: "producer", Tier: "pro", Brief: "b"}}
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Briefs: b, Actor: "rowan", Now: func() time.Time { return at }}
	wireFixture(t, &e, f, "rowan")
	args := []string{"add", "--stream", "s", "--brief-file", "a", "--brief-file", "b"}
	dry := e.Execute(context.Background(), append(append([]string{}, args...), "--dry-run"))
	if dry.Exit != 0 {
		t.Fatal(dry)
	}
	s, _ := f.Snapshot(context.Background())
	if len(s.Cards) != 0 {
		t.Fatal(s.Cards)
	}
	r := e.Execute(context.Background(), args)
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(context.Background())
	if len(s.Cards) != 2 || len(s.Cards["a"].Needs) != 1 {
		t.Fatal(s.Cards)
	}
}
func TestAddCycleRefuses(t *testing.T) {
	adds := []card.Add{{ID: "a", Kind: "producer", Needs: []string{"b"}}, {ID: "b", Kind: "producer", Needs: []string{"a"}}}
	_, why := planAdds(store.Snapshot{}, adds)
	if why == "" {
		t.Fatal("cycle accepted")
	}
}

func TestStandaloneSentinel(t *testing.T) {
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "brief"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }}
	wireFixture(t, &e, f, "rowan")
	noNeeds := e.Execute(context.Background(), []string{"add", "--stream", "s", "--sentinel", "done"})
	if noNeeds.Exit != 1 {
		t.Fatal(noNeeds)
	}
	mixed := e.Execute(context.Background(), []string{"add", "--stream", "s", "--sentinel", "done", "--needs", "p", "--brief-file", "x"})
	if mixed.Exit != 1 {
		t.Fatal(mixed)
	}
	r := e.Execute(context.Background(), []string{"add", "--stream", "s", "--sentinel", "done", "--needs", "p"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(context.Background())
	if s.Cards["done"].Kind != "sentinel" || len(s.Cards["done"].Needs) != 1 {
		t.Fatal(s.Cards)
	}
}

func TestAddRejectsReservedGroupBranch(t *testing.T) {
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "stream", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Briefs: briefsFake{"x": {ID: "group", Kind: "producer", Tier: "pro", Brief: "x"}}, Actor: "rowan", Now: func() time.Time { return at }}
	wireFixture(t, &e, f, "rowan")
	got := e.Execute(context.Background(), []string{"add", "--stream", "s", "--brief-file", "x"})
	if got.Exit != 1 || got.Output != "SPRINT REFUSED add: card group is reserved for the stream group branch: choose another card id\n" {
		t.Fatal(got)
	}
}
