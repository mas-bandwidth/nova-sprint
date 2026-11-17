package run

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A: a failed check while others still run (unfinished) is asked again
// only until the request's wait, like an absent CI, and then goes to layer
// 3 as ci_no_verdict ("no verdict after <wait>: <n> checks still pending"),
// never as ci_red: what failed so far is named as such, not as failing.
func TestUnfinishedCIIsBoundedThenNoVerdict(t *testing.T) {
	t.Parallel()
	unfinished := git.LandResult{Outcome: git.LandOutcome("ci_unfinished"), Rung: 2, GatedSHA: strings.Repeat("a", 40), Pending: 67, FailingCI: []string{"lint"}, CIURL: "https://ci/1"}
	wait := 4 * LandPoll
	passes, waited := 0, time.Duration(0)
	for again := true; again; passes++ {
		again, waited = askAgain_internal(unfinished, nil, waited, wait)
		if passes > 1000 {
			t.Fatal("an unfinished CI is waited for without bound")
		}
	}
	if passes != 4 {
		t.Fatalf("an unfinished CI stopped after %d passes, want 4", passes)
	}
	it := landItem("land.s1.x", unfinished, nil, wait)
	if it.Outcome != "ci_no_verdict" || !strings.Contains(it.Reason, "no verdict after 2m0s: 67 checks still pending; failed so far: lint") {
		t.Fatalf("item %+v, want ci_no_verdict after the wait with the pending count", it)
	}
	if strings.Contains(it.Reason, "failing lint") || strings.Contains(it.Reason, "https://ci/1") {
		t.Fatalf("reason %q names a check of an unfinished run as failing", it.Reason)
	}
	if again, _ := askAgain_internal(git.LandResult{Outcome: git.LandOutcome("ci_no_verdict")}, nil, 0, wait); again {
		t.Fatal("a no-verdict pass is asked again by the run")
	}
}

// D: the judgement names the checks stopped rather than failed apart from
// the failing ones.
func TestLandFactsNameStoppedApart(t *testing.T) {
	t.Parallel()
	res := git.LandResult{Outcome: git.OutcomeRed, Rung: 1, FailingCI: []string{"unit"}, Stopped: []string{"corpus killed (exit code 137)"}, Reason: "CI red"}
	it := landItem("land.s1.x", res, nil, time.Minute)
	if !strings.Contains(it.Reason, "failing unit; stopped, not failed (no verdict): corpus killed (exit code 137)") {
		t.Fatalf("reason %q, want the stopped check named apart", it.Reason)
	}
}

// stopForge is a forge whose gated head (a...) is red with its one failing
// check killed (the forge has no way to run CI again).
type stopForge struct {
	forge.Forge
}

func (f *stopForge) Runs(context.Context, string) ([]forge.Run, error) { return nil, nil }
func (f *stopForge) RunsAt(context.Context, string, string, string) ([]forge.Run, error) {
	return nil, nil
}

func (f *stopForge) PR(context.Context, string, string, string) (forge.PRInfo, bool, error) {

	return forge.PRInfo{Number: 1, HeadSHA: strings.Repeat("a", 40)}, true, nil
}

func (f *stopForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	return map[string]forge.CheckRollup{shas[0]: {SHA: shas[0], State: forge.CheckRed, Failing: []string{"corpus"}}}, nil
}

func (f *stopForge) FailedLines(context.Context, string, string) ([]string, error) {
	return []string{"signal: killed"}, nil
}

func (f *stopForge) Failures(context.Context, string, string) ([]forge.Failure, error) {
	return []forge.Failure{{Name: "corpus", URL: "https://github.com/o/r/actions/runs/7/job/8", Stop: "killed (exit code 137)"}}, nil
}

func (f *stopForge) Merge(context.Context, string, int, string) (string, error) {
	return strings.Repeat("b", 40), nil
}

