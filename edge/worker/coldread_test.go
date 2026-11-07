package worker

import (
	"context"
	"errors"
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

// Cold read, 2026-11-06, at 649d1b4c (Rowan). One failing probe per confirmed defect.

type crSprint struct {
	mu      sync.Mutex
	next    NextResponse
	reports []ReportRequest
}

func (s *crSprint) Next(context.Context, string) (NextResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, nil
}
func (s *crSprint) Take(_ context.Context, row, id string, gen int) (TakeInfo, error) {
	return TakeInfo{Consumer: id, Producer: "p", Kind: "read", Stream: "s1", Tier: "flash", Model: "m1", Deadline: 900, Gen: gen, Head: strings.Repeat("a", 40)}, nil
}
func (s *crSprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	return store.Receipt{Accepted: true, Op: req.ResultID}, nil
}
func (s *crSprint) Hook(context.Context, string) error                   { return nil }
func (s *crSprint) Unhook(context.Context, string) error                 { return nil }
func (s *crSprint) OpenJudgement(context.Context, store.Judgement) error { return nil }
func (s *crSprint) reported() []ReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ReportRequest(nil), s.reports...)
}

type crWall struct {
	mu          sync.Mutex
	running     int
	maxRunning  int
	pidsSeen    []int
	barrier     chan struct{}
	barrierOnce sync.Once
}

func (w *crWall) RunHarness(_ context.Context, j Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
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
func (w *crWall) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}

func crRunner_internal(t *testing.T, sp SprintClient, wall ReserveWall) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Now: testClock_internal(), Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: sp, Wall: wall, DefaultHarness: &ScriptHarness{}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Guarantee 12: "No key or DSN is in the wall's environment". The filter is a
// substring list (harness.go:398-405); DATABASE_URL (named in Built as
// stripped) and SSH_AUTH_SOCK (the runner's push credentials) pass through to
// RunTest's environment (runner.go:729).
func TestColdreadDefectWallEnvKeepsDatabaseURLAndAgent(t *testing.T) {
	t.Parallel()
	env := SanitizeWallEnv([]string{"DATABASE_URL=postgres://sprint:pw@db/sprint", "SSH_AUTH_SOCK=/tmp/agent.sock", "PATH=/usr/bin"}, t.TempDir())
	for _, kv := range env {
		if strings.HasPrefix(kv, "DATABASE_URL=") || strings.HasPrefix(kv, "SSH_AUTH_SOCK=") {
			t.Errorf("wall environment keeps %s", kv)
		}
	}
}

// "A card can never, on a bench: reach the store or the bus ... write outside
// its job directory". RunTest runs the card's done-when lines as `sh -c
// <text>` under nova-reserve only, never nova-sandbox, even with UseSandbox
// (runner.go:724-730). /bin/echo stands in for nova-reserve and prints argv.
func TestColdreadDefectCardTestsRunOutsideSandbox(t *testing.T) {
	t.Parallel()
	w := &OSReserveWall{UseSandbox: true, Config: WallConfig{ReserveBin: "/bin/echo", SandboxBin: "nova-sandbox", NetDeny: true}}
	dir := t.TempDir()
	res, _ := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "make test", time.Minute)
	out := string(res.Output)
	if !strings.Contains(out, "nova-sandbox") {
		t.Errorf("card test command ran as %q: no sandbox wall, shell string from card text", strings.TrimSpace(out))
	}
}

// Guarantee 8: "A harness runs only inside a nova-reserve slot". nova-worker
// wires Wall: &worker.OSReserveWall{} (cmd/nova-worker/main.go:94); with no
// ReserveBin and UseSandbox false, RunHarness execs the harness bare
// (runner.go:713-722): no slot, no sandbox, no proxy.
func TestColdreadDefectShippedWallRunsHarnessBare(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := (&OSReserveWall{}).RunHarness(context.Background(), Job{JobDir: dir, WorktreeDir: dir}, []string{"/bin/echo", "harness-ran"}, nil, time.Minute)
	if err == nil && strings.TrimSpace(string(res.Output)) == "harness-ran" {
		t.Errorf("the wall nova-worker ships ran the harness directly, outside nova-reserve and nova-sandbox")
	}
}

// Results: "fail: the result file says broken, no result file". Any non-zero
// exit makes execBounded_internal return an *exec.ExitError (runner.go:768-783)
// and harvestStep_internal turns every such run into a fault (runner.go:445-447).
// A fault spends no attempt (guarantee 11), so a card that keeps failing is
// retried forever instead of bouncing.
func TestColdreadDefectNonZeroExitBecomesFault(t *testing.T) {
	t.Parallel()
	r := crRunner_internal(t, &crSprint{}, &OSReserveWall{})
	dir := t.TempDir()
	job := Job{Consumer: "p1.r1", Kind: "read", Row: "vision.ds", JobDir: dir, WorktreeDir: dir, HarnessName: "script"}
	res, runErr := (&OSReserveWall{}).RunHarness(context.Background(), job, []string{"sh", "-c", "echo broken >&2; exit 1"}, nil, time.Minute)
	var rec JobRecord
	if err := r.harvestStep_internal(context.Background(), job, &rec, res, runErr); err != nil {
		t.Fatal(err)
	}
	if rec.Outcome.Result != "fail" {
		t.Errorf("harness exited 1 with no result file: want fail, got %s (%s)", rec.Outcome.Result, rec.Outcome.Reason)
	}
}

