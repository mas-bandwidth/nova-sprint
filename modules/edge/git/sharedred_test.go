package git

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

const sharedJob = "https://github.com/o/r/actions/runs/1/job/2"

// sharedWorld is rung 1's world with the gated head c1 red on head's
// checks and the base tip root's rollup base; pr, when set, is the merged
// PR GitHub says made root, its head's checks prChecks.
type sharedWorld struct {
	head, base forge.CheckRollup
	pr         *forge.BasePR
	prChecks   forge.CheckRollup
	prFails    []forge.Failure
	headFails  []forge.Failure
	off        bool
}

// pass runs one LandPass of c1 in w; landed is whether base moved to c1.
func (w sharedWorld) pass(t *testing.T) (LandResult, bool) {
	t.Helper()
	gf, ff, root, c1, _ := landWorld_internal()
	w.head.URL = sharedJob
	ff.SetCheck("o/r", c1, w.head)
	ff.SetCheck("o/r", root, w.base)
	if w.headFails != nil {
		ff.SetFailures("o/r", c1, w.headFails)
	}
	if w.pr != nil {
		ff.SetBasePR("o/r", root, *w.pr)
		ff.SetCheck("o/r", w.pr.HeadSHA, w.prChecks)
		if w.prFails != nil {
			ff.SetFailures("o/r", w.pr.HeadSHA, w.prFails)
		}
	}
	req := one_internal(c1)
	req.LandOnSharedRed = !w.off
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	return res, gf.OriginRef("v2") == c1
}

func red(names ...string) forge.CheckRollup {
	return forge.CheckRollup{State: forge.CheckRed, Failing: names, Ran: append([]string{"lint"}, names...)}
}

// A red head whose every failing check is red on the base tip too lands
// (land_on_shared_red, Glenn 2026-11-15: "heavy suite failure on main is
// fine"), with the shared checks and the base tip it was read red at.
func TestLandOnTheBasesOwnReds(t *testing.T) {
	t.Parallel()
	res, landed := sharedWorld{head: red("corpus", "heavy"), base: red("heavy", "corpus", "other")}.pass(t)
	if !landed || res.Outcome != OutcomeLanded || !slices.Equal(res.SharedRed, []string{"corpus", "heavy"}) || !strings.Contains(res.SharedProof, "corpus, heavy red on the base tip "+res.BaseTip) {
		t.Fatalf("%+v landed=%v, want landed on the base's own reds", res, landed)
	}
}

// A red the base does not share is the card's own: never landed, the
// coordinator's judgement with why it was not landed.
func TestOwnRedIsNeverLanded(t *testing.T) {
	t.Parallel()
	for name, w := range map[string]sharedWorld{
		"green on the base":       {head: red("corpus", "unit"), base: red("corpus")},
		"base all green":          {head: red("unit"), base: forge.CheckRollup{State: forge.CheckGreen, Ran: []string{"unit"}}},
		"base still running":      {head: red("corpus"), base: forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}, Waiting: true, Pending: 1}},
		"base pages not all read": {head: red("unit"), base: forge.CheckRollup{State: forge.CheckGreen, RanPartial: true}},
		"switch off":              {head: red("corpus"), base: red("corpus"), off: true},
		"a check stopped on the head": {head: red("corpus", "slow"), base: red("corpus", "slow"),
			headFails: []forge.Failure{{Name: "corpus", URL: sharedJob}, {Name: "slow", URL: sharedJob, Stop: "killed (exit code 137)"}}},
		"a failing check with no name": {head: red("corpus"), base: red("corpus"),
			headFails: []forge.Failure{{Name: "corpus", URL: sharedJob}, {URL: sharedJob}}},
	} {
		res, landed := w.pass(t)
		if landed || res.Outcome != OutcomeRed || len(res.SharedRed) != 0 {
			t.Fatalf("%s: %+v landed=%v, want ci_red, never landed", name, res, landed)
		}
		if !w.off && !strings.Contains(res.Reason, "not landed on the base's reds") {
			t.Errorf("%s: the reason says nothing of why it was not landed: %s", name, res.Reason)
		}
		// proven the card's own only where the failing check completed on
		// the base tip, read complete, and was not red there
		if want := name == "base all green"; res.OwnRed != want {
			t.Errorf("%s: OwnRed = %v, want %v", name, res.OwnRed, want)
		}
	}
}

