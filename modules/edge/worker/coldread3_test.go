package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Cold read 3, 2026-11-08, at 032afd657 (modules/edge): one failing probe
// per confirmed defect.

// warnRunner_internal is a runner on sp and wall whose WORKER WARNING lines
// are kept.
func warnRunner_internal(t *testing.T, sp SprintClient, wall ReserveWall) (*Runner, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	r, err := NewRunner(Runner{Now: testClock_internal(), Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: sp, Wall: wall, DefaultHarness: &ScriptHarness{},
		Warn: func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	return r, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

// cliFailure_internal is the error CLISprintClient.Report gives for a
// nova-sprint that printed out and exited code (err: exec's).
func cliFailure_internal(t *testing.T, out string, code int, err error) error {
	t.Helper()
	cli := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) { return []byte(out), code, err }}
	_, rerr := cli.Report(context.Background(), ReportRequest{Row: "vision.ds", Consumer: "p1.r1", Gen: 2, ResultID: "res-1", Result: "ok"})
	if rerr == nil {
		t.Fatalf("%q exit %d was not an error", out, code)
	}
	return rerr
}

// verbs.go classifyCLIError_internal: every exit but 1 was the store down,
// so a report the sprint failed for misuse or version skew (exit 2) was
// waited out forever. Only the store unreachable (its own words, a verb
// that never finished, or a sprint that said nothing) is ErrStoreDown.
func TestColdread3SprintExit2IsARefusalUnlessTheStoreIsDown(t *testing.T) {
	t.Parallel()
	timeout := &edge.ExecError{Program: "nova-sprint", Kind: edge.ErrTimeout, Reason: "timed out after 30s"}
	for name, tc := range map[string]struct {
		out       string
		code      int
		err       error
		storeDown bool
	}{
		"misuse":          {out: "SPRINT REFUSED report: unknown flag --usage: nova-sprint help report\n", code: 2},
		"other exit":      {out: "panic: boom\n", code: 3},
		"store":           {out: "SPRINT REFUSED report: the store did not answer (connection refused): nova-sprint check\n", code: 2, storeDown: true},
		"postgres":        {out: "nova-secrets: warning: key cache stale\nSPRINT REFUSED report: PostgreSQL cannot be reached or NOVA_PG_DSN is invalid (dial): check NOVA_PG_DSN and run nova-fleet check\n", code: 2, storeDown: true},
		"timeout":         {code: -1, err: timeout, storeDown: true},
		"nothing printed": {code: 2, storeDown: true},
	} {
		err := cliFailure_internal(t, tc.out, tc.code, tc.err)
		if errors.Is(err, ErrStoreDown) != tc.storeDown || errors.Is(err, ErrRefused) == tc.storeDown {
			t.Errorf("%s: err = %v, want store down %v (else refused)", name, err, tc.storeDown)
		}
		if !tc.storeDown && !strings.Contains(err.Error(), fmt.Sprintf("exit %d", tc.code)) {
			t.Errorf("%s: err = %v does not name exit %d", name, err, tc.code)
		}
	}
}

// A report refused with exit 2 is counted on its record, said as a WORKER
// WARNING, and a --once pass (StepRow) fails rather than print WORKER PASS.
func TestColdread3RefusedReportIsWarnedCountedAndFailsOnce(t *testing.T) {
	t.Parallel()
	refused := cliFailure_internal(t, "SPRINT REFUSED report: unknown flag --usage: nova-sprint help report\n", 2, errors.New("exit status 2"))
	sp := &cr2Sprint{next: NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}, reportErr: refused}
	r, warnings := warnRunner_internal(t, sp, &cr2Wall{})
	if err := r.StepRow(context.Background(), "vision.ds"); err == nil {
		t.Errorf("a pass whose report was refused (exit 2) returned no error: --once prints WORKER PASS and exits 0")
	}
	rec, err := ReadJobRecord(filepath.Join(r.RootDir, "jobs", "p1.r1"))
	if err != nil || rec.Refusals != 1 || rec.State != JobReporting {
		t.Errorf("record %+v (%v): want the refusal counted once at %s", rec, err, JobReporting)
	}
	if w := strings.Join(warnings(), "\n"); !strings.Contains(w, "WORKER WARNING") || !strings.Contains(w, "exit 2") {
		t.Errorf("warnings %q: the refused report is never said", w)
	}
}

