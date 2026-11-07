package run

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// One coordinator hooked in and not in nova-config is one roster
// judgement, not one from the steps and another from the policy (the e2e
// found two for one coordinator): the steps own it, group roster:<name>.
func TestOneUnlistedCoordinatorIsOneJudgement(t *testing.T) {
	t.Parallel()
	f, ro := seeded(t)
	must(t, f, "coord", store.PutRow{Name: "coord-1", Session: "sess-2", Role: "coordinator"})
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 3)
	var got []store.Judgement
	for _, j := range tk.Snapshot().Judgements {
		if j.About == "coord-1" {
			got = append(got, j)
		}
	}
	if len(got) != 1 || got[0].Kind != "roster" || got[0].Group != "roster:coord-1" || got[0].State != "open" {
		t.Fatalf("want one open roster judgement grouped roster:coord-1, got %+v", got)
	}
}
