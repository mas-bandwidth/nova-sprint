package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Probes of the runner's and the wall's guarantees, each against the package's
// fakes: a fake sprint, a fake wall, and a fake edge.Exec in place of the
// processes OSReserveWall starts.

// wallExec stands in for the processes an OSReserveWall starts: it records
// each argv and answers every command with res.
type wallExec struct {
	mu    sync.Mutex
	argvs [][]string
	res   edge.Result
}

func (e *wallExec) Run(_ context.Context, c edge.Cmd) (edge.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.argvs = append(e.argvs, slices.Clone(c.Argv))
	return e.res, nil
}

type crSprint struct {
	mu       sync.Mutex
	next     NextResponse
	reports  []ReportRequest
	releases []string
}

func (s *crSprint) Next(context.Context, string) (NextResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, nil
}
func (s *crSprint) Take(_ context.Context, row, id string, gen int) (TakeInfo, error) {
	return TakeInfo{Consumer: id, Producer: "p", Kind: "read", Stream: "s1", Tier: "flash", Model: "m1", Deadline: 900, CommitName: testCommitName, CommitEmail: testCommitEmail, Gen: gen, Head: strings.Repeat("a", 40)}, nil
}
func (s *crSprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	return store.Receipt{Accepted: true, Op: req.ResultID}, nil
}
func (s *crSprint) Hook(context.Context, string) error   { return nil }
func (s *crSprint) Unhook(context.Context, string) error { return nil }
func (s *crSprint) Release(_ context.Context, row, id string, gen int, _, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = append(s.releases, fmt.Sprintf("%s@%d %s", id, gen, reason))
	return nil
}
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
	barrier     chan struct{} // closed when two harnesses run at once
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
	if w.barrier != nil && w.running >= 2 {
		w.barrierOnce.Do(func() { close(w.barrier) })
	}
	w.mu.Unlock()
	if w.barrier != nil {
		select {
		case <-w.barrier:
		case <-time.After(2 * time.Second): // unitlint: a hang guard, not a measurement; a serial runner never meets the barrier
		}
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	w.mu.Lock()
	w.running--
	w.mu.Unlock()
	return WallRunResult{ExitCode: 0, PID: 4242, Nonce: "n-4242"}, nil
}
func (w *crWall) RunStep(context.Context, Job, []string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
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
// its job directory": with UseSandbox, RunTest runs the card's done-when
// command as its own argv (never a shell string) inside nova-sandbox, inside a
// nova-reserve slot.
func TestCardTestsRunInsideTheSandbox(t *testing.T) {
	t.Parallel()
	ex := &wallExec{}
	w := &OSReserveWall{UseSandbox: true, Exec: ex, Config: WallConfig{ReserveBin: "/fixture/nova-reserve", SandboxBin: "/fixture/nova-sandbox", NetDeny: true}}
	dir := t.TempDir()
	res, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "make test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	argv := res.Argv
	if len(ex.argvs) != 1 || !slices.Equal(ex.argvs[0], argv) {
		t.Fatalf("the wall ran %q, reported %q", ex.argvs, argv)
	}
	argv = unniced(argv)
	if argv[0] != "/fixture/nova-reserve" || !slices.Contains(argv, "/fixture/nova-sandbox") || !slices.Equal(argv[len(argv)-3:], []string{"--", "make", "test"}) {
		t.Errorf("card test command ran as %q: want nova-reserve, then nova-sandbox, then make test", argv)
	}
}

// fifoExec is a wallExec that also looks, while the command "runs", at the
// FIFO jobserver its CARGO_MAKEFLAGS names: whether it is a named pipe and the
// tokens the worker holds in it.
type fifoExec struct {
	env    []string
	fifo   string
	isPipe bool
	tokens int
}

func (e *fifoExec) Run(_ context.Context, c edge.Cmd) (edge.Result, error) {
	e.env = slices.Clone(c.Env)
	for _, kv := range c.Env {
		if v, ok := strings.CutPrefix(kv, "CARGO_MAKEFLAGS="); ok {
			_, e.fifo, _ = strings.Cut(v, "--jobserver-auth=fifo:")
		}
	}
	if st, err := os.Lstat(e.fifo); err == nil && st.Mode()&os.ModeNamedPipe != 0 {
		e.isPipe = true
		if f, err := os.OpenFile(e.fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			buf := make([]byte, 64)
			e.tokens, _ = f.Read(buf)
			_ = f.Close()
		}
	}
	return edge.Result{}, nil
}

// A done-when gate under the strict wall gets a FIFO jobserver: cargo's
// default pipe jobserver makes std fork through socketpair(SOCK_SEQPACKET),
// which the wall's seccomp refuses, so rustc never runs. The FIFO is in the
// job's --write directory, held open by the worker with gateJobs-1 tokens,
// named by CARGO_MAKEFLAGS passed with --env, with TMPDIR in the job's tree
// passed too; the wall gains no flag (never --unix), and the FIFO is gone
// after the gate.
func TestGateGetsFIFOJobserverAndWritableTmpdir(t *testing.T) {
	t.Parallel()
	ex := &fifoExec{}
	w := &OSReserveWall{UseSandbox: true, Exec: ex, Config: WallConfig{ReserveBin: "/fixture/nova-reserve", SandboxBin: "/fixture/nova-sandbox", NetDeny: true}}
	dir := t.TempDir()
	res, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "cargo test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(res.Argv, " ")
	if !strings.Contains(argv, "--write "+dir+" ") || !strings.Contains(argv, "--env CARGO_MAKEFLAGS") || !strings.Contains(argv, "--env TMPDIR") || strings.Contains(argv, "--unix") {
		t.Errorf("wall argv %q: want --write %s, --env CARGO_MAKEFLAGS, --env TMPDIR and no --unix", argv, dir)
	}
	want := fmt.Sprintf("CARGO_MAKEFLAGS=-j%d --jobserver-auth=fifo:%s", gateJobs_internal(), ex.fifo)
	if !slices.Contains(ex.env, want) || !pathUnder_internal(ex.fifo, dir) || !ex.isPipe || ex.tokens != gateJobs_internal()-1 {
		t.Errorf("gate env %q: want %q, a named pipe under %s holding %d tokens (pipe %v, %d tokens)", ex.env, want, dir, gateJobs_internal()-1, ex.isPipe, ex.tokens)
	}
	if !slices.Contains(ex.env, "TMPDIR="+filepath.Join(dir, "tmp")) {
		t.Errorf("gate env %q: want TMPDIR in the job's --write tree", ex.env)
	}
	if _, err := os.Lstat(ex.fifo); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the gate's FIFO %s outlived the gate: %v", ex.fifo, err)
	}
}

