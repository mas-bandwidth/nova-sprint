package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type fakeEnv struct {
	green, schema, hooked bool
	detail                string
}

func (f fakeEnv) Fleet(context.Context) (bool, string, error) { return f.green, f.detail, nil }
func (f fakeEnv) Schema(context.Context) (bool, error)        { return f.schema, nil }
func (f fakeEnv) Hooked(context.Context, string) (bool, string, error) {
	return f.hooked, "nova-bus hook --as rowan", nil
}
func (f fakeEnv) Coordinator(context.Context) (string, error) { return "rowan", nil }

type fakeReader struct{ s store.Snapshot }

func (f fakeReader) Snapshot(context.Context) (store.Snapshot, error) { return f.s, nil }

func TestNextSevenQuestions(t *testing.T) {
	base := store.Snapshot{Streams: map[string]store.Stream{"docs": {Name: "docs"}}, Cards: map[string]store.Card{}, Judgements: map[string]store.Judgement{}}
	cases := []struct {
		name string
		env  fakeEnv
		snap store.Snapshot
		want string
	}{
		{"fleet", fakeEnv{detail: "nova-fleet check reports tailscale, redis, postgres missing"}, base, "nova-fleet check reports tailscale, redis, postgres missing"},
		{"schema", fakeEnv{green: true}, base, "nova-sprint up --as rowan"},
		{"hook", fakeEnv{green: true, schema: true}, base, "nova-bus hook --as rowan"},
		{"held", fakeEnv{green: true, schema: true, hooked: true}, store.Snapshot{Cards: map[string]store.Card{"p.w1": {ID: "p.w1", Holder: "rowan", State: "working", Kind: "work", Gen: 1}}}, "next: nova-sprint finish p.w1@1 --head <sha> --as rowan"},
		{"judgement", fakeEnv{green: true, schema: true, hooked: true}, store.Snapshot{Judgements: map[string]store.Judgement{"j": {ID: "j", State: "open", OpenedAt: time.Unix(1000, 0), Command: "nova-sprint judge j retry"}}}, "next: nova-sprint judge j retry"},
		{"empty stream", fakeEnv{green: true, schema: true, hooked: true}, base, "next: nova-sprint add --stream docs --brief-file <file>"},
		{"wait", fakeEnv{green: true, schema: true, hooked: true}, store.Snapshot{Streams: base.Streams, Cards: map[string]store.Card{"p": {ID: "p", Stream: "docs", Kind: "producer", State: "ready"}}}, "next: nothing; wait for the bus"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.snap.Rows == nil {
				tc.snap.Rows = map[string]store.Row{}
			}
			tc.snap.Rows["rowan"] = store.Row{Name: "rowan", Role: "coordinator", Session: "fixture"}
			e := Engine{Store: fakeReader{tc.snap}, Env: tc.env, Roles: testRoles{role: "coordinator"}, Actor: "rowan"}
			got := e.Execute(context.Background(), nil)
			wantExit := 0
			if tc.name == "fleet" || tc.name == "schema" || tc.name == "hook" {
				wantExit = 1
			}
			if got.Exit != wantExit || !strings.Contains(got.Output, tc.want) {
				t.Fatalf("got %+v want %q", got, tc.want)
			}
		})
	}
}

func TestNextJSONAndInvalid(t *testing.T) {
	e := Engine{Store: fakeReader{}, Env: fakeEnv{detail: "missing"}, Actor: "worker"}
	r := e.Execute(context.Background(), []string{"next", "--as", "worker", "--json"})
	if r.Exit != 1 || !strings.Contains(r.Output, "the fleet is not up") {
		t.Fatalf("%+v", r)
	}
	r = e.Execute(context.Background(), []string{"next", "--as"})
	if r.Exit != 1 || r.Output != "SPRINT REFUSED next: invalid arguments: nova-sprint help next\n" {
		t.Fatalf("%+v", r)
	}
}

func TestNextJSONIncludesAllRowObligations(t *testing.T) {
	s := store.Snapshot{Cards: map[string]store.Card{
		"z":     {ID: "z", Holder: "worker", State: "ready", Kind: "work", Gen: 2},
		"a":     {ID: "a", Holder: "worker", State: "working", Kind: "read", Gen: 3},
		"other": {ID: "other", Holder: "another", State: "ready"},
	}}
	e := Engine{Store: fakeReader{s}, Env: fakeEnv{green: true, schema: true, hooked: true}, Roles: testRoles{role: "worker"}, Actor: "worker"}
	got := e.Execute(context.Background(), []string{"next", "--json"})
	if got.Exit != 0 || !strings.Contains(got.Output, `"card":"a"`) || !strings.Contains(got.Output, `"card":"z"`) || strings.Contains(got.Output, `"card":"other"`) || strings.Index(got.Output, `"card":"a"`) > strings.Index(got.Output, `"card":"z"`) {
		t.Fatal(got)
	}
}

func TestColdNextNeedsNoStore(t *testing.T) {
	e := Engine{Env: fakeEnv{detail: "nova-fleet check reports tailscale, redis, postgres missing"}, Actor: "rowan"}
	got := e.Execute(context.Background(), nil)
	if got.Exit != 1 || !strings.Contains(got.Output, "the fleet is not up") {
		t.Fatal(got)
	}
}

func TestNextPrefersWorkingAtWidth(t *testing.T) {
	s := store.Snapshot{Epoch: 1, Cards: map[string]store.Card{
		"a": {ID: "a", Holder: "worker", Kind: "work", State: "ready"},
		"z": {ID: "z", Holder: "worker", Kind: "work", State: "working", Gen: 1},
	}, Rows: map[string]store.Row{"worker": {Name: "worker"}}}
	e := Engine{Store: fakeReader{s}, Env: fakeEnv{green: true, schema: true, hooked: true}, Roles: testRoles{role: "worker"}, Actor: "worker"}
	got := e.Execute(context.Background(), []string{"next"})
	if got.Exit != 0 || !strings.Contains(got.Output, "next: nova-sprint finish z@1") || !strings.Contains(got.Output, "(1 more)") {
		t.Fatal(got)
	}
}

func TestNamedOperationalFailureIsOneLine(t *testing.T) {
	r := operationalFailure("check", &OperationError{Problem: "nova-fleet check failed\nextra detail", Fix: "nova-fleet up", Exit: 1})
	if r.Exit != 1 || strings.Count(r.Output, "\n") != 1 {
		t.Fatal(r)
	}
}

func TestColdNoActorProgress(t *testing.T) {
	e := Engine{Env: fakeEnv{detail: "nova-fleet check reports tailscale, redis, postgres missing"}}
	r := e.Execute(context.Background(), nil)
	want := "SPRINT REFUSED next: the fleet is not up: nova-fleet check reports tailscale, redis, postgres missing\n"
	if r.Exit != 1 || r.Output != want {
		t.Fatal(r)
	}
	e.Env = fakeEnv{green: true}
	r = e.Execute(context.Background(), nil)
	if r.Exit != 1 || r.Output != "SPRINT REFUSED next: the sprint is not set up: nova-sprint up --as <coordinator>\n" {
		t.Fatal(r)
	}
	e.Env = fakeEnv{green: true, schema: true}
	r = e.Execute(context.Background(), nil)
	if r.Exit != 1 || !strings.Contains(r.Output, "nova-sprint hook --as <you> --role worker") {
		t.Fatal(r)
	}

}
