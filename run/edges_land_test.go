package run

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// A landing waits on a pending CI for as long as it runs, and on an absent
// one (no check at all on the gated sha) only until CIAbsentWait (defect 40).
func TestWaitAgainBoundsAnAbsentCI(t *testing.T) {
	pending := git.LandResult{Outcome: git.OutcomeCIPending}
	absent := git.LandResult{Outcome: git.OutcomeCIAbsent, GatedSHA: strings.Repeat("a", 40)}
	if w, n := waitAgain_internal(pending, nil, 7); !w || n != 0 {
		t.Fatalf("pending: wait %v count %d, want true 0", w, n)
	}
	n, passes := 0, 0
	for {
		var w bool
		w, n = waitAgain_internal(absent, nil, n)
		passes++
		if !w {
			break
		}
		if passes > 1000 {
			t.Fatal("an absent CI is waited for without bound")
		}
	}
	if want := int(CIAbsentWait / LandPoll); passes != want {
		t.Fatalf("an absent CI stopped after %d passes, want %d", passes, want)
	}
	if w, _ := waitAgain_internal(absent, errors.New("boom"), 0); w {
		t.Fatal("a failed pass waits")
	}
	if w, _ := waitAgain_internal(git.LandResult{Outcome: git.OutcomeCIRed}, nil, 3); w {
		t.Fatal("a red pass waits")
	}
	it := landItem("land.s1.x", absent, nil)
	if it.Outcome != "no-ci" || !strings.Contains(it.Reason, absent.GatedSHA) {
		t.Fatalf("absent item %+v", it)
	}
}

// A red gate's item carries the failing checks and the URL to the fix.
func TestLandItemRedCarriesTheFinding(t *testing.T) {
	res := git.LandResult{Outcome: git.OutcomeCIRed, BlamedHead: "h1", FailingCI: []string{"unit (linux)", "ci-ok"}, CIURL: "https://ci/1", Reason: "CI red: unit (linux), ci-ok"}
	it := landItem("land.s1.x", res, nil)
	if it.Outcome != "red" || it.Blamed != "h1" {
		t.Fatalf("red item %+v", it)
	}
	for _, want := range []string{"unit (linux)", "ci-ok", "https://ci/1"} {
		if !strings.Contains(it.Reason, want) {
			t.Fatalf("red finding %q lacks %q", it.Reason, want)
		}
	}
}

// readyForge is a forge that refuses before anything runs.
type readyForge struct {
	forge.Forge
	err error
}

func (f readyForge) Ready() error { return f.err }

// A landing with no forge token merges and pushes nothing: it answers forge
// with the refusal, which layer 3 opens as one judgement (defect 39).
func TestLandRefusesWithoutAToken(t *testing.T) {
	refusal := &edge.Refusal{Problem: "the forge has no GitHub token for sprint", Fix: "run it under nova-secrets exec --only GH_GATE_TOKEN"}
	e := &Edges{Forge: readyForge{err: refusal}} // a nil Git: any git call would panic
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{"h"}}, store.Stream{Name: "s1", Forge: "github", Mode: "auto"})
	got := e.Results()
	if len(got) != 1 || got[0].Outcome != "forge" || !strings.Contains(got[0].Reason, "nova-secrets exec --only GH_GATE_TOKEN") {
		t.Fatalf("results %+v", got)
	}
}
