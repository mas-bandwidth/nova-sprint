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
// the head's failures fs (nil: the rollup's own) and the base tip's rollup
// base (nil: none reported).
func verdictPass_internal(t *testing.T, r forge.CheckRollup, fs []forge.Failure, base *forge.CheckRollup) (LandResult, *fakeForge_internal) {
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
	res, ff := verdictPass_internal(t, r, nil, &forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}})
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
	if res, _ = verdictPass_internal(t, r, nil, nil); res.Outcome != LandOutcome("ci_unfinished") {
		t.Fatalf("%+v, want ci_unfinished for a fault while checks still run", res)
	}
}

// A: once every check completed, a red is judged at once with its log read
// once.
func TestLandRedAfterChecksCompletedIsJudged(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: verdictJob1}
	res, ff := verdictPass_internal(t, r, nil, nil)
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
	res, _ := verdictPass_internal(t, head, nil, &running)
	if res.Outcome != OutcomeRed || len(res.BaseFailing) != 0 {
		t.Fatalf("%+v, want ci_red with no base red stated while the base's checks run", res)
	}
	if !strings.Contains(res.Reason, "base CI still running") || !strings.Contains(res.Reason, "1 red so far (luacheck)") {
		t.Fatalf("reason %q, want the base still running with its red so far", res.Reason)
	}
	pending := forge.CheckRollup{State: forge.CheckPending}
	if res, _ = verdictPass_internal(t, head, nil, &pending); len(res.BaseFailing) != 0 || !strings.Contains(res.Reason, "base CI still running") || !strings.Contains(res.Reason, "0 red so far") {
		t.Fatalf("%+v, want a pending base said as running", res)
	}
	done := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"luacheck", "corpus"}}
	if res, _ = verdictPass_internal(t, head, nil, &done); !slices.Equal(res.BaseFailing, done.Failing) {
		t.Fatalf("%+v, want the completed base's red stated", res)
	}
}

// B with D's rule: the base is called red only from its checks that failed
// on their own; a base check that was stopped is named apart in the
// reason, and a base whose failing checks were all stopped is not red.
func TestLandBaseRedOnlyFromItsOwnFailures(t *testing.T) {
	t.Parallel()
	head := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: verdictJob1}
	base := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "luacheck"}, URL: verdictJob2}
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, head)
	ff.SetCheck("o/r", root, base)
	ff.SetFailures("o/r", root, []forge.Failure{{Name: "corpus", URL: verdictJob2, Stop: "killed (exit code 137)"}, {Name: "luacheck", URL: verdictJob3}})
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeRed || !slices.Equal(res.BaseFailing, []string{"luacheck"}) || !strings.Contains(res.Reason, "corpus killed (exit code 137)") {
		t.Fatalf("%+v err=%v, want the base red on luacheck only, corpus said stopped", res, err)
	}
	ff.SetFailures("o/r", root, []forge.Failure{{Name: "corpus", URL: verdictJob2, Stop: "killed (exit code 137)"}, {Name: "luacheck", URL: verdictJob3, Stop: "cancelled"}})
	if res, _ = LandPass(context.Background(), gf, ff, one_internal(c1)); res.Outcome != OutcomeRed || len(res.BaseFailing) != 0 || !strings.Contains(res.Reason, "the base tip's failing checks were all stopped") {
		t.Fatalf("%+v, want no base red when every failing base check was stopped", res)
	}
}

// D: a failing check GitHub says was stopped (killed, cancelled, timed out)
// is named apart from the ones that failed on their own, and the first
// failing lines are the first own failure's.
func TestLandStoppedChecksAreNamedApart(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "unit"}, URL: verdictJob1}
	fs := []forge.Failure{{Name: "corpus", URL: verdictJob1, Stop: "killed (exit code 137)"}, {Name: "unit", URL: verdictJob3}}
	res, _ := verdictPass_internal(t, r, fs, nil)
	if res.Outcome != OutcomeRed || !slices.Equal(res.FailingCI, []string{"unit"}) || !slices.Equal(res.Stopped, []string{"corpus killed (exit code 137)"}) || res.CIURL != verdictJob3 {
		t.Fatalf("%+v, want ci_red on unit at its url, corpus named stopped", res)
	}
}

// D: every failing check stopped is no verdict about the code, and the
// coordinator's at once: the sprint runs nothing again (LESSONS M11). The
// judgement names each stopped check and carries the first stopped check's
// failing lines, its log read once.
func TestLandEveryCheckStoppedIsNoVerdict(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus", "cpp"}, URL: verdictJob1}
	fs := []forge.Failure{{Name: "corpus", URL: verdictJob1, Stop: "killed (exit code 137)"}, {Name: "cpp", URL: verdictJob2, Stop: "killed (exit code 137)"}, {Name: "lua", URL: verdictJob3, Stop: "cancelled"}}
	res, ff := verdictPass_internal(t, r, fs, nil)
	calls := ff.calls_internal()
	if res.Outcome != LandOutcome("ci_no_verdict") || slices.Contains(calls, "Rerun") || len(res.FailingCI) != 0 {
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
	if err != nil || res.Outcome != LandOutcome("ci_no_verdict") || len(res.FailingLines) == 0 || slices.Contains(ff.calls_internal(), "Rerun") || gf.OriginRef("v2") != root {
		t.Fatalf("%+v err=%v, want ci_no_verdict with its lines and nothing run again", res, err)
	}
	ff.SetFailures("o/r", c1, []forge.Failure{{Name: "lua", URL: verdictJob3}})
	if res, _ = LandPass(context.Background(), gf, ff, one_internal(c1)); res.Outcome != OutcomeCIFault {
		t.Fatalf("%+v, want ci_fault", res)
	}
}
