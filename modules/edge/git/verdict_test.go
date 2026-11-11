package git

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

const (
	verdictJob1 = "https://github.com/o/r/actions/runs/11/job/21"
	verdictJob2 = "https://github.com/o/r/actions/runs/11/job/22"
	verdictJob3 = "https://github.com/o/r/actions/runs/12/job/31"
)

// verdictPass_internal is one rung-1 pass with the gated head's rollup r,
// the head's failures fs (nil: the rollup's own) and the base tip's rollup
// base (nil: none reported); reran is LandRequest.Reran.
func verdictPass_internal(t *testing.T, r forge.CheckRollup, fs []forge.Failure, base *forge.CheckRollup, reran bool) (LandResult, *fakeForge_internal) {
	t.Helper()
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, r)
	if fs != nil {
		ff.SetFailures("o/r", c1, fs)
	}
	if base != nil {
		ff.SetCheck("o/r", root, *base)
	}
	req := one_internal(c1)
	req.Reran = reran
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || gf.OriginRef("v2") != root {
		t.Fatalf("pass %+v err=%v: nothing lands", res, err)
	}
	return res, ff
}

// A: one fast check failed while 67 still run is no verdict yet: the pass
// is unfinished (asked again until the wait), names what failed so far and
// how many still run, reads no failing log (the run has not completed: gh
// run view refuses it) and no base checks.
func TestLandRedWhileChecksRunIsNoVerdictYet(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"lint"}, URL: verdictJob1, Waiting: true, Pending: 67}
	res, ff := verdictPass_internal(t, r, nil, &forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}}, false)
	if res.Outcome != LandOutcome("ci_unfinished") || res.Outcome == OutcomeRed {
		t.Fatalf("%+v, want ci_unfinished, never ci_red, while checks still run", res)
	}
	if !strings.Contains(res.Reason, "67 checks still pending") || !strings.Contains(res.Reason, "lint") {
		t.Fatalf("reason %q, want the 67 pending and lint failed so far", res.Reason)
	}
	if calls := ff.calls_internal(); slices.Contains(calls, "FailedLines") || slices.Contains(calls, "Failures") || len(res.FailingLines) != 0 || len(res.BaseFailing) != 0 {
		t.Fatalf("calls %v, lines %v, base %v: a run still in progress has no log and no verdict to compare", calls, res.FailingLines, res.BaseFailing)
	}
	// A faulted check while others run is no verdict yet either.
	r = forge.CheckRollup{State: forge.CheckFault, Failing: []string{"runner"}, Waiting: true, Pending: 3}
	if res, _ = verdictPass_internal(t, r, nil, nil, false); res.Outcome != LandOutcome("ci_unfinished") {
		t.Fatalf("%+v, want ci_unfinished for a fault while checks still run", res)
	}
}

// A: once every check completed, a red is judged at once with its log read
// once.
func TestLandRedAfterChecksCompletedIsJudged(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: verdictJob1}
	res, ff := verdictPass_internal(t, r, nil, nil, false)
	if res.Outcome != OutcomeRed || !slices.Equal(res.FailingCI, []string{"unit"}) || len(res.FailingLines) == 0 {
		t.Fatalf("%+v, want ci_red on unit with its lines", res)
	}
	if n := len(slices.DeleteFunc(ff.calls_internal(), func(c string) bool { return c != "FailedLines" })); n != 1 {
		t.Fatalf("the failing log was read %d times, want once", n)
	}
}

// B: the base's red list is stated only from the base's completed checks:
// a base whose checks still run is said as running with what is red so
// far, and never compared with the head's.
func TestLandBaseRedOnlyFromItsCompletedChecks(t *testing.T) {
	t.Parallel()
	head := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: verdictJob1}
	running := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"luacheck"}, Waiting: true, Pending: 115}
	res, _ := verdictPass_internal(t, head, nil, &running, false)
	if res.Outcome != OutcomeRed || len(res.BaseFailing) != 0 {
		t.Fatalf("%+v, want ci_red with no base red stated while the base's checks run", res)
	}
	if !strings.Contains(res.Reason, "base CI still running") || !strings.Contains(res.Reason, "1 red so far (luacheck)") {
		t.Fatalf("reason %q, want the base still running with its red so far", res.Reason)
	}
	pending := forge.CheckRollup{State: forge.CheckPending}
	if res, _ = verdictPass_internal(t, head, nil, &pending, false); len(res.BaseFailing) != 0 || !strings.Contains(res.Reason, "base CI still running") || !strings.Contains(res.Reason, "0 red so far") {
		t.Fatalf("%+v, want a pending base said as running", res)
	}
	done := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"luacheck", "corpus"}}
	if res, _ = verdictPass_internal(t, head, nil, &done, false); !slices.Equal(res.BaseFailing, done.Failing) {
		t.Fatalf("%+v, want the completed base's red stated", res)
	}
}

