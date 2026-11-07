package git

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// redPass_internal is one rung-1 pass with the gated head's rollup r and
// the forge's split (nil: the fake's default, every failing check new).
func redPass_internal(t *testing.T, r forge.CheckRollup, split *forge.RedSplit, splitErr error) (LandResult, *fakeForge_internal) {
	t.Helper()
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, r)
	ff.Split, ff.SplitErr = split, splitErr
	ff.AuthorsOf = []string{"Aurora <aurora@example.invalid> (aurora-x)"}
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || gf.OriginRef("v2") != root {
		t.Fatalf("pass %+v err=%v: red never lands", res, err)
	}
	return res, ff
}

var (
	inheritedCorpus = forge.WorkflowRed{Workflow: "hostile-corpus", Basis: "the merge base cccccccccccc", Jobs: []forge.FailedJob{{Name: "corpus"}}}
	newGates        = forge.WorkflowRed{Workflow: "Gates", Basis: "the merge base cccccccccccc", Jobs: []forge.FailedJob{{Name: "Linux Debug", Runner: "bench-b", Lines: []string{"--- FAIL: TestX"}}}}
)

// Directive 59: red that is all red on the base too, the run ended, is
// ci_red_inherited: still a judgement (base unmoved), its facts the
// inherited workflows and the merge base.
func TestLandInheritedOnlyIsAGlanceJudgement(t *testing.T) {
	t.Parallel()
	split := &forge.RedSplit{MergeBase: "cccccccccccccccccccccccccccccccccccccccc", Inherited: []forge.WorkflowRed{inheritedCorpus}}
	res, _ := redPass_internal(t, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}}, split, nil)
	if res.Outcome != OutcomeInherited || len(res.NewRed) != 0 || len(res.Inherited) != 1 || res.MergeBase != split.MergeBase ||
		!strings.HasPrefix(res.Reason, "only inherited red: hostile-corpus (the merge base") {
		t.Fatalf("%+v, want ci_red_inherited naming hostile-corpus", res)
	}
}

// Directive 76: while the run goes on, red that is all inherited waits
// (pending); red that is new escalates at once.
func TestLandWaitsForTheRunUnlessRedIsNew(t *testing.T) {
	t.Parallel()
	waiting := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}, Waiting: true}
	res, _ := redPass_internal(t, waiting, &forge.RedSplit{Inherited: []forge.WorkflowRed{inheritedCorpus}}, nil)
	if res.Outcome != OutcomePending || !strings.Contains(res.Reason, "waiting for the run") {
		t.Fatalf("inherited while running: %+v, want ci_pending waiting for the run", res)
	}
	waiting.Failing = []string{"corpus", "Linux Debug"}
	res, _ = redPass_internal(t, waiting, &forge.RedSplit{Inherited: []forge.WorkflowRed{inheritedCorpus}, New: []forge.WorkflowRed{newGates}}, nil)
	if res.Outcome != OutcomeRed || len(res.NewRed) != 1 || res.NewRed[0].Jobs[0].Runner != "bench-b" || !strings.HasPrefix(res.Reason, "new red: Gates") {
		t.Fatalf("new while running: %+v, want ci_red at once with the new job's runner", res)
	}
}

// A failing check that is no failed job of any workflow (a status or
// another app's check) is new red: nothing on the base compares with it.
// An agreement check among them names the PR's commit authors (63).
func TestLandAgreementCheckNamesTheAuthors(t *testing.T) {
	t.Parallel()
	r := forge.CheckRollup{State: forge.CheckRed, Failing: []string{"Contributor Assignment Agreement", "corpus"}, URL: "https://cla.example.invalid/o/r/2830"}
	res, ff := redPass_internal(t, r, &forge.RedSplit{Inherited: []forge.WorkflowRed{inheritedCorpus}}, nil)
	if res.Outcome != OutcomeRed || len(res.NewRed) != 1 || res.NewRed[0].Workflow != "Contributor Assignment Agreement" ||
		res.NewRed[0].Jobs[0].URL != r.URL || !slices.Equal(res.Unsigned, ff.AuthorsOf) {
		t.Fatalf("%+v, want the agreement as new red with the PR's authors", res)
	}
	plain, ff := redPass_internal(t, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}}, &forge.RedSplit{Inherited: []forge.WorkflowRed{inheritedCorpus}}, nil)
	if len(plain.Unsigned) != 0 || slices.Contains(ff.calls_internal(), "Authors") {
		t.Fatalf("no agreement check, yet authors were read: %+v", plain)
	}
}

// A split that cannot be read: while the run goes on the pass waits; once
// it ended the red escalates as before, with the first failing lines and
// the reason saying it was not compared.
func TestLandUncomparedRed(t *testing.T) {
	t.Parallel()
	boom := errors.New("HTTP 502 from compare")
	res, _ := redPass_internal(t, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, Waiting: true}, nil, boom)
	if res.Outcome != OutcomePending {
		t.Fatalf("uncompared while running: %+v, want pending", res)
	}
	res, _ = redPass_internal(t, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: "https://github.com/o/r/actions/runs/1/job/2"}, nil, boom)
	if res.Outcome != OutcomeRed || len(res.FailingLines) == 0 || !strings.Contains(res.Reason, "not compared with the base (HTTP 502") {
		t.Fatalf("uncompared after the run: %+v, want ci_red with lines and the cause", res)
	}
}
