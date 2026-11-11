package worker

import (
	"context"
	"errors"
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
// head"). An attempt at a prior head no attempt finished at (the take
// receipt says: prior_finished) is now verified there, the stream merged in
// and the done-whens run on that head's own tree, and finishes at it.

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
	// The seal commits only what is inside PATHS, so whatever the harness
	// changed outside them (a test, a Makefile, untracked files) is still
	// in the job's tree: it is put back at the prior head, in the wall,
	// before the gate, or the gate passes on a tree that is not that head.
	if len(wall.order) != 2 || !strings.HasPrefix(wall.order[0], "step ") || wall.order[1] != "test go test ./..." {
		t.Fatalf("the wall ran %q, want the tree put back at the prior head, then the done-when", wall.order)
	}
	for _, want := range []string{"git checkout -q -f --detach", "git clean -q -f -d", testCommitSHA} {
		if !strings.Contains(wall.order[0], want) {
			t.Errorf("the step before the done-when is %q: it does not %q", wall.order[0], want)
		}
	}
	if !strings.Contains(rec.Outcome.Reason, "this attempt added nothing") {
		t.Errorf("the ok's reason is %q: a reader of this head is not told the attempt added nothing", rec.Outcome.Reason)
	}
	if p := g.pushes(); len(p) != 0 {
		t.Errorf("pushes %v: the prior head is on origin already", p)
	}
}