// D: a failing check GitHub says was stopped (killed, cancelled, timed out)
// is named apart from the ones that failed on their own, and the first
// failing lines are the first own failure's.
func TestLandStoppedChecksAreNamedApart(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "unit"}, URL: verdictJob1}
	fs := []forge.Failure{{Name: "corpus", URL: verdictJob1, Stop: "killed (exit code 137)"}, {Name: "unit", URL: verdictJob3}}
	res, ff := verdictPass_internal(t, r, fs, nil, false)
	if res.Outcome != OutcomeRed || !slices.Equal(res.FailingCI, []string{"unit"}) || !slices.Equal(res.Stopped, []string{"corpus killed (exit code 137)"}) || res.CIURL != verdictJob3 {
		t.Fatalf("%+v, want ci_red on unit at its url, corpus named stopped", res)
	}
	if slices.Contains(ff.calls_internal(), "Rerun") {
		t.Fatal("a red with an own failure ran jobs again")
	}
}

// D: every failing check stopped is no verdict about the code: the
// stopped jobs are run again once, one rerun per Actions run (rerun, asked
// again), and with Reran set it is the coordinator's no-verdict judgement.
func TestLandEveryCheckStoppedIsRunAgainOnce(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "cpp"}, URL: verdictJob1}
	fs := []forge.Failure{{Name: "corpus", URL: verdictJob1, Stop: "killed (exit code 137)"}, {Name: "cpp", URL: verdictJob2, Stop: "killed (exit code 137)"}, {Name: "lua", URL: verdictJob3, Stop: "cancelled"}}
	res, ff := verdictPass_internal(t, r, fs, nil, false)
	if res.Outcome != LandOutcome("ci_rerun") || !slices.Equal(ff.reruns, []string{verdictJob1, verdictJob3}) || slices.Contains(ff.calls_internal(), "FailedLines") {
		t.Fatalf("%+v, reruns %v, calls %v: want ci_rerun with runs 11 and 12 asked once each and no log read", res, ff.reruns, ff.calls_internal())
	}
	res, ff = verdictPass_internal(t, r, fs, nil, true)
	if res.Outcome != LandOutcome("ci_no_verdict") || len(ff.reruns) != 0 || len(res.FailingCI) != 0 || !strings.Contains(res.Reason, "already run again once") {
		t.Fatalf("%+v, reruns %v: want ci_no_verdict, no second rerun, nothing named failing", res, ff.reruns)
	}
	for _, want := range []string{"corpus killed (exit code 137)", "lua cancelled"} {
		if !slices.Contains(res.Stopped, want) {
			t.Fatalf("stopped %v lack %q", res.Stopped, want)
		}
	}
}

// D: a cancelled-only rollup (a fault) is no verdict too, run again once;
// a rerun GitHub refuses leaves the no-verdict judgement with its reason,
// and a rate-limited one waits (forge_rested).
func TestLandStoppedRerunRefusedOrRested(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckFault, Failing: []string{"lua"}, URL: verdictJob3}
	fs := []forge.Failure{{Name: "lua", URL: verdictJob3, Stop: "cancelled"}}
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, r)
	ff.SetFailures("o/r", c1, fs)
	ff.RerunErr = errors.New("HTTP 403: Resource not accessible by integration")
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != LandOutcome("ci_no_verdict") || !strings.Contains(res.Reason, "run 12 could not be run again") || gf.OriginRef("v2") != root {
		t.Fatalf("%+v err=%v, want ci_no_verdict saying the rerun failed", res, err)
	}
	ff.RerunErr = &forge.FaultError{Kind: forge.FaultRateLimit, Reason: "HTTP 403: API rate limit exceeded"}
	if res, err = LandPass(context.Background(), gf, ff, one_internal(c1)); err != nil || res.Outcome != OutcomeRested {
		t.Fatalf("%+v err=%v, want forge_rested", res, err)
	}
	// A faulted check that was not stopped (a workflow that never started)
	// stays the coordinator's ci_fault, at once.
	ff.RerunErr = nil
	ff.SetFailures("o/r", c1, []forge.Failure{{Name: "lua", URL: verdictJob3}})
	if res, _ = LandPass(context.Background(), gf, ff, one_internal(c1)); res.Outcome != OutcomeCIFault || len(ff.reruns) != 0 {
		t.Fatalf("%+v, want ci_fault and nothing run again", res)
	}
}
