package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Sandbox, environment, outcome-classification and query guarantees of the
// runner and the process wall. Unit tier: fake sprint, fake wall, fake exec.

type sandboxSprint struct {
	mu       sync.Mutex
	next     NextResponse
	reports  []ReportRequest
	releases []string
}

func (s *sandboxSprint) Next(context.Context, string) (NextResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, nil
}
func (s *sandboxSprint) Take(_ context.Context, row, id string, gen int) (TakeInfo, error) {
	return TakeInfo{Consumer: id, Producer: "p", Kind: "read", Stream: "s1", Tier: "flash", Model: "m1", Deadline: 900, CommitName: testCommitName, CommitEmail: testCommitEmail, Gen: gen, Head: strings.Repeat("a", 40)}, nil
}
func (s *sandboxSprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	return store.Receipt{Accepted: true, Op: req.ResultID}, nil
}
func (s *sandboxSprint) Hook(context.Context, string) error                   { return nil }
func (s *sandboxSprint) Unhook(context.Context, string) error                 { return nil }
func (s *sandboxSprint) OpenJudgement(context.Context, store.Judgement) error { return nil }
func (s *sandboxSprint) Release(_ context.Context, row, id string, gen int, _, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = append(s.releases, fmt.Sprintf("%s@%d %s", id, gen, reason))
	return nil
}
func (s *sandboxSprint) reported() []ReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ReportRequest(nil), s.reports...)
}

type sandboxWall struct {
	mu          sync.Mutex
	running     int
	maxRunning  int
	pidsSeen    []int
	barrier     chan struct{}
	barrierOnce sync.Once
}

func (w *sandboxWall) RunHarness(_ context.Context, j Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.running++
	if w.running > w.maxRunning {
		w.maxRunning = w.running
	}
	if rec, err := ReadJobRecord(j.JobDir); err == nil {
		w.pidsSeen = append(w.pidsSeen, rec.PID)
	}
	w.mu.Unlock()
	if w.barrier != nil {
		w.barrierOnce.Do(func() {
			select {
			case <-w.barrier:
			case <-time.After(2 * time.Second): // bound only; a serial runner never meets the barrier
			}
		})
		select {
		case w.barrier <- struct{}{}:
		default:
		}
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	w.mu.Lock()
	w.running--
	w.mu.Unlock()
	return WallRunResult{ExitCode: 0, PID: 4242, Nonce: "n-4242"}, nil
}
func (w *sandboxWall) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}

func sandboxRunner_internal(t *testing.T, sp SprintClient, wall ReserveWall) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Now: testClock_internal(), Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: sp, Wall: wall, DefaultHarness: &ScriptHarness{}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// No key or DSN is in the wall's environment. DATABASE_URL and SSH_AUTH_SOCK
// (the runner's push credentials) must not pass through to RunTest's
// environment.
func TestWallEnvKeepsDatabaseURLAndAgent(t *testing.T) {
	t.Parallel()
	env := SanitizeWallEnv([]string{"DATABASE_URL=postgres://sprint:pw@db/sprint", "SSH_AUTH_SOCK=/tmp/agent.sock", "PATH=/usr/bin"}, t.TempDir())
	for _, kv := range env {
		if strings.HasPrefix(kv, "DATABASE_URL=") || strings.HasPrefix(kv, "SSH_AUTH_SOCK=") {
			t.Errorf("wall environment keeps %s", kv)
		}
	}
}

// A card can never, on a bench, reach the store or the bus or write outside
// its job directory. RunTest must run the card's done-when lines under
// nova-sandbox when UseSandbox is set, not under nova-reserve alone.
// /bin/echo stands in for nova-reserve and prints argv.
func TestCardTestsRunInsideSandbox(t *testing.T) {
	t.Parallel()
	w := &OSReserveWall{UseSandbox: true, Config: WallConfig{ReserveBin: "/bin/echo", SandboxBin: "nova-sandbox", NetDeny: true}}
	dir := t.TempDir()
	res, _ := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "make test", time.Minute)
	out := string(res.Output)
	if !strings.Contains(out, "nova-sandbox") {
		t.Errorf("card test command ran as %q: no sandbox wall, shell string from card text", strings.TrimSpace(out))
	}
}

// A harness runs only inside a nova-reserve slot. The wall nova-worker wires
// (a bare &OSReserveWall{}) must not exec the harness directly: no slot, no
// sandbox, no proxy.
func TestShippedWallRunsHarnessInsideReserve(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := (&OSReserveWall{}).RunHarness(context.Background(), Job{JobDir: dir, WorktreeDir: dir}, []string{"/bin/echo", "harness-ran"}, nil, time.Minute)
	if err == nil && strings.TrimSpace(string(res.Output)) == "harness-ran" {
		t.Errorf("the wall nova-worker ships ran the harness directly, outside nova-reserve and nova-sandbox")
	}
}

// A non-zero exit is judged by the result file: "fail" when it says broken or
// there is none for a work job. Treating every such run as a fault would spend
// no attempt, and a card that keeps failing would be retried forever instead
// of bouncing.
func TestNonZeroExitBecomesFault(t *testing.T) {
	t.Parallel()
	r := sandboxRunner_internal(t, &sandboxSprint{}, &OSReserveWall{})
	dir := t.TempDir()
	job := Job{Consumer: "p1.r1", Kind: "read", Row: "vision.ds", JobDir: dir, WorktreeDir: dir, HarnessName: "script"}
	res, runErr := (&OSReserveWall{}).RunHarness(context.Background(), job, []string{"sh", "-c", "echo broken >&2; exit 1"}, nil, time.Minute)
	var rec JobRecord
	if err := r.harvestStep_internal(context.Background(), job, &rec, res, runErr); err != nil {
		t.Fatal(err)
	}
	// A read reports ok, fix or fault (ReadOutcome): a read's
	// harness that exits with no verdict is a fault of the harness, bounded
	// by the fault limit, never a verdict on the head. A work job's is
	// "fail: no verdict" (TestNoVerdictIsReadFaultAndWorkFail).
	if rec.Outcome.Result != "fault" || !rec.Outcome.NoVerdict {
		t.Errorf("read harness exited 1 with no result file: want fault, got %s (%s)", rec.Outcome.Result, rec.Outcome.Reason)
	}
}

