package verbs

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"testing"
	"time"
)

func TestRemainingEdit(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "seed-edit", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "original", Prose: "old"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, Briefs: briefsFake{"new": card.Add{Prose: "new", Paths: []string{"a"}, DoneWhen: []string{"done"}}}}
	r := e.edit(ctx, []string{"p", "--brief-file", "new", "--reason", "update"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Cards["p"].Prose != "new" || s.Cards["p"].Brief != "original" {
		t.Fatal(s.Cards["p"])
	}
	r = e.edit(ctx, []string{"missing", "--brief-file", "new", "--reason", "update"})
	if r.Output != "SPRINT REFUSED edit: unknown card missing: nova-sprint show missing\n" {
		t.Fatal(r)
	}
}
