package git

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// The base comparison (LANDING.md "A red base"): a red gate's own failures,
// each compared job by job with the same workflow's run, started by the
// same event, on the base's probe PR.

const (
	redHeadRun = 11 // the gate's pull_request run of workflow 7 ("CI")
	redBaseRun = 21 // the probe's run of workflow 7
	redJobA    = "https://github.com/o/r/actions/runs/11/job/101"
	redJobB    = "https://github.com/o/r/actions/runs/11/job/102"
)

// redWorld_internal is landWorld_internal with c1's checks red on jobs a and
// b of one pull_request run, and the probe of the base tip's runs ready to
// be set (probe: its sha).
func redWorld_internal(t *testing.T) (gf *Fake, ff *fakeForge_internal, root, c1, probe string) {
	t.Helper()
	gf, ff, root, c1, _ = landWorld_internal()
	ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"a", "b"}, URL: redJobA})
	ff.SetFailures("o/r", c1, []forge.Failure{{Name: "a", URL: redJobA}, {Name: "b", URL: redJobB}})
	ff.SetRun(c1, forge.Run{ID: redHeadRun, Workflow: 7, Name: "CI", Event: "pull_request", Status: "completed", Conclusion: "failure"},
		forge.Job{Name: "a", Status: "completed", Conclusion: "failure"}, forge.Job{Name: "b", Status: "completed", Conclusion: "failure"})
	probe, err := gf.Probe(context.Background(), "", root)
	if err != nil {
		t.Fatal(err)
	}
	return gf, ff, root, c1, probe
}

// baseJobs_internal sets the probe's run of workflow 7 (status and its
// jobs' conclusions by name; "" a job still running).
func baseJobs_internal(ff *fakeForge_internal, probe, status string, jobs map[string]string) {
	var js []forge.Job
	for _, n := range []string{"a", "b"} {
		if c, ok := jobs[n]; ok {
			js = append(js, forge.Job{Name: n, Status: map[bool]string{true: "in_progress", false: "completed"}[c == ""], Conclusion: c})
		}
	}
	ff.SetRun(probe, forge.Run{ID: redBaseRun, Workflow: 7, Name: "CI", Event: "pull_request", Status: status}, js...)
}

// Every own failure red on the base too: the card lands past them, and
// the result names each job, its workflow and both runs. The probe branch
// holds the probe and its PR into the base is opened once.
func TestLandRedOnlyWhereTheBaseIsRedLands(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, probe := redWorld_internal(t)
	baseJobs_internal(ff, probe, "completed", map[string]string{"a": "failure", "b": "failure"})
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	want := []SharedRed{{Base: "v2", Workflow: "CI", Job: "a", HeadRun: redHeadRun, BaseRun: redBaseRun}, {Base: "v2", Workflow: "CI", Job: "b", HeadRun: redHeadRun, BaseRun: redBaseRun}}
	if err != nil || res.Outcome != OutcomeLanded || !slices.Equal(res.BaseRed, want) || gf.OriginRef("v2") == root {
		t.Fatalf("pass %+v err=%v, want landed past a and b", res, err)
	}
	if gf.OriginRef(ProbeBranch("v2")) != probe {
		t.Fatalf("probe branch at %s, want %s", gf.OriginRef(ProbeBranch("v2")), probe)
	}
	if i := slices.IndexFunc(ff.prs, func(p forge.PRInfo) bool { return p.HeadRef == ProbeBranch("v2") && p.BaseRef == "v2" }); i < 0 {
		t.Fatalf("no probe PR among %+v", ff.prs)
	}
	if slices.Contains(ff.calls_internal(), "FailedLines") {
		t.Fatal("a landing past the base's red read a failing log")
	}
}

// A job green on the base is the card's: the landing stops as before
// (ci_red, one judgement), naming it, with its own failing lines; the job
// red on both sides is named the base's beside it.
func TestLandRedGreenOnTheBaseStops(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, probe := redWorld_internal(t)
	baseJobs_internal(ff, probe, "completed", map[string]string{"a": "failure", "b": "success"})
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeRed || gf.OriginRef("v2") != root {
		t.Fatalf("pass %+v err=%v, want ci_red, nothing landed", res, err)
	}
	if len(res.BaseRed) != 1 || res.BaseRed[0].Job != "a" || len(res.NotBase) != 1 || !strings.Contains(res.NotBase[0], "b: red in run 11 here, green on the base in run 21: the card's") {
		t.Fatalf("base red %+v, not base %q", res.BaseRed, res.NotBase)
	}
	if res.CIURL != redJobB || len(res.FailingLines) == 0 {
		t.Fatalf("lines %v at %s, want b's", res.FailingLines, res.CIURL)
	}
}

