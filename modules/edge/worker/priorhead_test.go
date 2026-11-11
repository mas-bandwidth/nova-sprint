package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// An ok at the prior head (runner.go rebaseAndTestStep_internal),
// 2026-11-11. The worker failed every attempt whose harness said ok and
// committed nothing over its prior head ("the store refuses the prior
// head"). The card machine refuses that head only when an attempt already
// finished at it (card.finished, tla/CardMachine.tla FinishedHeads). So an
// attempt that pushed complete work and then died of something else left
// every later attempt nothing to do and nothing but a fail: two of them and
// the card was held ("failed 2 top-rank attempts running with no new
// head"). An attempt that is no fix is now verified at the prior head, the
// stream merged in and the done-whens run there, and finishes at it.

// priorStep_internal runs the stream merge and done-when step of rec, a
// harvested work job whose harness said ok at its prior head
// (testCommitSHA, on origin), and returns the record it left.
func priorStep_internal(t *testing.T, g *fakeGit, wall ReserveWall, rec JobRecord) JobRecord {
	t.Helper()
	r, err := NewRunner(Runner{Now: (&gateClock_internal{sec: 1600}).now, Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: wall, Git: g, DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	rec.Consumer, rec.Kind, rec.Row, rec.Repo, rec.Forge, rec.Gen = "p1.w3", "work", "vision.ds", "o/r", "github", 2
	rec.StreamRef, rec.Branch, rec.BaseSHA, rec.StartedAt, rec.DeadlineAt = "sprint/s1", "sprint/s1/p1", testPriorSHA, 1000, 6400
	rec.PriorHead, rec.StartTip, rec.PushedHead = testCommitSHA, testCommitSHA, testCommitSHA
	rec.DoneWhen, rec.Outcome = []string{"go test ./..."}, Outcome{Result: "ok", Head: testCommitSHA}
	g.originHeads[rec.Branch] = testCommitSHA // an earlier attempt pushed it
	jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.RootDir, RemotesDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, RemotesDir, rec.Consumer), []byte(recordRemote_internal(rec)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.rebaseAndTestStep_internal(context.Background(), jobDir, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// The attempt before pushed complete work and died; this one found nothing
// left to do. The prior head is checked out in the worker's clone, the
// stream merged in (it has not moved), the done-whens run in the job's
// tree, which is at that head already, and the job is ok at it: nothing to
// apply, nothing to push.
func TestOkAtAnUnfinishedPriorHeadIsVerifiedAndFinishesThere(t *testing.T) {
	t.Parallel()
	g, wall := newFakeGit_internal(), &gateWall_internal{}
	rec := priorStep_internal(t, g, wall, JobRecord{})
	if rec.Outcome.Result != "ok" || rec.Outcome.Head != testCommitSHA || rec.State != JobRebased {
		t.Fatalf("outcome %+v at %s, want ok at the prior head: its done-whens pass there", rec.Outcome, rec.State)
	}
	if len(g.checkouts) != 1 || g.checkouts[0] != testCommitSHA {
		t.Errorf("checkouts in the worker's clone %v, want the prior head: the seal found no commits, so nothing is checked out there to merge the stream into", g.checkouts)
	}
	if len(wall.tests) != 1 {
		t.Errorf("%d done-whens ran, want 1: the prior head is verified, never taken on the harness's word", len(wall.tests))
	}
	if len(wall.steps) != 0 {
		t.Errorf("%d wall steps ran: there is no merged head to apply, the job's tree is at the prior head", len(wall.steps))
	}
	if p := g.pushes(); len(p) != 0 {
		t.Errorf("pushes %v: the prior head is on origin already", p)
	}
}

// Verified means its done-whens decide: red at the prior head is the
// tests' fail with what failed, never "committed nothing".
func TestOkAtAnUnfinishedPriorHeadWithRedDoneWhensIsTheirFail(t *testing.T) {
	t.Parallel()
	wall := &gateWall_internal{}
	wall.run = func(string, time.Duration) (WallRunResult, error) {
		return WallRunResult{ExitCode: 1, Output: []byte("--- FAIL: TestX (0.00s)\nFAIL")}, nil
	}
	rec := priorStep_internal(t, newFakeGit_internal(), wall, JobRecord{})
	if rec.Outcome.Result != "fail" || !strings.Contains(rec.Outcome.Reason, "tests red (go test ./...)") || !strings.Contains(rec.Outcome.Reason, "--- FAIL: TestX") {
		t.Fatalf("outcome %+v, want the done-when's fail", rec.Outcome)
	}
}

// The stream moved since the prior head: the merge is a new head, applied
// to the job's tree for the done-whens and pushed by lease from the prior
// head.
func TestOkAtAPriorHeadTheStreamMovedPastPushesTheMerge(t *testing.T) {
	t.Parallel()
	g, wall := newFakeGit_internal(), &gateWall_internal{}
	g.rebasedSHA = testRebSHA
	rec := priorStep_internal(t, g, wall, JobRecord{})
	if rec.Outcome.Result != "ok" || rec.Outcome.Head != testRebSHA || rec.PushedHead != testRebSHA {
		t.Fatalf("outcome %+v pushed %s, want ok at the merge %s", rec.Outcome, rec.PushedHead, testRebSHA)
	}
	if len(wall.steps) != 1 || len(wall.tests) != 1 {
		t.Errorf("%d apply steps and %d done-whens, want one of each at the merged head", len(wall.steps), len(wall.tests))
	}
	if p := g.pushes(); len(p) != 1 || p[0] != (fakePush{branch: "sprint/s1/p1", sha: testRebSHA, expect: testCommitSHA}) {
		t.Errorf("pushes %v, want the merge by lease from the prior head", p)
	}
}

// A fix (the deal carries a finding) that adds nothing to its prior head
// is the fail it was: that head was finished and sent back, and neither it
// nor a merge of the stream into it does anything about the finding. No
// checkout, no merge, no done-when.
func TestOkAtThePriorHeadOfAFixIsAFailWithNoHead(t *testing.T) {
	t.Parallel()
	g, wall := newFakeGit_internal(), &gateWall_internal{}
	g.rebasedSHA = testRebSHA // a merge would be a new head, and no answer to the finding
	rec := priorStep_internal(t, g, wall, JobRecord{Fix: true})
	if rec.Outcome.Result != "fail" || rec.Outcome.Head != "" || !strings.Contains(rec.Outcome.Reason, "committed nothing over the prior head") {
		t.Fatalf("outcome %+v, want fail with no head: a fix that committed nothing over the prior head", rec.Outcome)
	}
	if len(g.checkouts)+len(wall.tests)+len(wall.steps)+len(g.pushes()) != 0 {
		t.Errorf("a fix with nothing new ran %d checkouts, %d done-whens, %d steps, %d pushes; want none", len(g.checkouts), len(wall.tests), len(wall.steps), len(g.pushes()))
	}
}

// The record says whether its deal is a fix: the receipt's finding.
func TestAFixDealIsMarkedOnTheJobRecord(t *testing.T) {
	t.Parallel()
	at := time.Unix(1000, 0)
	for finding, want := range map[string]bool{"": false, "the table is not sorted (tables.go:41)": true} {
		_, rec := buildJob_internal(NextResponse{Row: "vision.ds", Epoch: 3}, TakeInfo{Consumer: "p1.w3", Kind: "work", Finding: finding, Deadline: 900}, t.TempDir(), "", at)
		if rec.Fix != want {
			t.Errorf("finding %q: the record's Fix is %v, want %v", finding, rec.Fix, want)
		}
	}
}

// refusingSprint_internal refuses every ok report in the card machine's
// words for a finished prior head, and takes any other.
type refusingSprint_internal struct{ cr2Sprint }

func (s *refusingSprint_internal) Report(ctx context.Context, req ReportRequest) (store.Receipt, error) {
	if req.Result != "ok" {
		return s.cr2Sprint.Report(ctx, req)
	}
	s.mu.Lock()
	s.reports = append(s.reports, req)
	s.mu.Unlock()
	why := "head " + req.Head + " is the prior head; nothing new was pushed"
	return store.Receipt{Reason: why}, fmt.Errorf("%w: nova-sprint report: exit 1: %s", ErrRefused, why)
}

// Only the card machine knows whether an attempt finished at the prior
// head (a card held in review and unheld is dealt again with no finding).
// When the sprint refuses an ok at the prior head, the attempt is the fail
// it was before: sent once more as that, under the same result id, never
// resent as ok until it is set aside. An ok at a new head that is refused
// is not touched.
func TestARefusedOkAtThePriorHeadIsReportedAsTheFail(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		head string
		want []string
	}{
		"at the prior head": {head: testCommitSHA, want: []string{"ok " + testCommitSHA, "fail "}},
		"at a new head":     {head: testRebSHA, want: []string{"ok " + testRebSHA}},
	} {
		sp := &refusingSprint_internal{}
		r, _ := warnRunner_internal(t, sp, &cr2Wall{})
		rec := JobRecord{Epoch: 3, Consumer: "p1.w3", Kind: "work", Row: "vision.ds", Gen: 2, BaseSHA: testPriorSHA, PriorHead: testCommitSHA, State: JobRebased, Outcome: Outcome{Result: "ok", Head: tc.head}}
		jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
		if err := WriteJobRecord(jobDir, rec); err != nil {
			t.Fatal(err)
		}
		err := r.sendReportAndClean_internal(context.Background(), jobDir, rec)
		var got []string
		for _, req := range sp.reported() {
			got = append(got, req.Result+" "+req.Head)
			if req.ResultID != resultID_internal(3, rec.Consumer, 2) {
				t.Errorf("%s: a report went under result id %q, want the record's one id", name, req.ResultID)
			}
			if req.Result == "fail" && !strings.Contains(req.Reason, "committed nothing over the prior head") {
				t.Errorf("%s: the fail's reason is %q", name, req.Reason)
			}
		}
		if strings.Join(got, "; ") != strings.Join(tc.want, "; ") {
			t.Errorf("%s: reports %q, want %q", name, got, tc.want)
		}
		if (err == nil) != (len(tc.want) == 2) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
