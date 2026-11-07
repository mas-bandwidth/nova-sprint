package verbs

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strings"
	"testing"
	"time"
)

type rowCatalogFake struct{}

func (rowCatalogFake) Catalog(_ context.Context, s store.Snapshot) (int, []string, []string, error) {
	return s.Policy, []string{"sol"}, []string{"codex"}, nil
}

func TestRemainingRowAdd(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, RowCatalog: rowCatalogFake{}}
	args := []string{"add", "r", "--kind", "friend", "--width", "2", "--tiers", "flash,pro", "--model", "sol", "--harness", "codex"}
	r := e.row(ctx, args)
	if r.Exit != 0 || !strings.Contains(r.Output, "next: nova-bus hook --as r") {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	row := s.Rows["r"]
	if row.Kind != "kin" || row.Width != 2 || row.Model != "sol" || row.Harness != "codex" || len(row.Tiers) != 2 {
		t.Fatal(row)
	}
	r = e.row(ctx, args)
	if r.Output != "SPRINT REFUSED row add: row r exists: nova-sprint show r\n" {
		t.Fatal(r)
	}
}
func TestRemainingRowRefusals(t *testing.T) {
	e := Engine{Store: store.NewFake(), Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }, RowCatalog: rowCatalogFake{}}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"add", "r", "--kind", "friend", "--width", "1", "--tiers", "pro", "--harness", "codex"}, "model is missing"},
		{[]string{"add", "r", "--kind", "friend", "--width", "1", "--tiers", "pro", "--model", "bad", "--harness", "codex"}, "model bad is not in policy version 0"},
		{[]string{"add", "r", "--kind", "worker", "--width", "1", "--tiers", "pro", "--model", "sol", "--harness", "bad"}, "harness bad is not in policy version 0"},
		{[]string{"add", "r", "--kind", "worker", "--width", "1", "--tiers", "pro,pro", "--model", "sol", "--harness", "codex"}, "tier pro is named twice"},
		{[]string{"add", "r", "--kind", "worker", "--width", "1", "--tiers", "pro", "--model", "sol", "--harness", "codex", "--as", "other"}, "only the coordinator"},
	}
	for _, tc := range cases {
		r := e.row(context.Background(), tc.args)
		if r.Exit != 1 || !strings.Contains(r.Output, tc.want) || strings.Count(r.Output, "\n") != 1 {
			t.Fatal(r)
		}
	}
}

func TestRemainingRowHookAndMissingHarness(t *testing.T) {
	e := Engine{Store: store.NewFake(), Env: fakeEnv{}, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }, RowCatalog: rowCatalogFake{}}
	missing := e.row(context.Background(), []string{"add", "r", "--kind", "worker", "--width", "1", "--tiers", "pro", "--model", "sol"})
	if missing.Output != "SPRINT REFUSED row add: harness is missing: nova-sprint help row add\n" {
		t.Fatal(missing)
	}
	got := e.row(context.Background(), []string{"add", "r", "--kind", "worker", "--width", "1", "--tiers", "pro", "--model", "sol", "--harness", "codex"})
	if got.Output != "SPRINT REFUSED row add: rowan is not hooked in: nova-bus hook --as rowan\n" {
		t.Fatal(got)
	}
}
