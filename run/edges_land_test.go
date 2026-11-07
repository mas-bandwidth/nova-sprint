package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// A landing asks a pending CI and a moved base again for as long as they
// last, and an absent CI (no check at all on the gated sha) only until the
// request's wait; everything else goes to layer 3 at once.
func TestAskAgainBoundsAnAbsentCI(t *testing.T) {
	pending := git.LandResult{Outcome: git.OutcomePending}
	moved := git.LandResult{Outcome: git.OutcomeBaseMoved}
	absent := git.LandResult{Outcome: git.OutcomeAbsent, GatedSHA: strings.Repeat("a", 40)}
	for _, res := range []git.LandResult{pending, moved} {
		if again, _ := askAgain_internal(res, nil, 0, time.Minute); !again {
			t.Fatalf("%s is not asked again", res.Outcome)
		}
	}
	wait := 5 * LandPoll
	passes, waited := 0, time.Duration(0)
	for again := true; again; passes++ {
		again, waited = askAgain_internal(absent, nil, waited, wait)
		if passes > 1000 {
			t.Fatal("an absent CI is waited for without bound")
		}
	}
	if passes != 5 {
		t.Fatalf("an absent CI stopped after %d passes, want 5", passes)
	}
	if again, _ := askAgain_internal(absent, errors.New("boom"), 0, wait); again {
		t.Fatal("a failed pass is asked again")
	}
	for _, o := range []git.LandOutcome{git.OutcomeRed, git.OutcomeConflict, git.OutcomeRefused, git.OutcomeUnopenable, git.OutcomeBranch, git.OutcomeCIFault, git.OutcomeMissing, git.OutcomeMoved, git.OutcomeLanded} {
		if again, _ := askAgain_internal(git.LandResult{Outcome: o}, nil, 0, wait); again {
			t.Fatalf("%s is asked again", o)
		}
	}
	it := landItem("land.s1.x", absent, nil, wait)
	if it.Outcome != "no-ci" || !strings.Contains(it.Reason, absent.GatedSHA) || !strings.Contains(it.Reason, wait.String()) {
		t.Fatalf("absent item %+v", it)
	}
}

// A red pass's item carries the facts the judgement shows: rung, base tip,
// group tip, gated sha, PR, failing checks, their URL and first lines.
func TestLandItemRedCarriesTheFacts(t *testing.T) {
	res := git.LandResult{Outcome: git.OutcomeRed, Rung: 2, BaseTip: "b1", GroupTip: "g1", GatedSHA: "g1", PR: "https://pr/7", FailingCI: []string{"unit (linux)"}, CIURL: "https://ci/1", FailingLines: []string{"--- FAIL: TestX"}, Reason: "failure"}
	it := landItem("land.s1.x", res, nil, time.Minute)
	if it.Outcome != string(git.OutcomeRed) || len(it.Landed) != 0 {
		t.Fatalf("red item %+v", it)
	}
	for _, want := range []string{"rung 2", "base tip b1", "group tip g1", "gated g1", "https://pr/7", "unit (linux)", "https://ci/1", "--- FAIL: TestX"} {
		if !strings.Contains(it.Reason, want) {
			t.Fatalf("red facts %q lack %q", it.Reason, want)
		}
	}
	landed := landItem("land.s1.y", git.LandResult{Outcome: git.OutcomeLanded, MergeSHA: "m1", LandedHeads: []string{"h1"}}, nil, time.Minute)
	if landed.Outcome != "landed" || landed.Merge != "m1" || len(landed.Landed) != 1 {
		t.Fatalf("landed item %+v", landed)
	}
	fault := landItem("land.s1.z", git.LandResult{}, errors.New("git: boom"), time.Minute)
	if fault.Outcome != "fault" || !strings.Contains(fault.Reason, "boom") {
		t.Fatalf("fault item %+v", fault)
	}
}

// A stream that lands by hand (mode hand, or no forge) merges and pushes
// nothing: it answers hand at once, which layer 3 opens as one judgement.
func TestLandByHandStreamTouchesNothing(t *testing.T) {
	for _, s := range []store.Stream{{Name: "s1", Mode: "hand", Forge: "github"}, {Name: "s1", Mode: "auto"}} {
		e := &Edges{} // a nil Git and Forge: any call would panic
		e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{"h"}, Branches: []string{"sprint/s1/x"}}, s)
		got := e.Results()
		if len(got) != 1 || got[0].Outcome != "hand" || !strings.Contains(got[0].Reason, "by hand") {
			t.Fatalf("results %+v", got)
		}
	}
}