// Guarantee 7 and the restart rule ("adopted by pid and nonce"). The pid is
// written to job.json only after RunHarness returns (runner.go:417-424), i.e.
// after the harness exited. While it runs, job.json says running with pid 0, so
// a restarted runner cannot adopt it and reports it as gone.
func TestColdreadDefectPidNotRecordedWhileHarnessRuns(t *testing.T) {
	t.Parallel()
	sp := &crSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}}
	w := &crWall{}
	r := crRunner_internal(t, sp, w)
	if err := r.RunConsumer(context.Background(), sp.next, sp.next.Working[0]); err != nil {
		t.Fatal(err)
	}
	if len(w.pidsSeen) == 0 || w.pidsSeen[0] == 0 {
		t.Errorf("job.json during the harness run held pid %v: restart cannot adopt a live harness", w.pidsSeen)
	}
}

// Design runner step 8: "retry the verb with that id until it exits 0 or 1".
// main's Run returns exec's error for any non-zero exit (cmd/nova-worker/main.go:74-80)
// and CLISprintClient treats err != nil as the store down (verbs.go:441), so a
// refusal (exit 1) is retried forever instead of ending the retry.
func TestColdreadDefectCLIRefusalReadAsStoreDown(t *testing.T) {
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

// Seam ruling 3: "Every edge answers requests as pure queries of the world".
// QueryEdge runs RecoverRow first (runner.go:586), which reports, kills and
// deletes job directories.
func TestColdreadDefectQueryEdgeHasSideEffects(t *testing.T) {
	t.Parallel()
	sp := &crSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 3}}}}
	r := crRunner_internal(t, sp, &crWall{})
	if _, err := r.QueryEdge(context.Background(), EdgeRequest{Kind: "runner", Card: "p1.w1", Row: "vision.ds"}); err != nil {
		t.Fatal(err)
	}
	if got := sp.reported(); len(got) > 0 {
		t.Errorf("a query wrote a store op: report %s result=%s reason=%q", got[0].ResultID, got[0].Result, got[0].Reason)
	}
}

// Design item 1, "never a shell string". The script harness, nova-worker's
// default (--harness script, main.go:28; and runner.go:113 when no name), builds
// `sh -c` from the card's prose with Go %q quoting (harness.go:118-121, 176-179),
// which a shell re-expands: $(...) in a brief runs.
func TestColdreadDefectScriptHarnessShellInjectionFromProse(t *testing.T) {
	t.Parallel()
	argv, _ := (&ScriptHarness{}).Command(Job{Consumer: "p1.w1", Row: "vision.ds", JobDir: t.TempDir(), Prose: "commit:f.txt:$(touch /tmp/cold)"})
	body := argv[len(argv)-1]
	if argv[0] == "sh" && strings.Contains(body, `"$(touch /tmp/cold)"`) {
		t.Errorf("card prose reached sh -c inside double quotes: %q", body)
	}
}

// Design: "width caps a row's working consumers"; the runner takes each ready
// consumer up to width. StepRow runs RunConsumer inline (runner.go:313-321) and
// main's wake loop is one goroutine (main.go:129-132): width 2 runs one harness
// at a time, and a WITHDRAWN wake cannot reach a running harness.
func TestColdreadDefectWidthTwoRunsSerially(t *testing.T) {
	t.Parallel()
	sp := &crSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 2, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 1}, {ID: "p2.r1", Kind: "read", Gen: 1}}}}
	w := &crWall{barrier: make(chan struct{})}
	r := crRunner_internal(t, sp, w)
	if err := r.StepRow(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	if w.maxRunning < 2 {
		t.Errorf("width 2 with 2 consumers in hand ran at most %d harness at once", w.maxRunning)
	}
}

// Ruling: "timeouts from policy with measured defaults". The runner's git
// path (OSGitOps, runGit_internal at runner.go:696-706) execs git with no
// timeout at all and outside edge.Exec; Timeouts.Push is never read (nor GH,
// BusSend, SSHConnect, CIWaitExtra, LaunchBackoff*Max).
func TestColdreadDefectRunnerGitHasNoTimeout(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(OSGitOps{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type == reflect.TypeOf(time.Duration(0)) || typ.Field(i).Type == reflect.TypeOf(Timeouts{}) {
			return
		}
	}
	t.Errorf("OSGitOps (push, fetch, rebase, ls-remote on the bench) carries no timeout; a hung push blocks the runner forever")
}

// Not a defect probe: a -race check of wake work against a concurrent query.
func TestColdreadCheckConcurrentStepRowAndQueryEdge(t *testing.T) {
	t.Parallel()
	sp := &crSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 2, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 1}, {ID: "p3.w1", Kind: "work", Gen: 1}}}}
	r := crRunner_internal(t, sp, &crWall{})
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