// A --once pass whose owed report (sent again by recovery) is refused fails
// too: recovery's resend errors were dropped.
func TestColdread3OnceFailsWhenAnOwedReportIsRefused(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{next: NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}},
		reportErr: fmt.Errorf("%w: report: exit 1: consumer p1.r1 is held by amd-2.ds", ErrRefused)}
	r, _ := warnRunner_internal(t, sp, &cr2Wall{})
	jobDir := filepath.Join(r.RootDir, "jobs", "p1.r1")
	if err := WriteJobRecord(jobDir, JobRecord{Epoch: 3, Consumer: "p1.r1", Kind: "read", Row: "vision.ds", Gen: 2, State: JobReporting, Outcome: Outcome{Result: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.StepRow(context.Background(), "vision.ds"); err == nil {
		t.Errorf("a pass whose owed report was refused returned no error")
	}
}

// A --once pass says a report the store was down for, and a job that got
// no nova-reserve slot: neither was sent, and both passed silently.
func TestColdread3OnceSaysStoreDownAndNoSlot(t *testing.T) {
	t.Parallel()
	next := NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}
	sp := &cr2Sprint{next: next, reportErr: fmt.Errorf("%w: nova-sprint report: exit 2", ErrStoreDown)}
	r, _ := warnRunner_internal(t, sp, &cr2Wall{})
	if err := r.StepRow(context.Background(), "vision.ds"); !errors.Is(err, ErrStoreDown) {
		t.Errorf("store down at the report: StepRow = %v, want it said", err)
	}
	r2, _ := warnRunner_internal(t, &cr2Sprint{next: next}, &cr2Wall{err: fmt.Errorf("%w: nova-reserve: no slot within 5s", ErrNoSlot)})
	if err := r2.StepRow(context.Background(), "vision.ds"); !errors.Is(err, ErrNoSlot) {
		t.Errorf("no slot: StepRow = %v, want it said", err)
	}
}

// infraWall_internal answers RunTest and RunStep as told.
type infraWall_internal struct {
	cr2Wall
	test, step       WallRunResult
	testErr, stepErr error
}

func (w *infraWall_internal) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return w.test, w.testErr
}
func (w *infraWall_internal) RunStep(context.Context, Job, []string, time.Duration) (WallRunResult, error) {
	return w.step, w.stepErr
}