// The base's run still going: base_pending, the gate kept (read again,
// never rebuilt) until the request's wait; no probe pushed twice.
func TestLandRedWaitsForTheBasesRun(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, probe := redWorld_internal(t)
	baseJobs_internal(ff, probe, "in_progress", map[string]string{"a": "failure", "b": ""})
	req := one_internal(c1)
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || res.Outcome != OutcomeBaseWait || res.Kept() != res.Gate || res.Gate.SHA != c1 || !strings.Contains(res.Reason, "b: still running in the base's run 21") {
		t.Fatalf("pass %+v err=%v, want base_pending, the gate kept", res, err)
	}
	// Before the probe's workflow even starts, it waits too.
	gf2, ff2, _, c1b, _ := redWorld_internal(t)
	if res2, _ := LandPass(context.Background(), gf2, ff2, one_internal(c1b)); res2.Outcome != OutcomeBaseWait || !strings.Contains(res2.Reason, "workflow CI has not started") {
		t.Fatalf("no probe run yet: %+v", res2)
	}
	baseJobs_internal(ff, probe, "completed", map[string]string{"a": "failure", "b": "failure"})
	req.Gate = res.Kept()
	if res, err = LandPass(context.Background(), gf, ff, req); err != nil || res.Outcome != OutcomeLanded || gf.OriginRef("v2") == root {
		t.Fatalf("pass 2 %+v err=%v, want landed once the base's run is red too", res, err)
	}
}

// Each failure not compared keeps today's behaviour (ci_red), naming why:
// a check that is no Actions job, a run started by another event, a
// workflow the probe never ran, a job the base's run lacks, a base job
// that ended with no verdict, the gate's runs unread.
func TestLandRedNotComparedStopsNamingWhy(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		set  func(ff *fakeForge_internal, c1, probe string)
		want string
	}{
		"no Actions job": {func(ff *fakeForge_internal, c1, probe string) {
			ff.SetFailures("o/r", c1, []forge.Failure{{Name: "a", URL: "https://example.invalid/status/1"}})
		}, "a: not compared with the base: it is no job of an Actions run of the gated sha"},
		"push event": {func(ff *fakeForge_internal, c1, probe string) {
			ff.SetRun(c1, forge.Run{ID: redHeadRun, Workflow: 7, Name: "CI", Event: "push", Status: "completed"})
		}, "its run 11 was started by push"},
		"never ran on the base": {func(ff *fakeForge_internal, c1, probe string) {
			ff.SetRun(probe, forge.Run{ID: redBaseRun, Workflow: 8, Name: "CAA", Event: "pull_request", Status: "completed", Conclusion: "success"})
		}, "ran no pull_request run of workflow CI"},
		"no such job": {func(ff *fakeForge_internal, c1, probe string) {
			baseJobs_internal(ff, probe, "completed", map[string]string{"b": "failure"})
		}, "the base's run 21 of CI has no job of that name"},
		"cancelled on the base": {func(ff *fakeForge_internal, c1, probe string) {
			baseJobs_internal(ff, probe, "completed", map[string]string{"a": "cancelled", "b": "failure"})
		}, "on the base it ended cancelled in run 21, no verdict"},
		"runs unread": {func(ff *fakeForge_internal, c1, probe string) {
			ff.RunsErr = errors.New("HTTP 502")
		}, "the runs of " /* + c1 */},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gf, ff, root, c1, probe := redWorld_internal(t)
			baseJobs_internal(ff, probe, "completed", map[string]string{"a": "failure", "b": "failure"})
			c.set(ff, c1, probe)
			res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
			if err != nil || res.Outcome != OutcomeRed || gf.OriginRef("v2") != root || !strings.Contains(strings.Join(res.NotBase, "; "), c.want) {
				t.Fatalf("pass %+v err=%v, want ci_red naming %q", res, err, c.want)
			}
		})
	}
}

