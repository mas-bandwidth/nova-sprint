package verbs

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strings"
	"testing"
)

func TestRemainingShow(t *testing.T) {
	s := store.Snapshot{Epoch: 1, Cards: map[string]store.Card{"p": {ID: "p", Kind: "producer", Stream: "s"}}, Streams: map[string]store.Stream{"s": {Name: "s"}}}
	e := Engine{Store: fakeReader{s}, Env: fakeEnv{green: true, schema: true, hooked: true}, Actor: "rowan"}
	r := e.show(context.Background(), []string{"p"})
	if r.Exit != 0 || !strings.Contains(r.Output, `"id": "p"`) || !strings.Contains(r.Output, "next:") {
		t.Fatal(r)
	}
	r = e.show(context.Background(), []string{"missing"})
	if r.Output != "SPRINT REFUSED show: unknown name missing: nova-sprint show\n" {
		t.Fatal(r)
	}
	r = e.show(context.Background(), []string{"s", "--stitch"})
	if r.Exit != 2 {
		t.Fatal(r)
	}
}

type showRunFake struct{}

func (showRunFake) Inspect(context.Context) (RunStatus, error) {
	return RunStatus{Held: true, Host: "studio", PID: 42}, nil
}

type showEdgesFake struct{}

func (showEdgesFake) StitchRange(_ context.Context, s store.Stream, c []store.Card) (string, error) {
	return "base..head", nil
}
func (showEdgesFake) CI(_ context.Context, c store.Card) (string, error) {
	return "green at " + c.Head, nil
}
func TestRemainingShowRunStitchAndCI(t *testing.T) {
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Rows: map[string]store.Row{"writer": {Name: "writer"}}, Cards: map[string]store.Card{
		"p":      {ID: "p", Kind: "producer", Stream: "s", Author: "writer", Head: "abc"},
		"p.w1":   {ID: "p.w1", Kind: "work", Of: "p", Stream: "s", Cost: 10, Priced: true},
		"p.r1.1": {ID: "p.r1.1", Kind: "read", Of: "p", Stream: "s", Holder: "reader", Result: "fix", Reason: "broken gate", Cost: 20, Priced: true},
	}}
	e := Engine{Store: fakeReader{s}, Env: fakeEnv{green: true, schema: true, hooked: true}, Actor: "rowan", RunInfo: showRunFake{}, ShowEdges: showEdgesFake{}, Roster: fixtureRoster{}}
	cases := []struct {
		args  []string
		parts []string
	}{{[]string{"run"}, []string{"studio", "42"}}, {[]string{"s", "--stitch"}, []string{"base..head", "writer", "sol", "reader", "broken gate", "cost_micro_usd"}}, {[]string{"p", "--ci"}, []string{"green at abc"}}}
	for _, tc := range cases {
		r := e.show(context.Background(), tc.args)
		if r.Exit != 0 {
			t.Fatal(tc.args, r)
		}
		for _, part := range tc.parts {
			if !strings.Contains(r.Output, part) {
				t.Fatal(tc.args, r)
			}
		}
	}
}
