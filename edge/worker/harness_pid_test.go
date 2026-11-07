package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Harness pid recording, exit classification, argv handling, restart and
// report-retry guarantees of the runner and the process wall. Unit tier:
// fake sprint, fake wall, fake process table, fake exec.

func pidOKExec_internal() *edge.FakeExec {
	fx := edge.NewFakeExec()
	fx.On("*", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{} })
	return fx
}

type pidProcs struct {
	mu    sync.Mutex
	alive bool
	kills []int
}

func (p *pidProcs) Alive(int, string) bool { p.mu.Lock(); defer p.mu.Unlock(); return p.alive }
func (p *pidProcs) Kill(pid int, _ string, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kills = append(p.kills, pid)
	return nil
}

type pidSprint struct {
	mu        sync.Mutex
	next      NextResponse
	kind      string
	reportErr error
	reports   []ReportRequest
	releases  []string
}

func (s *pidSprint) Next(context.Context, string) (NextResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, nil
}
func (s *pidSprint) Take(_ context.Context, _, id string, gen int) (TakeInfo, error) {
	kind := s.kind
	if kind == "" {
		kind = "read"
	}
	return TakeInfo{Consumer: id, Producer: "p1", Kind: kind, Stream: "s1", Tier: "flash", Model: "m1", Deadline: 900, CommitName: testCommitName, CommitEmail: testCommitEmail, Gen: gen, Epoch: 3, Attempt: 1, Branch: "sprint/s1/p1", BaseSHA: strings.Repeat("b", 40), Head: strings.Repeat("a", 40)}, nil
}
func (s *pidSprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	if s.reportErr != nil {
		return store.Receipt{}, s.reportErr
	}
	return store.Receipt{Accepted: true, Op: req.ResultID}, nil
}
func (s *pidSprint) Hook(context.Context, string) error                   { return nil }
func (s *pidSprint) Unhook(context.Context, string) error                 { return nil }
func (s *pidSprint) OpenJudgement(context.Context, store.Judgement) error { return nil }
func (s *pidSprint) Release(_ context.Context, row, id string, gen int, _, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = append(s.releases, fmt.Sprintf("%s@%d %s", id, gen, reason))
	return nil
}
func (s *pidSprint) reported() []ReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reports)
}

type pidWall struct{ err error }

func (w *pidWall) RunHarness(_ context.Context, j Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
	if w.err != nil {
		return WallRunResult{ExitCode: 1}, w.err
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	return WallRunResult{PID: 4242, Nonce: "n-4242"}, nil
}
func (w *pidWall) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}