// A check that runs only on PRs (no run of it on the base tip) is red on
// the base when the PR that merged the base tip failed it on a head with
// the tip's tree: then it lands, naming that PR.
func TestPROnlyCheckRedOnTheBasesOwnPR(t *testing.T) {
	t.Parallel()
	prHead := strings.Repeat("d", 40)
	pr := &forge.BasePR{Number: 7, URL: "https://github.com/o/r/pull/7", HeadSHA: prHead, SameTree: true}
	w := sharedWorld{head: red("corpus", "lint-fast"), base: forge.CheckRollup{State: forge.CheckRed, Failing: []string{"corpus"}, Ran: []string{"corpus", "unit"}},
		pr: pr, prChecks: forge.CheckRollup{State: forge.CheckRed, Failing: []string{"lint-fast"}, URL: sharedJob}}
	w.head.Ran = []string{"corpus", "lint-fast"}
	res, landed := w.pass(t)
	if !landed || !slices.Equal(res.SharedRed, []string{"corpus", "lint-fast"}) || !strings.Contains(res.SharedProof, "lint-fast red on https://github.com/o/r/pull/7 at "+prHead) {
		t.Fatalf("%+v landed=%v, want landed with lint-fast read red on PR 7", res, landed)
	}
	for name, change := range map[string]func(*sharedWorld){
		"no PR merged the base tip":     func(w *sharedWorld) { w.pr = nil },
		"the PR's head is another tree": func(w *sharedWorld) { p := *pr; p.SameTree = false; w.pr = &p },
		"the PR's head is green":        func(w *sharedWorld) { w.prChecks = forge.CheckRollup{State: forge.CheckGreen} },
		"the PR's head red elsewhere": func(w *sharedWorld) {
			w.prChecks = forge.CheckRollup{State: forge.CheckRed, Failing: []string{"other"}}
		},
		"the PR's checks still run": func(w *sharedWorld) {
			w.prChecks = forge.CheckRollup{State: forge.CheckRed, Failing: []string{"lint-fast"}, Waiting: true}
		},
		"lint-fast stopped on the PR": func(w *sharedWorld) {
			w.prFails = []forge.Failure{{Name: "lint-fast", URL: sharedJob, Stop: "cancelled"}}
		},
	} {
		v := w
		change(&v)
		res, landed := v.pass(t)
		if landed || res.Outcome != OutcomeRed || !strings.Contains(res.Reason, "not landed on the base's reds") {
			t.Fatalf("%s: %+v landed=%v, want ci_red with why", name, res, landed)
		}
	}
}

// A rate-limited read while weighing the base's reds is forge_rested
// (asked again), never a judgement.
func TestSharedRedReadRestedIsAskedAgain(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"lint-fast"}, URL: sharedJob})
	ff.SetCheck("o/r", root, forge.CheckRollup{State: forge.CheckGreen, Ran: []string{"unit"}})
	rest := &forge.FaultError{Kind: forge.FaultRateLimit, Identity: forge.HostLogin, Reason: "HTTP 403: API rate limit exceeded"}
	ff.OnCall = func(op string) {
		if op == "BasePR" {
			ff.ChecksErr = rest
		}
	}
	ff.SetBasePR("o/r", root, forge.BasePR{Number: 7, HeadSHA: strings.Repeat("d", 40), SameTree: true})
	req := one_internal(c1)
	req.LandOnSharedRed = true
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || res.Outcome != OutcomeRested || gf.OriginRef("v2") != root {
		t.Fatalf("%+v err=%v, want forge_rested and nothing landed", res, err)
	}
}