// A live harness is adopted by pid and nonce on restart. The pid must be
// written to job.json while the harness runs, not after RunHarness returns;
// otherwise job.json says running with pid 0, a restarted runner cannot adopt
// it and reports it as gone.
func TestPidRecordedWhileHarnessRuns(t *testing.T) {
	t.Parallel()
	sp := &sandboxSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	w := &sandboxWall{}
	r := sandboxRunner_internal(t, sp, w)
	if err := r.RunConsumer(context.Background(), sp.next, sp.next.Working[0]); err != nil {
		t.Fatal(err)
	}
	if len(w.pidsSeen) == 0 || w.pidsSeen[0] == 0 {
		t.Errorf("job.json during the harness run held pid %v: restart cannot adopt a live harness", w.pidsSeen)
	}
}

// A verb is retried with its id until it exits 0 or 1. A refusal (exit 1)
// must not be classified as the store being down, or it is retried forever
// instead of ending the retry.
func TestCLIRefusalIsNotStoreDown(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'SPRINT REFUSED report: consumer p1.w1 is held by amd-2.ds'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cli := &CLISprintClient{SprintBin: bin, Run: func(ctx context.Context, argv []string) ([]byte, int, error) {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		out, err := cmd.CombinedOutput()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return out, code, err
	}}
	_, err := cli.Report(context.Background(), ReportRequest{Row: "vision.ds", Consumer: "p1.w1", Gen: 3, ResultID: "res-p1.w1-g3", Result: "ok"})
	if errors.Is(err, ErrStoreDown) {
		t.Errorf("an exit-1 refusal was classified as store down (%v): the runner retries a refused report forever", err)
	}
}

// Every edge answers requests as pure queries of the world. QueryEdge must
// not run recovery, which reports, kills and deletes job directories.
func TestQueryEdgeHasNoSideEffects(t *testing.T) {
	t.Parallel()
	sp := &sandboxSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 3}}}}
	r := sandboxRunner_internal(t, sp, &sandboxWall{})
	if _, err := r.QueryEdge(context.Background(), EdgeRequest{Kind: "runner", Card: "p1.w1", Row: "vision.ds"}); err != nil {
		t.Fatal(err)
	}
	if got := sp.reported(); len(got) > 0 {
		t.Errorf("a query wrote a store op: report %s result=%s reason=%q", got[0].ResultID, got[0].Result, got[0].Reason)
	}
}

// Card prose is never a shell string. The script harness, nova-worker's
// default, must not build `sh -c` from the card's prose with Go %q quoting,
// which a shell re-expands: $(...) in a brief would run.
func TestScriptHarnessNeverExpandsProseInShell(t *testing.T) {
	t.Parallel()
	argv, _ := (&ScriptHarness{}).Command(Job{Consumer: "p1.w1", Row: "vision.ds", JobDir: t.TempDir(), Prose: "commit:f.txt:$(touch /tmp/cold)"})
	body := argv[len(argv)-1]
	if argv[0] == "sh" && strings.Contains(body, `"$(touch /tmp/cold)"`) {
		t.Errorf("card prose reached sh -c inside double quotes: %q", body)
	}
}

// Width caps a row's working consumers, and the runner takes each ready
// consumer up to width: width 2 runs two harnesses at once (a serial StepRow
// would also keep a WITHDRAWN wake from reaching a running harness).
func TestWidthTwoRunsConcurrently(t *testing.T) {
	t.Parallel()
	sp := &sandboxSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 2, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 1}, {ID: "p2.r1", Kind: "read", Gen: 1}}}}
	w := &sandboxWall{barrier: make(chan struct{})}
	r := sandboxRunner_internal(t, sp, w)
	if err := r.StepRow(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	if w.maxRunning < 2 {
		t.Errorf("width 2 with 2 consumers in hand ran at most %d harness at once", w.maxRunning)
	}
}

// Timeouts come from policy. The runner's git path (OSGitOps) must carry a
// timeout, or a hung push blocks the runner forever.
func TestRunnerGitHasATimeout(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(OSGitOps{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type == reflect.TypeOf(time.Duration(0)) || typ.Field(i).Type == reflect.TypeOf(Timeouts{}) {
			return
		}
	}
	t.Errorf("OSGitOps (push, fetch, rebase, ls-remote on the bench) carries no timeout; a hung push blocks the runner forever")
}

// A -race check of wake work against a concurrent query.
func TestCheckConcurrentStepRowAndQueryEdge(t *testing.T) {
	t.Parallel()
	sp := &sandboxSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 2, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 1}, {ID: "p3.w1", Kind: "work", Gen: 1}}}}
	r := sandboxRunner_internal(t, sp, &sandboxWall{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = r.StepRow(context.Background(), "vision.ds") }()
		go func() {
			defer wg.Done()
			_, _ = r.QueryEdge(context.Background(), EdgeRequest{Kind: "runner", Card: "p3.w1", Row: "vision.ds"})
		}()
	}
	wg.Wait()
}