func pidRunner_internal(t *testing.T, sp SprintClient, wall ReserveWall, procs ProcessTable, now time.Time) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: sp, Wall: wall, Procs: procs, DefaultHarness: &ScriptHarness{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The harness pid is recorded at start (a restart adopts or kills it, never
// doubles it). The wall must report the harness's pid and not the runner's:
// a restart that sees a dead runner pid reports fault, and the orphaned
// harness (its own process group) keeps running beside the next attempt.
func TestHarnessPidIsNotRunnerPid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := &OSReserveWall{Exec: pidOKExec_internal(), Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"}}
	res, _ := w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, []string{"claude"}, []string{"PATH=/usr/bin"}, time.Minute)
	if res.PID == os.Getpid() {
		t.Errorf("the wall reported pid %d for the harness: that is nova-worker itself", res.PID)
	}
}

// A live harness is adopted by pid and nonce. OSProcessTable must not treat
// any live process holding a recorded pid (pid reuse) as the harness, and
// Kill must not signal it.
func TestProcessTableChecksNonce(t *testing.T) {
	t.Parallel()
	if (OSProcessTable{}).Alive(os.Getppid(), "n-some-other-harness") {
		t.Errorf("pid %d with a nonce it never had was judged our live harness: a restart adopts or kills the wrong process", os.Getppid())
	}
}

// A working consumer whose harness is alive is adopted by pid and nonce, its
// deadline from the recorded start. RecoverRow must enforce the recorded
// deadline on an adopted harness: kill it past the deadline and report it.
func TestAdoptedHarnessDeadlineIsEnforced(t *testing.T) {
	t.Parallel()
	sp := &pidSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 2}}}}
	procs := &pidProcs{alive: true}
	r := pidRunner_internal(t, sp, &pidWall{}, procs, time.Unix(100000, 0))
	dir := filepath.Join(r.RootDir, "jobs", "p1.w1")
	if err := WriteJobRecord(dir, JobRecord{Consumer: "p1.w1", Kind: "work", Row: "vision.ds", Gen: 2, State: JobRunning, PID: 777, Nonce: "n-777", StartedAt: 1000, DeadlineAt: 2000}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverRow(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	if len(procs.kills) == 0 {
		t.Errorf("adopted harness pid 777 is 98000 s past its recorded deadline and was neither killed nor reported")
	}
}

// A fault is only an infrastructure failure. Output text that mentions "429",
// "rate limit", "server_error" or "bad gateway" must not override result.json:
// an agent whose transcript mentions 429 (it is working on rate limiting) and
// who wrote ok is reported ok.
func TestResultFileOverridesRateLimitText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "result.json"), []byte(`{"result":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := ClassifyJob(&ClaudeHarness{}, Job{JobDir: dir}, 0, time.Minute, 15*time.Second, []byte("added a test: the server answers HTTP 429 when the bucket is empty\nall tests pass"))
	if out.Result != "ok" {
		t.Errorf("exit 0 with result.json ok was classified %s (%s)", out.Result, out.Reason)
	}
}

// A done-when command line reaches the wall as an argv list: a quoted
// argument reaches the program unquoted, not split on whitespace.
func TestDoneWhenLineReachesWallAsArgv(t *testing.T) {
	t.Parallel()
	fx := pidOKExec_internal()
	dir := t.TempDir()
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"}}
	_, _ = w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, `go test -run 'TestA|TestB' ./...`, time.Minute)
	calls := fx.Calls()
	if len(calls) != 1 {
		t.Fatalf("want one exec, got %d", len(calls))
	}
	argv := calls[0].Cmd.Argv
	want := []string{"go", "test", "-run", "TestA|TestB", "./..."}
	if len(argv) < len(want) || !slices.Equal(argv[len(argv)-len(want):], want) {
		t.Errorf("done-when line reached the wall as %q, not as an argv list %q", argv, want)
	}
}

// A card can never write outside its job directory. The wall must not grant
// --write to the work clone's common .git, which every job shares. Inside the wall the harness can
// write .git/hooks/pre-push or core.fsmonitor in .git/config; the runner's own
// git (push at harvest, status at clean) then runs that code outside the
// wall with the runner's whole environment (OSGitOps passes no CleanEnv).
func TestWallGrantsWriteToCommonGitDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	common := filepath.Join(root, "clone", ".git")
	if err := os.MkdirAll(filepath.Join(common, "worktrees", "c1"), 0o755); err != nil {
		t.Fatal(err)
	}
	job := filepath.Join(root, "jobs", "c1")
	wt := filepath.Join(job, "repo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(common, "worktrees", "c1")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fx := pidOKExec_internal()
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true}}
	_, _ = w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: job, WorktreeDir: wt}, []string{"claude"}, []string{"PATH=/usr/bin"}, time.Minute)
	argv := fx.Calls()[0].Cmd.Argv
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--write" && argv[i+1] == common {
			t.Errorf("the wall grants write to %s (hooks, config) which the runner's git executes outside the wall: %q", common, argv)
		}
	}
}

// No slot within the wait is no start yet, never a failure. After Take,
// RunConsumer returns ErrNoSlot; the next wake's RecoverRow must not find the
// runner's pid dead and report fault "restart: harness exited while runner
// was down".
func TestNoSlotIsNotReportedAsFault(t *testing.T) {
	t.Parallel()
	sp := &pidSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	r := pidRunner_internal(t, sp, &pidWall{err: ErrNoSlot}, &pidProcs{}, time.Unix(1000, 0))
	if err := r.RunConsumer(context.Background(), sp.next, sp.next.Working[0]); !errors.Is(err, ErrNoSlot) {
		t.Fatalf("want ErrNoSlot, got %v", err)
	}
	sp.mu.Lock()
	sp.next.Ready, sp.next.Working = nil, []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2, Attempt: 1}}
	sp.mu.Unlock()
	_ = r.RecoverRow(context.Background(), "vision.ds")
	for _, rep := range sp.reported() {
		if rep.Result == "fault" {
			t.Errorf("a consumer that never got a slot was reported fault: %q", rep.Reason)
		}
	}
}

// A report that fails is durable (the job record keeps it; the next wake
// sends it), and a report is acknowledged only by exit 0. A refused report stays at state
// reporting and each wake that still lists the consumer as working sends it
// again under the same id; the record goes when the consumer turns final.
func TestRefusedReportResentEveryWake(t *testing.T) {
	t.Parallel()
	sp := &pidSprint{reportErr: fmt.Errorf("%w: consumer p1.r1 is held by amd-2.ds", ErrRefused),
		next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	r := pidRunner_internal(t, sp, &pidWall{}, &pidProcs{}, time.Unix(1000, 0))
	_ = r.RunConsumer(context.Background(), sp.next, sp.next.Working[0])
	sp.mu.Lock()
	sp.next.Ready, sp.next.Working = nil, []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2, Attempt: 1}}
	sp.mu.Unlock()
	_ = r.RecoverRow(context.Background(), "vision.ds")
	_ = r.RecoverRow(context.Background(), "vision.ds")
	if n := len(sp.reported()); n != 3 {
		t.Errorf("a refused report was sent %d times over three wakes, want 3", n)
	}
	sp.mu.Lock()
	sp.next.Working, sp.next.Final = nil, []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2, State: card.Fail}}
	sp.mu.Unlock()
	_ = r.RecoverRow(context.Background(), "vision.ds")
	if _, err := os.Stat(filepath.Join(r.RootDir, "jobs", "p1.r1")); !os.IsNotExist(err) {
		t.Errorf("a consumer that turned final left its job directory: %v", err)
	}
}

// ok means the result file says ok and a head past base_sha and prior_head is
// pushed. With Git nil (no --work-clone) a work consumer must not be reported
// ok with no head.
func TestNoGitNeverReportsOkWithoutHead(t *testing.T) {
	t.Parallel()
	sp := &pidSprint{kind: "work", next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 2}}}}
	r := pidRunner_internal(t, sp, &pidWall{}, &pidProcs{}, time.Unix(1000, 0))
	_ = r.RunConsumer(context.Background(), sp.next, sp.next.Working[0])
	for _, rep := range sp.reported() {
		if rep.Result == "ok" && rep.Head == "" {
			t.Errorf("work consumer %s reported ok with no pushed head", rep.Consumer)
		}
	}
}

// A zero-exit run whose output does not parse must not yield an accepted
// receipt: the receipt check before the job directory is cleaned would then
// hold by construction, and the directory be removed on a receipt the store
// never gave.
func TestUnparsedReportOutputIsNoReceipt(t *testing.T) {
	t.Parallel()
	cli := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) {
		return []byte("nova-secrets: warning: key cache stale\n"), 0, nil
	}}
	rec, err := cli.Report(context.Background(), ReportRequest{Row: "vision.ds", Consumer: "p1.w1", Gen: 3, ResultID: "res-e3-p1.w1-a1-g3", Result: "ok"})
	if err == nil && rec.Accepted {
		t.Errorf("output with no receipt became an accepted receipt for %s", rec.Op)
	}
}

// EdgeRequest, RequestID, report and take op ids all carry the epoch: the
// take verb is sent with an op id (FormatTakeOpID), so the epoch fences it.
func TestTakeSendsEpochOpID(t *testing.T) {
	t.Parallel()
	var argv []string
	cli := &CLISprintClient{Run: func(_ context.Context, a []string) ([]byte, int, error) {
		argv = a
		return []byte(`{"Consumer":"p1.w1"}`), 0, nil
	}}
	_, _ = cli.Take(context.Background(), "vision.ds", "p1.w1", 4)
	if !slices.Contains(argv, "--id") {
		t.Errorf("take was sent as %q: no op id, so no epoch fences it", argv)
	}
}