// A probe that cannot be pushed, or whose PR cannot be opened, is not
// compared: ci_red naming why.
func TestLandRedProbeRefusedStops(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, probe := redWorld_internal(t)
	baseJobs_internal(ff, probe, "completed", map[string]string{"a": "failure", "b": "failure"})
	gf.SetOriginRef(ProbeBranch("v2"), root) // what the pass reads as the lease
	g := &leaseLostGit_internal{Fake: gf}
	res, err := LandPass(context.Background(), g, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeRed || !strings.Contains(strings.Join(res.NotBase, "; "), "the probe branch sprint-base/v2 could not be pushed") {
		t.Fatalf("pass %+v err=%v, want ci_red naming the refused push", res, err)
	}
	gf2, ff2, _, c1b, _ := redWorld_internal(t)
	ff2.OpenErr = errors.New("HTTP 422: no commits between")
	ff2.Mergeable = ""
	ff2.prs = []forge.PRInfo{{Number: 1, HeadRef: "sprint/s1/c1", BaseRef: "v2", State: "OPEN"}}
	if res, _ = LandPass(context.Background(), gf2, ff2, one_internal(c1b)); res.Outcome != OutcomeRed || !strings.Contains(strings.Join(res.NotBase, "; "), "the base's probe PR or its runs could not be read") {
		t.Fatalf("pass %+v, want ci_red naming the unopened probe PR", res)
	}
}

// leaseLostGit_internal is the fake whose probe branch push loses its
// lease (another pass moved it).
type leaseLostGit_internal struct{ *Fake }

func (g *leaseLostGit_internal) Push(ctx context.Context, dir, sha, ref, old string) error {
	if strings.HasSuffix(ref, ProbeBranch("v2")) {
		return &PushRefusedError{Ref: ref, SHA: sha, Reason: "stale lease"}
	}
	return g.Fake.Push(ctx, dir, sha, ref, old)
}

// A rate-limited forge while comparing is no fact: forge_rested, the gate
// kept, asked again.
func TestLandRedRestsOnARateLimit(t *testing.T) {
	t.Parallel()
	gf, ff, _, c1, _ := redWorld_internal(t)
	ff.RunsErr = &forge.FaultError{Kind: forge.FaultRateLimit, Identity: forge.HostLogin, Reason: "HTTP 403: API rate limit exceeded"}
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeRested || res.Kept() != res.Gate {
		t.Fatalf("pass %+v err=%v, want forge_rested with the gate kept", res, err)
	}
}

// Own failures all the base's, others stopped: no verdict about the code,
// the coordinator's at once, never landed.
func TestLandRedTheBasesAndStoppedIsNoVerdict(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, probe := redWorld_internal(t)
	ff.SetFailures("o/r", c1, []forge.Failure{{Name: "a", URL: redJobA}, {Name: "b", URL: redJobB, Stop: "killed (exit code 137)"}})
	baseJobs_internal(ff, probe, "completed", map[string]string{"a": "failure", "b": "success"})
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeNoVerdict || gf.OriginRef("v2") != root || !strings.Contains(res.Reason, "b killed (exit code 137)") {
		t.Fatalf("pass %+v err=%v, want ci_no_verdict, nothing landed", res, err)
	}
}

// The base's run is the newest that completed other than cancelled, else
// the newest still going, else the newest; runs of another workflow or
// event never count.
func TestBaseRunPicksTheVerdict(t *testing.T) {
	t.Parallel()
	head := forge.Run{ID: 1, Workflow: 7, Event: "pull_request"}
	runs := []forge.Run{
		{ID: 30, Workflow: 7, Event: "pull_request", Status: "completed", Conclusion: "cancelled"},
		{ID: 20, Workflow: 7, Event: "pull_request", Status: "completed", Conclusion: "failure"},
		{ID: 25, Workflow: 7, Event: "pull_request", Status: "in_progress"},
		{ID: 40, Workflow: 7, Event: "push", Status: "completed", Conclusion: "success"},
		{ID: 50, Workflow: 8, Event: "pull_request", Status: "completed", Conclusion: "success"},
	}
	for _, c := range []struct {
		runs []forge.Run
		want int64
	}{{runs, 20}, {runs[:1:1], 30}, {append([]forge.Run{runs[0]}, runs[2]), 25}, {runs[3:], 0}} {
		if r, ok := baseRun_internal(c.runs, head); r.ID != c.want || ok != (c.want != 0) {
			t.Fatalf("baseRun(%v) = %d %v, want %d", c.runs, r.ID, ok, c.want)
		}
	}
}
