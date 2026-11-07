package verbs

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strings"
	"testing"
	"time"
)

func dropFixture(t *testing.T) (*store.Fake, *Engine) {
	t.Helper()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "drop-seed", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutCard{ID: "a", Kind: "producer", Stream: "s", Tier: "pro", Brief: "a"}, store.PutCard{ID: "b", Kind: "producer", Stream: "s", Tier: "pro", Brief: "b", Needs: []string{"a"}}, store.PutCard{ID: "c", Kind: "producer", Stream: "s", Tier: "pro", Brief: "c", Needs: []string{"b"}}, store.PutCard{ID: "x", Kind: "producer", Stream: "s", Tier: "pro", Brief: "x"}}})
	e := &Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, Preview: func(context.Context, string) error { return nil }}
	return f, e
}
func TestRemainingDropNeedsChoiceAndExpect(t *testing.T) {
	f, e := dropFixture(t)
	ctx := context.Background()
	args := []string{"a", "--reason", "remove"}
	dry := e.drop(ctx, append(append([]string{}, args...), "--dry-run"))
	if dry.Exit != 0 || !strings.Contains(dry.Output, "touch=a,b") || strings.Contains(dry.Output, "a,b,c") {
		t.Fatal(dry)
	}
	r := e.drop(ctx, args)
	if r.Exit != 1 || !strings.Contains(r.Output, "dependants b") {
		t.Fatal(r)
	}
	r = e.drop(ctx, []string{"a", "--reason", "remove", "--waive", "--expect", "c"})
	if r.Output != "SPRINT REFUSED drop: dependants changed: nova-sprint drop a --reason <why> --dry-run\n" {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Cards["a"].State != "waiting" {
		t.Fatal(s.Cards)
	}
	e.Preview = func(ctx context.Context, text string) error {
		s, _ := f.Snapshot(ctx)
		if s.Cards["a"].State != "waiting" || !strings.Contains(text, "touch=a,b") {
			t.Fatal(text, s.Cards)
		}
		return nil
	}
	r = e.drop(ctx, []string{"a", "--reason", "remove", "--waive", "--expect", "b"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(ctx)
	if s.Cards["a"].State != "fail" || len(s.Cards["b"].Needs) != 0 || s.Cards["c"].State != "waiting" {
		t.Fatal(s.Cards)
	}
}
func TestRemainingDropRelink(t *testing.T) {
	f, e := dropFixture(t)
	r := e.drop(context.Background(), []string{"a", "--reason", "remove", "--relink", "x"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(context.Background())
	if s.Cards["a"].State != "fail" || s.Cards["b"].State != "waiting" || len(s.Cards["b"].Needs) != 1 || s.Cards["b"].Needs[0] != "x" || s.Cards["c"].State != "waiting" {
		t.Fatal(s.Cards)
	}
}

func TestRemainingDropRefusesOpenJudgementAndPreviewFailure(t *testing.T) {
	ctx := context.Background()
	f, e := dropFixture(t)
	at := time.Unix(1000, 0)
	e.Preview = func(context.Context, string) error { return fmt.Errorf("output failed") }
	r := e.drop(ctx, []string{"a", "--reason", "remove", "--waive"})
	if r.Exit != 2 {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Cards["a"].State != "waiting" {
		t.Fatal(s.Cards)
	}
	applyTest(t, f, store.Op{ID: "drop-judgement", Epoch: 1, Actor: "test", Kind: "seed", At: at, Writes: []store.Write{store.PutJudgement{ID: "j", Kind: "blocked", About: "b"}}})
	r = e.drop(ctx, []string{"a", "--reason", "remove", "--waive"})
	if r.Output != "SPRINT REFUSED drop: card b has open judgement j: nova-sprint judge j drop --reason <why>\n" {
		t.Fatal(r)
	}
}