// D: a landing whose failing checks were all stopped is the coordinator's
// ci_no_verdict at once: nothing is run again by the sprint (LESSONS-PRIORITY-AND-MERGE W19),
// the stopped check is named apart, and the judgement carries its first
// failing lines.
func TestLandStoppedChecksAreNoVerdictAtOnce(t *testing.T) {
	t.Parallel()
	landed := false
	f := &stopForge{}
	pauses := 0
	e := &Edges{Git: restGit{landed: &landed}, Forge: f, ReposDir: t.TempDir(),
		Pause: func(_ context.Context, d time.Duration) error {
			if pauses++; pauses > 3 {
				return fmt.Errorf("pause %d", pauses)
			}
			return nil
		}}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{strings.Repeat("a", 40)}, Branches: []string{"sprint/s1/x"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got := e.Results()
	if len(got) != 1 || got[0].Outcome != "ci_no_verdict" || pauses != 0 {
		t.Fatalf("results %+v after %d pauses, want ci_no_verdict at once", got, pauses)
	}
	for _, want := range []string{"stopped, not failed (no verdict): corpus killed (exit code 137)", "first failing lines: signal: killed"} {
		if !strings.Contains(got[0].Reason, want) {
			t.Fatalf("judgement %q, want %q in it", got[0].Reason, want)
		}
	}
}

type basePollForge struct {
	stopForge
	baseCalls int
}

func (f *basePollForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	sha := shas[0]
	if sha == strings.Repeat("a", 40) {
		return map[string]forge.CheckRollup{sha: {SHA: sha, State: forge.CheckRed, Failing: []string{"corpus"}, Ran: []string{"corpus"}}}, nil
	}
	f.baseCalls++
	if f.baseCalls == 1 {
		return map[string]forge.CheckRollup{sha: {SHA: sha, State: forge.CheckPending, Pending: 3}}, nil
	}
	return map[string]forge.CheckRollup{sha: {SHA: sha, State: forge.CheckGreen, Ran: []string{"corpus"}}}, nil
}

func (f *basePollForge) Failures(_ context.Context, _, sha string) ([]forge.Failure, error) {
	return []forge.Failure{{Name: "corpus", URL: "https://github.com/o/r/actions/runs/7/job/8"}}, nil
}

// With SharedRed enabled, a red head whose base tip CI is still pending polls
// until the base tip's CI completes, then classifies OwnRed cleanly.
func TestLandPollsWhenBaseCIIsStillRunning(t *testing.T) {
	t.Parallel()
	landed := false
	f := &basePollForge{}
	pauses := 0
	e := &Edges{Git: restGit{landed: &landed}, Forge: f, ReposDir: t.TempDir(),
		Pause: func(context.Context, time.Duration) error { pauses++; return nil }}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", SharedRed: true, Heads: []string{strings.Repeat("a", 40)}, Branches: []string{"sprint/s1/x"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got := e.Results()
	if len(got) != 1 || got[0].Outcome != "ci_red" || !got[0].OwnRed || pauses != 1 {
		t.Fatalf("results %+v after %d pauses, want ci_red with OwnRed=true after 1 poll", got, pauses)
	}
}

type rerunForge struct {
	stopForge
	reruns    []int64
	greenFrom int
}

func (f *rerunForge) PR(_ context.Context, _, _, _ string) (forge.PRInfo, bool, error) {
	return forge.PRInfo{Number: 1}, true, nil
}

func (f *rerunForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	sha := shas[0]
	if f.greenFrom > 0 && len(f.reruns) >= f.greenFrom {
		return map[string]forge.CheckRollup{sha: {SHA: sha, State: forge.CheckGreen}}, nil
	}
	return map[string]forge.CheckRollup{sha: {SHA: sha, State: forge.CheckRed, Failing: []string{"corpus"}}}, nil
}

func (f *rerunForge) RerunFailed(_ context.Context, _ string, id int64) error {
	f.reruns = append(f.reruns, id)
	return nil
}

func TestLandRerunsStoppedChecksBounded(t *testing.T) {
	t.Parallel()
	landed := false
	rf := &rerunForge{greenFrom: 1}
	e := &Edges{Git: restGit{landed: &landed}, Forge: rf, ReposDir: t.TempDir(),
		Pause: func(context.Context, time.Duration) error { return nil }}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.1", Kind: "land", Stream: "s1", SharedRed: true, Heads: []string{strings.Repeat("a", 40)}, Branches: []string{"sprint/s1/x"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got := e.Results()
	if len(got) != 1 || got[0].Outcome != "landed" || len(rf.reruns) != 1 || rf.reruns[0] != 7 {
		t.Fatalf("transient solo rerun: %+v reruns=%v, want landed after 1 rerun of 7", got, rf.reruns)
	}

	landed = false
	rf = &rerunForge{}
	e = &Edges{Git: restGit{landed: &landed}, Forge: rf, ReposDir: t.TempDir(),
		Pause: func(context.Context, time.Duration) error { return nil }}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.2", Kind: "land", Stream: "s1", SharedRed: true, Heads: []string{strings.Repeat("a", 40)}, Branches: []string{"sprint/s1/x"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got = e.Results()
	if len(got) != 1 || got[0].Outcome != "ci_no_verdict" || !got[0].OwnRed || len(rf.reruns) != 2 {
		t.Fatalf("persistent solo rerun: %+v reruns=%v, want ci_no_verdict OwnRed=true after 2 reruns", got, rf.reruns)
	}
}