// runner.go rebaseAndTestStep_internal: every done-when error or non-zero
// exit was "fail: tests red", a missing binary (127) and a refused sandbox
// too, so correct work was bounced. As the harness run's
// (isInfraFault_internal), the wall's or the machine's failure is a fault
// naming it; and a rebase or the rebased head's checkout that fails for
// anything but a conflict is the worker's, never the card's. A done-when
// its timeout killed is the card's since 2026-11-11: its bound is the
// card's own deadline, no longer the worker's two minutes.
func TestColdread3InfraFailureAfterTheHarnessIsAFaultNotAFail(t *testing.T) {
	t.Parallel()
	timeout := &edge.ExecError{Program: "nova-reserve", Kind: edge.ErrTimeout, Reason: "timed out after 10m"}
	for name, tc := range map[string]struct {
		wall      *infraWall_internal
		rebaseErr error
		stream    string
		want      string
	}{
		"test 127":        {wall: &infraWall_internal{test: WallRunResult{ExitCode: 127, Output: []byte("sh: go: command not found")}}, want: "fault"},
		"test missing":    {wall: &infraWall_internal{test: WallRunResult{ExitCode: 2, Output: []byte("/bin/sh: 1: sbcl: not found\nmake: *** [Makefile:10: roadmap] Error 127")}}, want: "fault"},
		"test sandbox":    {wall: &infraWall_internal{test: WallRunResult{ExitCode: 1, Output: []byte("SANDBOX REFUSED --write /x: not a directory")}}, want: "fault"},
		"test timeout":    {wall: &infraWall_internal{test: WallRunResult{ExitCode: -1}, testErr: timeout}, want: "fail"}, // it ran to its bound, the card's deadline (donewhen_test.go)
		"test red":        {wall: &infraWall_internal{test: WallRunResult{ExitCode: 1, Output: []byte("--- FAIL: TestX")}}, want: "fail"},
		"rebase broke":    {wall: &infraWall_internal{}, rebaseErr: errors.New("git rebase: exit 128: fatal: index.lock exists"), stream: "sprint/s1", want: "fault"},
		"apply 126":       {wall: &infraWall_internal{step: WallRunResult{ExitCode: 126, Output: []byte("git: permission denied")}}, stream: "sprint/s1", want: "fault"},
		"apply timeout":   {wall: &infraWall_internal{step: WallRunResult{ExitCode: -1}, stepErr: timeout}, stream: "sprint/s1", want: "fault"},
		"rebase clean ok": {wall: &infraWall_internal{}, stream: "sprint/s1", want: "ok"},
	} {
		g := newFakeGit_internal()
		g.rebaseErr = tc.rebaseErr
		r, err := NewRunner(Runner{Now: testClock_internal(), Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: tc.wall, Git: g, DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
		if err != nil {
			t.Fatal(err)
		}
		rec := JobRecord{Consumer: "p1.w1", Kind: "work", Row: "vision.ds", Repo: "o/r", Forge: "github", Gen: 2, StreamRef: tc.stream, Branch: "sprint/s1/p1",
			BaseSHA: testPriorSHA, PushedHead: testCommitSHA, DoneWhen: []string{"go test ./..."}, Outcome: Outcome{Result: "ok", Head: testCommitSHA}}
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
			t.Fatalf("%s: %v", name, err)
		}
		if rec.Outcome.Result != tc.want || (tc.want == "fault" && rec.Outcome.Reason == "") {
			t.Errorf("%s: outcome %+v, want %s with its reason", name, rec.Outcome, tc.want)
		}
	}
}

// runner.go rebaseAndTestStep_internal: a retry whose harness wrote ok
// with no new commit has the prior head as its head. It was rebased in a
// clone with nothing checked out, reported as a fault at the prior head,
// refused by the store three times and quarantined. It is a fail, with no
// head, and no rebase is tried.
func TestRetryOkAtThePriorHeadIsAFailWithNoHead(t *testing.T) {
	t.Parallel()
	g := newFakeGit_internal()
	g.rebaseErr = errors.New("git rebase: exit 128: fatal: Could not resolve HEAD to a commit")
	r, err := NewRunner(Runner{Now: testClock_internal(), Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: &infraWall_internal{}, Git: g, DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	rec := JobRecord{Consumer: "p1.w2", Kind: "work", Row: "vision.ds", Repo: "o/r", Forge: "github", Gen: 2, StreamRef: "sprint/s1", Branch: "sprint/s1/p1",
		BaseSHA: testPriorSHA, PriorHead: testCommitSHA, PushedHead: testCommitSHA, DoneWhen: []string{"go test ./..."}, Outcome: Outcome{Result: "ok", Head: testCommitSHA}}
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
	if rec.Outcome.Result != "fail" || rec.Outcome.Head != "" || !strings.Contains(rec.Outcome.Reason, "committed nothing over the prior head") {
		t.Fatalf("outcome %+v, want fail with no head: the harness committed nothing over the prior head", rec.Outcome)
	}
}

// runner.go runWallCmd_internal: no slot was any exit 1 whose output held
// "nova-reserve:" and not "nova-reserve: HELD", so a harness or test that
// failed mentioning it was no slot and ran again for ever. nova-reserve's
// own refusal is exit 1 with its first stderr line "nova-reserve: no slot
// within <d>" (or "no exclusive hold within"), before any HELD.
func TestColdread3NoSlotIsNovaReservesOwnRefusal(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		res    edge.Result
		noSlot bool
	}{
		"no slot":        {res: edge.Result{ExitCode: 1, Stderr: "nova-reserve: no slot within 5s: every slot busy; run: nova-reserve status\n"}, noSlot: true},
		"no exclusive":   {res: edge.Result{ExitCode: 1, Stderr: "nova-reserve: no exclusive hold within 5s: c1 (pid 9); run: nova-reserve status\n"}, noSlot: true},
		"mentioned":      {res: edge.Result{ExitCode: 1, Stdout: "--- FAIL: TestWrap: nova-reserve: wrapper exited\n"}},
		"after held":     {res: edge.Result{ExitCode: 1, Stderr: "nova-reserve: HELD mode=shared slot=1/4 pid=9\nnova-reserve: no slot within 1s: every slot busy\n"}},
		"command failed": {res: edge.Result{ExitCode: 1, Stderr: "nova-reserve: run: nova-sandbox: exit 1\n"}},
	} {
		w := &OSReserveWall{Exec: &wallExec{res: tc.res}, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"}}
		_, err := w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: t.TempDir()}, []string{"claude"}, nil, time.Minute)
		if errors.Is(err, ErrNoSlot) != tc.noSlot {
			t.Errorf("%s: err = %v, want no slot %v", name, err, tc.noSlot)
		}
	}
}

// resultWall_internal is a wall with no slot, or one whose harness writes
// no verdict; seen says whether a result.json was in the job directory when
// the harness started.
type resultWall_internal struct {
	cr2Wall
	noSlot bool
	mu     sync.Mutex
	seen   []bool
}

func (w *resultWall_internal) RunHarness(_ context.Context, j Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.seen = append(w.seen, fileExists_internal(filepath.Join(j.JobDir, "result.json")))
	w.mu.Unlock()
	if w.noSlot {
		return WallRunResult{ExitCode: 1}, fmt.Errorf("%w: nova-reserve: no slot within 5s", ErrNoSlot)
	}
	return WallRunResult{ExitCode: 1, PID: 4242, Nonce: "n-4242"}, nil
}

// A job with no slot waits for one: every pass runs it again from staged,
// never giving the card back (live 2026-11-09: a bench released 124 cards in
// 20 minutes, each to be dealt to it again). The card's deadline bounds the
// wait: past it the sprint withdraws the consumer (the steps' limits). And a
// run never starts with an earlier run's result.json in place, which would
// be read as its verdict.
func TestColdread3NoSlotWaitsAndAStaleVerdictIsCleared(t *testing.T) {
	t.Parallel()
	next := NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2, Attempt: 1}}}
	sp := &cr2Sprint{next: next}
	wall := &resultWall_internal{noSlot: true}
	r, _ := warnRunner_internal(t, sp, wall)
	const passes = 10
	for range passes {
		if err := r.StepRow(context.Background(), "vision.ds"); !errors.Is(err, ErrNoSlot) {
			t.Fatalf("pass: %v, want no slot", err)
		}
	}
	rec, err := ReadJobRecord(filepath.Join(r.RootDir, "jobs", "p1.r1"))
	if len(sp.releases) != 0 || len(sp.reported()) != 0 || err != nil || rec.State != JobStaged || len(wall.seen) != passes {
		t.Errorf("after %d passes with no slot: releases %v reports %+v record %+v (%v) runs %d, want the job kept staged and run each pass", passes, sp.releases, sp.reported(), rec, err, len(wall.seen))
	}

	sp2 := &cr2Sprint{next: next}
	wall2 := &resultWall_internal{}
	r2, _ := warnRunner_internal(t, sp2, wall2)
	jobDir := filepath.Join(r2.RootDir, "jobs", "p1.r1")
	stale := JobRecord{Epoch: 3, Consumer: "p1.r1", Producer: "p1", Kind: "read", Row: "vision.ds", Stream: "s1", Gen: 2, Attempt: 1, State: JobStaged}
	if err := WriteJobRecord(jobDir, stale); err != nil {
		t.Fatal(err)
	}
	if err := writeReceipt_internal(jobDir, TakeInfo{Consumer: "p1.r1", Producer: "p1", Kind: "read", Stream: "s1", Tier: "flash", Model: "m1", Deadline: 900, CommitName: testCommitName, CommitEmail: testCommitEmail, Gen: 2, Epoch: 3, Attempt: 1, Head: strings.Repeat("a", 40)}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(jobDir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = r2.StepRow(context.Background(), "vision.ds")
	if len(wall2.seen) != 1 || wall2.seen[0] {
		t.Errorf("harness runs %v: the run started with the earlier run's result.json in place", wall2.seen)
	}
	if reps := sp2.reported(); len(reps) != 1 || reps[0].Result == "ok" {
		t.Errorf("reports %+v: a harness that wrote no verdict was reported from a stale one", reps)
	}
}

func TestIsInfraFaultIgnoresExitZeroTranscriptNoise(t *testing.T) {
	t.Parallel()
	noisy := WallRunResult{
		ExitCode: 0,
		Output:   []byte("SANDBOX REFUSED in earlier probe\n/bin/sh: 1: foo: not found\nmake: *** [Makefile:1: bar] Error 127\n{\"result\":\"ok\"}\n"),
	}
	if fault, why := isInfraFault_internal(noisy, nil); fault {
		t.Fatalf("exit-0 run classified as infra fault: %s", why)
	}
	if fault, why := isMissingTestToolchain_internal(noisy); fault {
		t.Fatalf("exit-0 run classified as missing toolchain: %s", why)
	}
}
