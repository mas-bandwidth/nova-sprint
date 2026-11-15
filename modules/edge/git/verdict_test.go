package git

import (
	"context"
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
// the head's failures fs (nil: the rollup's own).
func verdictPass_internal(t *testing.T, r forge.CheckRollup, fs []forge.Failure) (LandResult, *fakeForge_internal) {
	t.Helper()
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, r)
	if fs != nil {
		ff.SetFailures("o/r", c1, fs)
	}
	req := one_internal(c1)
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || gf.OriginRef("v2") != root {
		t.Fatalf("pass %+v err=%v: nothing lands", res, err)
	}
	return res, ff
}

// A: one fast check failed while 67 still run is no verdict yet: the pass
// is unfinished (asked again until the wait), names what failed so far and
// how many still run, reads no failing log (the run has not completed: gh
// run view refuses it) and nothing of the base (no runs compared).
func TestLandRedWhileChecksRunIsNoVerdictYet(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"lint"}, URL: verdictJob1, Waiting: true, Pending: 67}
	res, ff := verdictPass_internal(t, r, nil)
	if res.Outcome != LandOutcome("ci_unfinished") || res.Outcome == OutcomeRed {
		t.Fatalf("%+v, want ci_unfinished, never ci_red, while checks still run", res)
	}
	if !strings.Contains(res.Reason, "67 checks still pending") || !strings.Contains(res.Reason, "lint") {
		t.Fatalf("reason %q, want the 67 pending and lint failed so far", res.Reason)
	}
	if calls := ff.calls_internal(); slices.Contains(calls, "FailedLines") || slices.Contains(calls, "Failures") || slices.Contains(calls, "Runs") || len(res.FailingLines) != 0 || len(res.BaseRed) != 0 {
		t.Fatalf("calls %v, lines %v, base %v: a run still in progress has no log and no verdict to compare", calls, res.FailingLines, res.BaseRed)
	}
	// A faulted check while others run is no verdict yet either.
	r = forge.CheckRollup{State: forge.CheckFault, Failing: []string{"runner"}, Waiting: true, Pending: 3}
	if res, _ = verdictPass_internal(t, r, nil); res.Outcome != LandOutcome("ci_unfinished") {
		t.Fatalf("%+v, want ci_unfinished for a fault while checks still run", res)
	}
}

// A: once every check completed, a red is judged at once with its log read
// once.
func TestLandRedAfterChecksCompletedIsJudged(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: verdictJob1}
	res, ff := verdictPass_internal(t, r, nil)
	if res.Outcome != OutcomeRed || !slices.Equal(res.FailingCI, []string{"unit"}) || len(res.FailingLines) == 0 {
		t.Fatalf("%+v, want ci_red on unit with its lines", res)
	}
	if n := len(slices.DeleteFunc(ff.calls_internal(), func(c string) bool { return c != "FailedLines" })); n != 1 {
		t.Fatalf("the failing log was read %d times, want once", n)
	}
}

// D: a failing check GitHub says was stopped (killed, cancelled, timed out)
// is named apart from the ones that failed on their own, and the first
// failing lines are the first own failure's.
func TestLandStoppedChecksAreNamedApart(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "unit"}, URL: verdictJob1}
	fs := []forge.Failure{{Name: "corpus", URL: verdictJob1, Stop: "killed (exit code 137)"}, {Name: "unit", URL: verdictJob3}}
	res, _ := verdictPass_internal(t, r, fs)
	if res.Outcome != OutcomeRed || !slices.Equal(res.FailingCI, []string{"unit"}) || !slices.Equal(res.Stopped, []string{"corpus killed (exit code 137)"}) || res.CIURL != verdictJob3 {
		t.Fatalf("%+v, want ci_red on unit at its url, corpus named stopped", res)
	}
}

// D: every failing check stopped is no verdict about the code, and the
// coordinator's at once: the sprint runs nothing again (LESSONS-PRIORITY-AND-MERGE W19). The
// judgement names each stopped check and carries the first stopped check's
// failing lines, its log read once.
func TestLandEveryCheckStoppedIsNoVerdict(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "cpp"}, URL: verdictJob1}
	fs := []forge.Failure{{Name: "corpus", URL: verdictJob1, Stop: "killed (exit code 137)"}, {Name: "cpp", URL: verdictJob2, Stop: "killed (exit code 137)"}, {Name: "lua", URL: verdictJob3, Stop: "cancelled"}}
	res, ff := verdictPass_internal(t, r, fs)
	calls := ff.calls_internal()
	if res.Outcome != LandOutcome("ci_no_verdict") || len(res.FailingCI) != 0 {
		t.Fatalf("%+v, calls %v: want ci_no_verdict at once, nothing run again, nothing named failing", res, calls)
	}
	if !slices.Equal(res.FailingLines, []string{"--- FAIL: TestX", "FAIL"}) || res.CIURL != verdictJob1 || len(slices.DeleteFunc(calls, func(c string) bool { return c != "FailedLines" })) != 1 {
		t.Fatalf("lines %v at %s: want the first stopped check's failing lines, read once", res.FailingLines, res.CIURL)
	}
	for _, want := range []string{"corpus killed (exit code 137)", "lua cancelled"} {
		if !slices.Contains(res.Stopped, want) {
			t.Fatalf("stopped %v lack %q", res.Stopped, want)
		}
	}
}

// D: a cancelled-only rollup (a fault) is no verdict too, at once, with
// its failing lines; a faulted check that was not stopped (a workflow that
// never started) stays the coordinator's ci_fault, at once.
func TestLandCancelledOnlyIsNoVerdict(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckFault, Failing: []string{"lua"}, URL: verdictJob3}
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, r)
	ff.SetFailures("o/r", c1, []forge.Failure{{Name: "lua", URL: verdictJob3, Stop: "cancelled"}})
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != LandOutcome("ci_no_verdict") || len(res.FailingLines) == 0 || gf.OriginRef("v2") != root {
		t.Fatalf("%+v err=%v, want ci_no_verdict with its lines and nothing run again", res, err)
	}
	ff.SetFailures("o/r", c1, []forge.Failure{{Name: "lua", URL: verdictJob3}})
	if res, _ = LandPass(context.Background(), gf, ff, one_internal(c1)); res.Outcome != OutcomeCIFault {
		t.Fatalf("%+v, want ci_fault", res)
	}
}
