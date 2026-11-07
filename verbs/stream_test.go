package verbs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type repoFake struct{}

func (repoFake) Readable(context.Context, string, string, bool) error { return nil }
func TestStreamAdd(t *testing.T) {
	f := store.NewFake()
	e := Engine{Store: f, Env: fakeEnv{schema: true, hooked: true}, Repositories: repoFake{}, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }}
	wireFixture(t, &e, f, "rowan")
	before, _ := f.Snapshot(context.Background())
	r := e.Execute(context.Background(), []string{"stream", "add", "docs", "--repo", "owner/repo", "--pr"})
	if r.Exit != 0 || r.Output != "STREAM ADDED docs repo=owner/repo base=main branch=sprint/docs\nnext: nova-sprint add --stream docs --brief-file <file>\n" {
		t.Fatal(r)
	}
	events, _ := f.Events(context.Background(), before.Seq, 100)
	if len(events) != 1 || events[0].After == nil || len(events[0].After.Policies) != 1 {
		t.Fatal(events)
	}
	for _, p := range events[0].After.Policies {
		if p.Name != "stream:docs" || p.Active || !strings.Contains(p.Body, `"pr":true`) {
			t.Fatal(p)
		}
	}
	s, _ := f.Snapshot(context.Background())
	if s.Streams["docs"].Repo != "owner/repo" {
		t.Fatal(s.Streams)
	}
	r = e.Execute(context.Background(), []string{"stream", "add", "docs", "--repo", "other/repo"})
	if r.Exit != 1 || r.Output != "SPRINT REFUSED stream add: stream docs exists: nova-sprint show docs\n" {
		t.Fatal(r)
	}
	s, _ = f.Snapshot(context.Background())
	if s.Streams["docs"].Repo != "owner/repo" {
		t.Fatal(s.Streams)
	}
}

func TestStreamModeSet(t *testing.T) {
	f := store.NewFake()
	e := Engine{Store: f, Env: fakeEnv{schema: true, hooked: true}, Repositories: repoFake{}, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }}
	wireFixture(t, &e, f, "rowan")
	r := e.Execute(context.Background(), []string{"stream", "add", "docs", "--repo", "owner/repo", "--pr"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	r = e.Execute(context.Background(), []string{"stream", "set", "docs", "--mode", "group"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	snap, _ := f.Snapshot(context.Background())
	body, found, err := (Choices{Store: f}).getAt(context.Background(), "stream:docs", snap)
	if err != nil || !found || !strings.Contains(body, `"mode":"group"`) || !strings.Contains(body, `"pr":true`) {
		t.Fatal(body, found, err)
	}
	bad := e.Execute(context.Background(), []string{"stream", "set", "docs", "--mode", "other"})
	if bad.Exit != 1 || !strings.Contains(bad.Output, "auto, group or hand") {
		t.Fatal(bad)
	}
}
