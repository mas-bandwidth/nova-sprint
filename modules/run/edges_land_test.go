package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A landing asks a pending CI and a moved base again for as long as they
// last, and an absent CI (no check at all on the gated sha) only until the
// request's wait; everything else goes to layer 3 at once.
func TestAskAgainBoundsAnAbsentCI(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// A red pass on a red base leads with both sides, plainly: the base itself
// is red at its tip on its checks, and the card's head on its own, so the
// coordinator judges the whole picture (LANDING.md "A red base"); a red pass
// on a green base says nothing of the base.
func TestLandItemRedBaseNamesBothSides(t *testing.T) {
	t.Parallel()
	res := git.LandResult{Outcome: git.OutcomeRed, Rung: 1, BaseTip: "b1", BaseFailing: []string{"corpus"}, FailingCI: []string{"corpus", "unit"}, Reason: "red"}
	it := landItem("land.s1.x", res, nil, time.Minute)
	want := "the base itself is red at its tip b1 on: corpus; the card's head is red on: corpus, unit; rung 1"
	if it.Outcome != string(git.OutcomeRed) || !strings.HasPrefix(it.Reason, want) {
		t.Fatalf("red item %q\nwant prefix %q", it.Reason, want)
	}
	res.BaseFailing = nil
	if it := landItem("land.s1.y", res, nil, time.Minute); strings.Contains(it.Reason, "base itself") {
		t.Fatalf("a green base is named red: %q", it.Reason)
	}
}

// A stream that lands by hand (mode hand, or no forge) merges and pushes
// nothing: it answers hand at once, which layer 3 opens as one judgement.
func TestLandByHandStreamTouchesNothing(t *testing.T) {
	t.Parallel()
	for _, s := range []store.Stream{{Name: "s1", Mode: "hand", Forge: "github"}, {Name: "s1", Mode: "auto"}} {
		e := &Edges{} // a nil Git and Forge: any call would panic
		e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{"h"}, Branches: []string{"sprint/s1/x"}}, s)
		got := e.Results()
		if len(got) != 1 || got[0].Outcome != "hand" || !strings.Contains(got[0].Reason, "by hand") {
			t.Fatalf("results %+v", got)
		}
	}
}

// onGit is a repository where every head and merge is already on every
// tip, and main is origin's default branch.
type onGit struct{ git.Git }

func (onGit) Clone(context.Context, string, string) error    { return nil }
func (onGit) Fetch(context.Context, string, ...string) error { return nil }
func (onGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return true, nil
}
func (onGit) LsRemote(context.Context, string, string) (string, bool, error) {
	return strings.Repeat("b", 40), true, nil
}
func (onGit) Main(context.Context, string) (string, string, error) {
	return "main", strings.Repeat("b", 40), nil
}

// A landing onto main says its merge is on main; onto any other base it
// does not, so layer 3 records the base merge and not landed; a promotion
// of that base dispatches PromotePass and its landed merges are on main
// (LANDING.md, "Landed means on main").
func TestLandSaysWhetherTheMergeIsOnMain(t *testing.T) {
	t.Parallel()
	h := strings.Repeat("a", 40)
	for _, c := range []struct {
		kind, base string
		main       bool
	}{{"land", "main", true}, {"land", "sprint/w", false}, {"promote", "sprint/w", true}} {
		e := &Edges{Git: onGit{}, Forge: pendingForge{}, ReposDir: t.TempDir(), Pause: func(context.Context, time.Duration) error { return nil }}
		e.land(context.Background(), steps.EdgeRequest{ID: c.kind + ".s1.x", Kind: c.kind, Stream: "s1", Heads: []string{h}, Branches: []string{"sprint/s1/x"}, Merges: []string{h}},
			store.Stream{Name: "s1", Repo: "o/r", Base: c.base, Forge: "github"})
		got := e.Results()
		if len(got) != 1 || got[0].Outcome != "landed" || got[0].Main != c.main || len(got[0].Landed) != 1 {
			t.Fatalf("%s onto %s: results %+v, want landed with main %v", c.kind, c.base, got, c.main)
		}
	}
}

// restGit is a repository whose card branch (at a...) holds main's tip
// (b...) and is on main only once landed is set: a rung-1 landing that
// reaches the forge until then.
type restGit struct {
	onGit
	landed *bool
}

func (restGit) LsRemote(_ context.Context, _, ref string) (string, bool, error) {
	if strings.HasPrefix(ref, "refs/heads/sprint/") {
		return strings.Repeat("a", 40), true, nil
	}
	return strings.Repeat("b", 40), true, nil
}

func (g restGit) IsAncestor(_ context.Context, _, ancestor, _ string) (bool, error) {
	return *g.landed || ancestor != strings.Repeat("a", 40), nil
}

// limitedForge is a forge GitHub rate-limits on every call.
type limitedForge struct{ forge.Forge }

func (limitedForge) PR(context.Context, string, string, string) (forge.PRInfo, bool, error) {
	return forge.PRInfo{}, false, &forge.FaultError{Kind: forge.FaultRateLimit, Identity: forge.HostLogin, RetryAfter: 1060, Reason: "HTTP 403: API rate limit exceeded"}
}

// A landing whose forge GitHub rate-limits waits: it is asked again every
// LandPoll like a pending CI, said once as an edge fault when the wait
// starts, and answers layer 3 only with what the landing itself came to
// (here landed, after two rate-limited passes). It is never a land
// judgement per card (LAYER-4-EDGES "Rate limits": a fault that spends
// nothing).
func TestLandWaitsOutARateLimitedForge(t *testing.T) {
	t.Parallel()
	if again, _ := askAgain_internal(git.LandResult{Outcome: git.OutcomeRested}, nil, 0, time.Minute); !again {
		t.Fatal("a rate-limited pass is not asked again")
	}
	// A rest is no fact about the gated sha's CI: the time it has had no
	// check at all is kept, neither restarted nor counted on.
	if again, absent := askAgain_internal(git.LandResult{Outcome: git.OutcomeRested}, nil, 2*LandPoll, 5*LandPoll); !again || absent != 2*LandPoll {
		t.Fatalf("a rested pass after 2 absent ones: again %v, absent %s; want true and %s kept", again, absent, 2*LandPoll)
	}
	landed, pauses := false, 0
	var said []string
	e := &Edges{Git: restGit{landed: &landed}, Forge: limitedForge{}, ReposDir: t.TempDir(),
		Fault: func(what string, err error) { said = append(said, what+": "+err.Error()) },
		Pause: func(_ context.Context, d time.Duration) error {
			if pauses++; d != LandPoll || pauses > 2 {
				return fmt.Errorf("pause %d of %s", pauses, d)
			}
			landed = pauses == 2
			return nil
		}}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{strings.Repeat("a", 40)}, Branches: []string{"sprint/s1/x"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got := e.Results()
	if len(got) != 1 || got[0].Outcome != "landed" || pauses != 2 {
		t.Fatalf("results %+v after %d pauses, want landed after 2", got, pauses)
	}
	if len(said) != 1 || !strings.Contains(said[0], "land.s1.x") || !strings.Contains(said[0], "rate limit") {
		t.Fatalf("said %q, want the rate limit said once for land.s1.x", said)
	}
}
