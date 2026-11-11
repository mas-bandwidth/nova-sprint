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

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Cold read 2, 2026-11-06, at ef4df30c (Rowan's cold reader; probes marked
// Stella verify findings from Stella's source-only read). One failing probe
// per confirmed defect.

func cr2OKExec_internal() *edge.FakeExec {
	fx := edge.NewFakeExec()
	fx.On("*", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{} })
	return fx
}

type cr2Procs struct {
	mu    sync.Mutex
	alive bool
	kills []int
}

func (p *cr2Procs) Alive(int, string) bool { p.mu.Lock(); defer p.mu.Unlock(); return p.alive }
func (p *cr2Procs) Kill(pid int, _ string, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kills = append(p.kills, pid)
	return nil
}

type cr2Sprint struct {
	mu        sync.Mutex
	next      NextResponse
	kind      string
	reportErr error
	reports   []ReportRequest
	releases  []string
}

func (s *cr2Sprint) Next(context.Context, string) (NextResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, nil
}
func (s *cr2Sprint) Take(_ context.Context, _, id string, gen int) (TakeInfo, error) {
	kind := s.kind
	if kind == "" {
		kind = "read"
	}
	return TakeInfo{Consumer: id, Producer: "p1", Kind: kind, Stream: "s1", Tier: "flash", Model: "m1", Deadline: 900, CommitName: testCommitName, CommitEmail: testCommitEmail, Gen: gen, Epoch: 3, Attempt: 1, Branch: "sprint/s1/p1", BaseSHA: strings.Repeat("b", 40), Head: strings.Repeat("a", 40)}, nil
}
func (s *cr2Sprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	if s.reportErr != nil {
		return store.Receipt{}, s.reportErr
	}
	return store.Receipt{Accepted: true, Op: req.ResultID}, nil
}
func (s *cr2Sprint) Hook(context.Context, string) error   { return nil }
func (s *cr2Sprint) Unhook(context.Context, string) error { return nil }
func (s *cr2Sprint) Release(_ context.Context, row, id string, gen int, _, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = append(s.releases, fmt.Sprintf("%s@%d %s", id, gen, reason))
	return nil
}
func (s *cr2Sprint) reported() []ReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reports)
}

type cr2Wall struct{ err error }

func (w *cr2Wall) RunHarness(_ context.Context, j Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
	if w.err != nil {
		return WallRunResult{ExitCode: 1}, w.err
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	return WallRunResult{PID: 4242, Nonce: "n-4242"}, nil
}
func (w *cr2Wall) RunStep(context.Context, Job, []string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}
func (w *cr2Wall) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}

func cr2Runner_internal(t *testing.T, sp SprintClient, wall ReserveWall, procs ProcessTable, now time.Time) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: sp, Wall: wall, Procs: procs, DefaultHarness: &ScriptHarness{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Round 2 ruling (worker): "the harness pid is recorded at start (a restart
// adopts or kills it, never doubles it)". The shipped wall records the
// runner's own pid, never the harness's (runner.go:907-908), and runStep
// writes os.Getpid() before launch (runner.go:541-543). A restart sees a dead
// runner pid, reports fault, and the orphaned harness (its own process group,
// exec.go:110) keeps running beside the next attempt.
func TestColdread2DefectHarnessPidIsRunnerPid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := &OSReserveWall{Exec: cr2OKExec_internal(), Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"}}
	res, _ := w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, []string{"claude"}, []string{"PATH=/usr/bin"}, time.Minute)
	if res.PID == os.Getpid() {
		t.Errorf("the wall reported pid %d for the harness: that is nova-worker itself", res.PID)
	}
}

// Stella, source read: OSProcessTable ignores the nonce (runner.go:75-88).
// Design restart rule: "adopted by pid and nonce". Any live process holding a
// recorded pid (pid reuse) is adopted, and Kill signals it.
func TestColdread2DefectProcessTableIgnoresNonce(t *testing.T) {
	t.Parallel()
	if (OSProcessTable{}).Alive(os.Getppid(), "n-some-other-harness") {
		t.Errorf("pid %d with a nonce it never had was judged our live harness: a restart adopts or kills the wrong process", os.Getppid())
	}
}

