package verbs

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strings"
	"testing"
	"time"
)

func TestParseCost(t *testing.T) {
	for _, tc := range []struct {
		in    string
		want  int64
		valid bool
	}{{"0.04", 40000, true}, {"1", 1000000, true}, {"0.000001", 1, true}, {"-1", 0, false}, {"1.0000001", 0, false}, {"abc", 0, false}, {"1.-1", 0, false}, {"1.+1", 0, false}, {" 1", 0, false}, {"1e2", 0, false}, {"9223372036855", 0, false}} {
		got, err := parseCost(tc.in)
		if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
			t.Fatalf("%q got %d %v", tc.in, got, err)
		}
	}
}
func TestParseReadAndEpoch(t *testing.T) {
	got, ok := parseReport("read", []string{"p.r1.1@2", "fix", "--finding", "broken", "--epoch", "3"}, "reader")
	if !ok || got.result != "fix" || got.reason != "broken" || got.epoch != 3 {
		t.Fatal(got, ok)
	}
}

type headFake struct{}

func (headFake) VerifyHead(context.Context, store.Card, string) (string, string, error) {
	return "", "", nil
}
func TestAtomicWorkReportAndReplay(t *testing.T) {
	f := store.NewFake()
	ctx := context.Background()
	at := time.Unix(1000, 0)
	_, err := f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: at})
	if err != nil {
		t.Fatal(err)
	}
	recreateWork(t, f, at)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "worker", Now: func() time.Time { return at }, Reports: CardReportPlanner{}, Heads: headFake{}}
	if r := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	head := strings.Repeat("b", 40)
	args := []string{"finish", "p.w1@1", "--head", head, "--epoch", "2"}
	first := e.Execute(ctx, args)
	again := e.Execute(ctx, args)
	if first.Exit != 0 || first != again {
		t.Fatal(first, again)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Cards["p.w1"].State != "done" || snap.Cards["p"].State != "review" || snap.Cards["p"].Head != head {
		t.Fatal(snap.Cards)
	}
}

func TestReplayShowsOriginalReportOutcome(t *testing.T) {
	r := store.Receipt{Actor: "worker", Kind: "report", Accepted: true, Seq: 4, After: &store.State{Cards: map[string]store.Card{"p.w1": {ID: "p.w1", Kind: "work", Result: "fail"}}}}
	got := reportReceipt(r, "worker", "finish", "p.w1")
	if got.Exit != 0 || !strings.HasPrefix(got.Output, "FAILED p.w1") {
		t.Fatal(got)
	}
}