// Guarantee 8: "A harness runs only inside a nova-reserve slot". nova-worker
// wires Wall: &worker.OSReserveWall{} (cmd/nova-worker/main.go); with no
// ReserveBin and no SandboxBin configured, RunHarness still runs the harness
// under nova-reserve and nova-sandbox.
func TestDefaultWallRunsHarnessUnderReserveAndSandbox(t *testing.T) {
	t.Parallel()
	ex := &wallExec{}
	dir := t.TempDir()
	harness := func(string) (string, error) { return "/fixture/bin/harness", nil }
	res, err := (&OSReserveWall{Exec: ex, LookPath: harness}).RunHarness(context.Background(), Job{JobDir: dir, WorktreeDir: dir}, []string{"harness", "run"}, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	argv := unniced(res.Argv)
	sandbox := slices.IndexFunc(argv, func(a string) bool { return filepath.Base(a) == "nova-sandbox" })
	if filepath.Base(argv[0]) != "nova-reserve" || sandbox < 0 || !slices.Equal(argv[len(argv)-3:], []string{"--", "harness", "run"}) {
		t.Errorf("the wall nova-worker ships ran the harness as %q: want nova-reserve, then nova-sandbox, then the harness", argv)
	}
}

// Results: a read's harness that exits non-zero and writes no result file is
// a fault of the harness (ReadOutcome), bounded by the fault limit, never a
// verdict on the head. A work job's is "fail: no verdict"
// (TestNoVerdictIsAFailForWorkAndAFaultForAReadWithTheTail).
func TestReadHarnessNonZeroExitWithNoResultIsAFault(t *testing.T) {
	t.Parallel()
	wall := &OSReserveWall{Exec: &wallExec{res: edge.Result{ExitCode: 1, Stderr: "broken\n", PID: 4242}}}
	r := crRunner_internal(t, &crSprint{}, wall)
	dir := t.TempDir()
	job := Job{Consumer: "p1.r1", Kind: "read", Row: "vision.ds", JobDir: dir, WorktreeDir: dir, HarnessName: "script"}
	res, runErr := wall.RunHarness(context.Background(), job, []string{"harness"}, nil, time.Minute)
	var rec JobRecord
	if err := r.harvestStep_internal(context.Background(), job, &rec, res, runErr); err != nil {
		t.Fatal(err)
	}
	if rec.Outcome.Result != "fault" || !rec.Outcome.NoVerdict {
		t.Errorf("read harness exited 1 with no result file: want fault, got %s (%s)", rec.Outcome.Result, rec.Outcome.Reason)
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
	// Run answers as the real nova-sprint does for a refusal: the line on
	// stdout, exit 1, and exec's error for the non-zero exit. No process runs:
	// a script written and exec'd here raced parallel forks (ETXTBSY) in CI.
	cli := &CLISprintClient{SprintBin: "nova-sprint", Run: func(ctx context.Context, argv []string) ([]byte, int, error) {
		return []byte("SPRINT REFUSED report: consumer p1.w1 is held by amd-2.ds\n"), 1, errors.New("exit status 1")
	}}
	_, err := cli.Report(context.Background(), ReportRequest{Row: "vision.ds", Consumer: "p1.w1", Gen: 3, ResultID: "res-p1.w1-g3", Result: "ok"})
	if errors.Is(err, ErrStoreDown) {
		t.Errorf("an exit-1 refusal was classified as store down (%v): the runner retries a refused report forever", err)
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

// Not a defect probe: a -race check of concurrent passes.
func TestColdreadCheckConcurrentStepRow(t *testing.T) {
	t.Parallel()
	sp := &crSprint{next: NextResponse{Row: "vision.ds", State: card.Up, Width: 2, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 1}, {ID: "p3.w1", Kind: "work", Gen: 1}}}}
	r := crRunner_internal(t, sp, &crWall{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = r.StepRow(context.Background(), "vision.ds") }()
		go func() { defer wg.Done(); _ = r.StepRow(context.Background(), "vision.ds") }()
	}
	wg.Wait()
}

// unniced drops the "nice -n N" prefix the wall puts in front of card work
// (BuildWallArgv), so the guarantees below read the wall itself.
func unniced(argv []string) []string {
	if len(argv) >= 3 && argv[0] == "nice" && argv[1] == "-n" {
		return argv[3:]
	}
	return argv
}