// The restart rule (2026-11-10): a bench that restarts gives up its cards
// and the dealer deals them again; nothing is adopted or resumed. A live
// harness on a card the row still holds is never this worker's own (its
// harness ends with its job), so it is a dead worker's: it is killed and the
// card given back with no blame, never reported and never run again.
func TestALiveHarnessADeadWorkerLeftIsKilledAndItsCardGivenBack(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 2}}}}
	procs := &cr2Procs{alive: true}
	wall := &liveWall{}
	r := cr2Runner_internal(t, sp, wall, procs, time.Unix(100000, 0))
	dir := filepath.Join(r.RootDir, "jobs", "p1.w1")
	if err := WriteJobRecord(dir, JobRecord{Consumer: "p1.w1", Kind: "work", Row: "vision.ds", Gen: 2, State: JobRunning, PID: 777, Nonce: "n-777", StartedAt: 1000, DeadlineAt: 200000}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverRow(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(procs.kills, []int{777}) || len(sp.releases) != 1 || !strings.HasPrefix(sp.releases[0], "p1.w1@2 its worker restarted") ||
		len(sp.reported()) != 0 || len(wall.ran()) != 0 {
		t.Errorf("a dead worker's live harness: kills %v releases %v reports %+v runs %v, want it killed and the card given back, unreported and not run", procs.kills, sp.releases, sp.reported(), wall.ran())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the given-back job's directory must go: %v", err)
	}
}

// Round 2 ruling (worker): "a fault is only an infrastructure failure".
// classifyExitAndTail_internal matches "429", "rate limit", "server_error" or
// "bad gateway" anywhere in the harness output before reading result.json
// (harness.go:188-198), and ClassifyJob returns the fault without looking at
// the file (harness.go:210-214). An agent whose transcript mentions 429 (it is
// working on rate limiting) and who wrote ok is reported fault; one who wrote
// broken is never bounced.
func TestColdread2DefectRateLimitTextOverridesResultFile(t *testing.T) {
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

// Round 2 ruling (worker): "the brief's command fields are argv lists".
// DoneWhen is []string of command lines, split by strings.Fields in RunTest
// (runner.go:858): a quoted argument reaches the program with its quotes.
func TestColdread2DefectDoneWhenIsNotArgv(t *testing.T) {
	t.Parallel()
	fx := cr2OKExec_internal()
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

// Design: "A card can never ... write outside its job directory"; guarantee
// 12. effectiveConfig_internal grants --write to the work clone's common .git
// (runner.go:868-873), shared by every job. Inside the wall the harness can
// write .git/hooks/pre-push or core.fsmonitor in .git/config; the runner's own
// git (push at harvest, status at clean) then runs that code outside the
// wall with the runner's whole environment (OSGitOps passes no CleanEnv).
func TestColdread2DefectWallGrantsWriteToCommonGitDir(t *testing.T) {
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
	fx := cr2OKExec_internal()
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true}}
	_, _ = w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: job, WorktreeDir: wt}, []string{"claude"}, []string{"PATH=/usr/bin"}, time.Minute)
	argv := fx.Calls()[0].Cmd.Argv
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--write" && argv[i+1] == common {
			t.Errorf("the wall grants write to %s (hooks, config) which the runner's git executes outside the wall: %q", common, argv)
		}
	}
}

// Design runner step 4: "no slot within the wait is no start yet, never a
// failure." After Take, RunConsumer returns ErrNoSlot leaving job.json at
// state running with the runner's pid (runner.go:454-456); the next wake's
// RecoverRow finds that pid not alive and reports fault "restart: harness
// exited while runner was down" (runner.go:297-304).
func TestColdread2DefectNoSlotBecomesFaultReport(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	r := cr2Runner_internal(t, sp, &cr2Wall{err: ErrNoSlot}, &cr2Procs{}, time.Unix(1000, 0))
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

// Round 2 ruling (worker): "a nova-sprint refusal (exit 1) is final".
// Reversed by the stitch (drift row 20, Rowan 2026-11-06): "a report that
// fails is durable (the job record keeps it; the next wake sends it)", and a
// report is acknowledged only by exit 0. A refused report stays at state
// reporting and each wake that still lists the consumer as working sends it
// again under the same id; the record goes when the consumer turns final.
func TestColdread2DefectRefusedReportResentEveryWake(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{reportErr: fmt.Errorf("%w: consumer p1.r1 is held by amd-2.ds", ErrRefused),
		next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	r := cr2Runner_internal(t, sp, &cr2Wall{}, &cr2Procs{}, time.Unix(1000, 0))
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

// Stella, source read: git is optional where the design requires it
// (cmd/nova-worker/main.go:84-86 leaves Git nil without --work-clone). Design
// results: "ok: the result file says ok and a head past base_sha and
// prior_head is pushed." With Git nil, harvestStep skips the head check
// (runner.go:590) and a work consumer is reported ok with no head.
func TestColdread2DefectNoGitReportsOkWithoutHead(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{kind: "work", next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 2}}}}
	r := cr2Runner_internal(t, sp, &cr2Wall{}, &cr2Procs{}, time.Unix(1000, 0))
	_ = r.RunConsumer(context.Background(), sp.next, sp.next.Working[0])
	for _, rep := range sp.reported() {
		if rep.Result == "ok" && rep.Head == "" {
			t.Errorf("work consumer %s reported ok with no pushed head", rep.Consumer)
		}
	}
}

// Stella, source read: verbs.go:124-128 synthesizes an accepted receipt when
// a zero-exit run's output does not parse, and fills a missing op id. The
// receipt check sendReportAndClean relies on (runner.go:688) then holds by
// construction, and the job directory is removed on a receipt the store
// never gave.
func TestColdread2DefectUnparsedReportOutputBecomesReceipt(t *testing.T) {
	t.Parallel()
	cli := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) {
		return []byte("nova-secrets: warning: key cache stale\n"), 0, nil
	}}
	rec, err := cli.Report(context.Background(), ReportRequest{Row: "vision.ds", Consumer: "p1.w1", Gen: 3, ResultID: "res-e3-p1.w1-a1-g3", Result: "ok"})
	if err == nil && rec.Accepted {
		t.Errorf("output with no receipt became an accepted receipt for %s", rec.Op)
	}
}

// Round 2 ruling (keys): "EdgeRequest, RequestID, report and take op ids all
// carry the epoch." The take verb derives its op id from the epoch
// (store.ConsumerOpID), so the take is sent with its --epoch.
func TestColdread2DefectTakeSendsNoEpochOpID(t *testing.T) {
	t.Parallel()
	var argv []string
	cli := &CLISprintClient{Run: func(_ context.Context, a []string) ([]byte, int, error) {
		argv = a
		return []byte(`{"Consumer":"p1.w1"}`), 0, nil
	}}
	_, _ = cli.Take(context.Background(), "vision.ds", "p1.w1", 4)
	if !slices.Contains(argv, "--epoch") {
		t.Errorf("take was sent as %q: no epoch, so none fences its op id", argv)
	}
}