// The stream's gates run at a verified prior head too, after its
// done-whens and after the one step that puts the job's tree at that head.
// A stream gate red there is judged at the base in a tree of its own beside
// the job's (<job>/base, outside what that step's clean reaches), made
// after the step and removed again before the job goes on; red at the base
// too, it is the base's, and the ok says both that and that this attempt
// added nothing.
func TestStreamGatesRunAtAVerifiedPriorHeadAfterTheTreeStep(t *testing.T) {
	t.Parallel()
	g, wall := newFakeGit_internal(), &gateWall_internal{}
	g.streamTip = testPriorSHA
	wall.run = func(cmd string, _ time.Duration) (WallRunResult, error) {
		if cmd == "make gen-check" {
			return WallRunResult{ExitCode: 1, Output: []byte("FAIL")}, nil
		}
		return WallRunResult{}, nil
	}
	rec := priorStep_internal(t, g, wall, JobRecord{StreamGates: []string{"make gen-check"}})
	if rec.Outcome.Result != "ok" || rec.Outcome.Head != testCommitSHA || rec.State != JobRebased {
		t.Fatalf("outcome %+v at %s, want ok at the prior head: its stream gate is red at the base too", rec.Outcome, rec.State)
	}
	for _, want := range []string{"this attempt added nothing", "stream gate make gen-check is red at base " + testPriorSHA} {
		if !strings.Contains(rec.Outcome.Reason, want) {
			t.Errorf("the ok's reason %q does not say %q", rec.Outcome.Reason, want)
		}
	}
	base := filepath.Join("jobs", "p1.w3", BaseTreeDir)
	if len(wall.order) != 6 {
		t.Fatalf("the wall ran %q, want the tree step, the done-when, the stream gate, then the base's tree, its gate and its removal", wall.order)
	}
	for i, want := range [][]string{
		{"step ", "git checkout -q -f --detach", "git clean -q -f -d", testCommitSHA},
		{"test go test ./..."},
		{"test make gen-check"},
		{"step ", "git worktree add", testPriorSHA, base},
		{"test make gen-check"},
		{"step ", "rm -rf", "git worktree prune", base},
	} {
		for _, w := range want {
			if !strings.Contains(wall.order[i], w) {
				t.Errorf("wall command %d is %q: it does not hold %q", i, wall.order[i], w)
			}
		}
	}
	if strings.Contains(wall.order[0], base) || strings.Contains(wall.order[3], filepath.Join("repo", BaseTreeDir)) {
		t.Errorf("the tree step %q touches the base's tree, or the base's tree %q is inside the job's", wall.order[0], wall.order[3])
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

// An ok that adds nothing to a prior head an attempt already finished at
// (the receipt says so) is the fail it was: that head was read and sent
// back, and neither it nor a merge of the stream into it answers the
// finding. No checkout, no merge, no done-when, and no report the card
// machine would refuse.
func TestOkAtAFinishedPriorHeadIsAFailWithNoHead(t *testing.T) {
	t.Parallel()
	g, wall := newFakeGit_internal(), &gateWall_internal{}
	g.rebasedSHA = testRebSHA // a merge would be a new head, and no answer to the finding
	rec := priorStep_internal(t, g, wall, JobRecord{PriorFinished: true})
	if rec.Outcome.Result != "fail" || rec.Outcome.Head != "" || !strings.Contains(rec.Outcome.Reason, "committed nothing over the prior head") {
		t.Fatalf("outcome %+v, want fail with no head: nothing committed over a finished prior head", rec.Outcome)
	}
	if len(g.checkouts)+len(wall.order)+len(g.pushes()) != 0 {
		t.Errorf("it ran %d checkouts, wall commands %q, %d pushes; want none", len(g.checkouts), wall.order, len(g.pushes()))
	}
}

// The record carries the receipt's word on the prior head, and nothing of
// the finding: a deal with a finding and an unfinished prior head is
// verified too. A finding is written against a finished head, so an
// unfinished prior head was pushed after it by an attempt on that finding
// that then died, and the read that follows judges it against the finding.
func TestTheRecordCarriesTheReceiptsWordOnThePriorHead(t *testing.T) {
	t.Parallel()
	at := time.Unix(1000, 0)
	for _, info := range []TakeInfo{
		{PriorFinished: true, Finding: "the table is not sorted (tables.go:41)"},
		{PriorFinished: false, Finding: "the table is not sorted (tables.go:41)"},
		{PriorFinished: false},
	} {
		info.Consumer, info.Kind, info.Deadline = "p1.w3", "work", 900
		_, rec := buildJob_internal(NextResponse{Row: "vision.ds", Epoch: 3}, info, t.TempDir(), "", at)
		if rec.PriorFinished != info.PriorFinished {
			t.Errorf("receipt prior_finished %v, finding %q: the record's is %v", info.PriorFinished, info.Finding, rec.PriorFinished)
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

// A refused report is never rewritten: ErrRefused is also a misuse, a
// version skew and a sprint verb that was killed, and a record turned into
// a fail before a re-send would make a worker stopped mid-report fail a
// valid ok after its restart. The ok at the prior head is sent as it is,
// kept on disk as it is, and its refusal counted like any other.
func TestARefusedOkAtThePriorHeadIsNotRewritten(t *testing.T) {
	t.Parallel()
	sp := &refusingSprint_internal{}
	r, _ := warnRunner_internal(t, sp, &cr2Wall{})
	rec := JobRecord{Epoch: 3, Consumer: "p1.w3", Kind: "work", Row: "vision.ds", Gen: 2, BaseSHA: testPriorSHA, PriorHead: testCommitSHA, State: JobRebased, Outcome: Outcome{Result: "ok", Head: testCommitSHA}}
	jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if err := r.sendReportAndClean_internal(context.Background(), jobDir, rec); !errors.Is(err, ErrRefused) {
		t.Errorf("err = %v, want the sprint's refusal", err)
	}
	if got := sp.reported(); len(got) != 1 || got[0].Result != "ok" || got[0].Head != testCommitSHA {
		t.Errorf("reports %+v, want the one ok at the prior head", got)
	}
	if kept, err := ReadJobRecord(jobDir); err != nil || kept.Outcome != rec.Outcome {
		t.Errorf("the record on disk holds %+v (%v), want the ok it was sent as", kept.Outcome, err)
	}
}

// A fail or a fault never names the prior head in its report: nothing new
// was pushed, and the card machine refuses one that does (red done-whens at
// a verified prior head keep the record's pushed head, which is that head).
func TestAFailAtThePriorHeadIsReportedWithNoHead(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{}
	r, _ := warnRunner_internal(t, sp, &cr2Wall{})
	rec := JobRecord{Epoch: 3, Consumer: "p1.w3", Kind: "work", Row: "vision.ds", Gen: 2, BaseSHA: testPriorSHA, PriorHead: testCommitSHA, State: JobHarvested,
		Outcome: Outcome{Result: "fail", Head: testCommitSHA, Reason: "tests red (go test ./...): FAIL"}}
	jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if err := r.sendReportAndClean_internal(context.Background(), jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if got := sp.reported(); len(got) != 1 || got[0].Result != "fail" || got[0].Head != "" {
		t.Errorf("reports %+v, want the fail with no head", got)
	}
}
