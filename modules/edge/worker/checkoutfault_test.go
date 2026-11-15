package worker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// checkoutRunner_internal is streamGateRunner_internal's worker on a wall
// whose step named failStep exits 1 (stepWall_internal), its gates green.
func checkoutRunner_internal(t *testing.T, failStep string) (*Runner, *stepWall_internal) {
	t.Helper()
	wall := &stepWall_internal{fakeWall: &fakeWall{}, steps: &[][]string{}, failStep: failStep}
	return streamGateRunner_internal(t, wall), wall
}

// A head the step cannot put in the job's tree for its gates is a fault:
// the gates never ran, the head is not judged, and dealt again the card
// spends no bounce (2026-11-15: a fail here spent nine attempts of one
// card on the AWS benches). The apply of a new head, the reset at a prior
// head and the export from the worker's clone each are; no gate runs after.
func TestAHeadNotCheckedOutForTheGatesIsAFault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, failStep string
		exportErr      error
		prior          bool
		why            string
	}{
		{name: "the apply of a new head", failStep: "apply", why: "exit 1: error: unable to unlink old 'go.sum'"},
		{name: "the reset at the prior head", failStep: "reset", prior: true, why: "exit 1: error: unable to unlink old 'go.sum'"},
		{name: "the export from the worker's clone", exportErr: errors.New("bundle: disk full"), why: "bundle: disk full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, wall := checkoutRunner_internal(t, tc.failStep)
			streamGateGit_internal(r).exportErr = tc.exportErr
			var rec JobRecord
			if tc.prior {
				rec = checkoutAtPrior_internal(t, r)
			} else {
				rec = streamGateStep_internal(t, r, "p1.w1", []string{"go test ./..."}, []string{genGate})
			}
			if rec.Outcome.Result != "fault" || !strings.HasPrefix(rec.Outcome.Reason, "the rebased head could not be checked out for the done-when tests: ") || !strings.Contains(rec.Outcome.Reason, tc.why) {
				t.Fatalf("outcome %+v, want the checkout's fault saying %q", rec.Outcome, tc.why)
			}
			if rec.Outcome.Head != rec.PushedHead || rec.State == JobRebased {
				t.Fatalf("outcome %+v at %s: want it at the pushed head, the step not ended rebased", rec.Outcome, rec.State)
			}
			if len(wall.testCalls) != 0 {
				t.Fatalf("gates ran after the checkout failed: %v", wall.testCalls)
			}
		})
	}
}

// The refusal: a checkout that goes through runs the gates, and the head
// green is ok.
func TestAHeadCheckedOutRunsItsGates(t *testing.T) {
	t.Parallel()
	r, wall := checkoutRunner_internal(t, "")
	rec := streamGateStep_internal(t, r, "p1.w1", []string{"go test ./..."}, nil)
	if rec.Outcome.Result != "ok" || !slices.Equal(wall.testCalls, []string{"go test ./..."}) {
		t.Fatalf("outcome %+v, gates %v: want ok, the done-when run once", rec.Outcome, wall.testCalls)
	}
	applied := false
	for _, s := range *wall.steps {
		applied = applied || len(s) > 3 && s[3] == "apply"
	}
	if !applied {
		t.Fatalf("steps %v: the head was not applied before its gates", *wall.steps)
	}
}

// checkoutAtPrior_internal runs the stream-and-test step of a job that
// committed nothing over an unfinished prior head (rebaseAndTestStep_internal
// verifies it there: the tree reset to it), on r.
func checkoutAtPrior_internal(t *testing.T, r *Runner) JobRecord {
	t.Helper()
	rec := JobRecord{Consumer: "p1.w1", Kind: "work", Row: "vision.ds", Gen: 2, BaseSHA: testBaseSHA, PriorHead: testPriorSHA, PushedHead: testPriorSHA,
		StartedAt: 1000, DeadlineAt: 1000 + 5400, DoneWhen: []string{"go test ./..."}, Outcome: Outcome{Result: "ok", Head: testPriorSHA}}
	jobDir := r.RootDir + "/jobs/p1.w1"
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if err := r.rebaseAndTestStep_internal(context.Background(), jobDir, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}
