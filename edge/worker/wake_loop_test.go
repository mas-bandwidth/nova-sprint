package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// The worker takes deadline, model and forge from the take receipt, holds no clock of its own, hooks in before it
// acts, and keeps a report until the sprint acknowledges it. Everything here
// is unit tier: a fake edge.Exec, a fake clock, a fake hook, no process.

// ---- fakes ----------------------------------------------------------------

type fakeWakeHook struct {
	wake    chan struct{}
	done    chan struct{}
	err     error
	stopped int
	mu      sync.Mutex
	dead    sync.Once
}

func newFakeWakeHook() *fakeWakeHook {
	return &fakeWakeHook{wake: make(chan struct{}), done: make(chan struct{})}
}
func (h *fakeWakeHook) Wake() <-chan struct{} { return h.wake }
func (h *fakeWakeHook) Done() <-chan struct{} { return h.done }
func (h *fakeWakeHook) Err() error            { h.mu.Lock(); defer h.mu.Unlock(); return h.err }
func (h *fakeWakeHook) Stop()                 { h.mu.Lock(); h.stopped++; h.mu.Unlock() }
func (h *fakeWakeHook) die(err error) {
	h.dead.Do(func() { h.mu.Lock(); h.err = err; h.mu.Unlock(); close(h.done) })
}
func (h *fakeWakeHook) stops() int { h.mu.Lock(); defer h.mu.Unlock(); return h.stopped }

// loopSprint records every verb the worker runs, in order.
type loopSprint struct {
	mu         sync.Mutex
	events     []string
	next       NextResponse
	take       TakeInfo
	unhookErr  error
	hookErr    error
	reportErrs []error // one per Report call, in order; then accepted
	reports    []ReportRequest
	gate       chan struct{} // the first Next waits for it to close
	gateOnce   sync.Once
	nexts      chan struct{}
}

func (s *loopSprint) note(e string) { s.mu.Lock(); s.events = append(s.events, e); s.mu.Unlock() }
func (s *loopSprint) log() []string { s.mu.Lock(); defer s.mu.Unlock(); return slices.Clone(s.events) }
func (s *loopSprint) Unhook(context.Context, string) error {
	s.note("unhook")
	return s.unhookErr
}
func (s *loopSprint) Hook(context.Context, string) error {
	s.note("hook")
	return s.hookErr
}
func (s *loopSprint) Next(context.Context, string) (NextResponse, error) {
	s.note("next")
	if s.nexts != nil {
		s.nexts <- struct{}{}
	}
	if s.gate != nil {
		s.gateOnce.Do(func() { <-s.gate })
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, nil
}
func (s *loopSprint) Take(_ context.Context, _, id string, gen int) (TakeInfo, error) {
	s.note("take")
	t := s.take
	t.Consumer, t.Gen = id, gen
	return t, nil
}
func (s *loopSprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.note("report")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	if n := len(s.reports); n <= len(s.reportErrs) && s.reportErrs[n-1] != nil {
		return store.Receipt{}, s.reportErrs[n-1]
	}
	// as the store: an accepted report takes the consumer out of the hand
	s.next.Working = slices.DeleteFunc(slices.Clone(s.next.Working), func(c ConsumerSummary) bool { return c.ID == req.Consumer })
	return store.Receipt{Accepted: true, Op: req.ResultID}, nil
}
func (s *loopSprint) OpenJudgement(context.Context, store.Judgement) error { return nil }
func (s *loopSprint) Release(_ context.Context, _, id string, gen int, _, _ string) error {
	s.note(fmt.Sprintf("release %s@%d", id, gen))
	return nil
}
func (s *loopSprint) reported() []ReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reports)
}

func loopRunner_internal(t *testing.T, sp SprintClient, hook *fakeWakeHook, wall ReserveWall) (*Runner, *[]string) {
	t.Helper()
	var order []string
	var mu sync.Mutex
	r, err := NewRunner(Runner{
		Now: testClock_internal(), Sleep: noSleep_internal,
		Host: "vision", RootDir: t.TempDir(), Sprint: sp, Wall: wall, DefaultHarness: &ScriptHarness{},
		BusHook: func(_ context.Context, row string) (WakeHook, error) {
			mu.Lock()
			order = append(order, "bus:"+row)
			mu.Unlock()
			if ls, ok := sp.(*loopSprint); ok {
				ls.note("bus")
			}
			return hook, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, &order
}

func readyRead_internal() NextResponse {
	return NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1, Harness: "script", Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}
}

var okReadReceipt_internal = TakeInfo{Producer: "p1", Kind: "read", Stream: "s1", Tier: "flash", Epoch: 3, Attempt: 1, Head: strings.Repeat("a", 40), Model: "m1", Deadline: 900, CommitName: testCommitName, CommitEmail: testCommitEmail}

// ---- row 20: startup order and the wake loop -------------------------------

func TestStartupOrderIsBusUnhookHookThenNext(t *testing.T) {
	t.Parallel()
	hook := newFakeWakeHook()
	sp := &loopSprint{next: NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1}, unhookErr: fmt.Errorf("%w: vision.ds is not hooked", ErrRefused), nexts: make(chan struct{}, 8)}
	r, _ := loopRunner_internal(t, sp, hook, &sandboxWall{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.RunWakeLoop(ctx, "vision.ds") }()
	<-sp.nexts // the first pass has started: everything before it has run
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunWakeLoop on ctx end: %v", err)
	}
	got := sp.log()
	if len(got) < 4 || !slices.Equal(got[:4], []string{"bus", "unhook", "hook", "next"}) {
		t.Fatalf("startup order = %v, want bus, unhook, hook, next", got)
	}
	if hook.stops() != 1 {
		t.Fatalf("the bus hook was stopped %d times, want once", hook.stops())
	}
}

func TestNothingActsBeforeHookingIn(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ unhook, hook error }{
		"unhook fails with the store down": {unhook: fmt.Errorf("%w: exit 3", ErrStoreDown)},
		"hook refused":                     {hook: fmt.Errorf("%w: hook in to the bus first", ErrRefused)},
	} {
		hook := newFakeWakeHook()
		sp := &loopSprint{next: readyRead_internal(), take: okReadReceipt_internal, unhookErr: tc.unhook, hookErr: tc.hook}
		r, _ := loopRunner_internal(t, sp, hook, &sandboxWall{})
		if err := r.RunWakeLoop(context.Background(), "vision.ds"); err == nil {
			t.Fatalf("%s: RunWakeLoop returned nil", name)
		}
		for _, e := range sp.log() {
			if e == "next" || e == "take" || e == "report" {
				t.Fatalf("%s: %s ran before the row was hooked in: %v", name, e, sp.log())
			}
		}
		if hook.stops() != 1 {
			t.Fatalf("%s: the bus hook was left running", name)
		}
	}
}

func TestWorkerExitsWithAnErrorWhenTheBusHookDies(t *testing.T) {
	t.Parallel()
	hook := newFakeWakeHook()
	sp := &loopSprint{next: NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1}, nexts: make(chan struct{}, 8)}
	r, _ := loopRunner_internal(t, sp, hook, &sandboxWall{})
	done := make(chan error, 1)
	go func() { done <- r.RunWakeLoop(context.Background(), "vision.ds") }()
	<-sp.nexts
	hook.die(errors.New("nova-bus hook --as vision.ds ended (exit status 1): redis: connection refused"))
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "redis: connection refused") {
		t.Fatalf("want the hook's own error, got %v", err)
	}
	if hook.stops() != 1 {
		t.Fatalf("the dead hook was not stopped")
	}
}

func TestWakesWhileAPassRunsCoalesceIntoOnePass(t *testing.T) {
	t.Parallel()
	hook := newFakeWakeHook()
	sp := &loopSprint{next: NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1}, gate: make(chan struct{}), nexts: make(chan struct{}, 32)}
	r, _ := loopRunner_internal(t, sp, hook, &sandboxWall{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.RunWakeLoop(ctx, "vision.ds") }()
	<-sp.nexts // pass one is inside its first Next
	for range 5 {
		hook.wake <- struct{}{} // each send returns only when the loop took it
	}
	close(sp.gate)
	for range 3 { // pass one's second Next, then pass two's two
		<-sp.nexts
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range sp.log() {
		if e == "next" {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("5 wakes during a pass ran %d Next calls, want 4 (two passes of two)", n)
	}
}

func TestStepRowDoesNotHookIn(t *testing.T) {
	t.Parallel()
	sp := &loopSprint{next: NextResponse{Epoch: 3, Row: "vision.ds", State: card.Up, Width: 1}}
	r, _ := loopRunner_internal(t, sp, newFakeWakeHook(), &sandboxWall{})
	if err := r.StepRow(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	for _, e := range sp.log() {
		if e == "hook" || e == "unhook" || e == "bus" {
			t.Fatalf("a pass hooked in: %v", sp.log())
		}
	}
}

func TestSprintHookVerbsGoThroughExec(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	fx.On("nova-sprint", func(c edge.FakeCall) edge.FakeOutcome {
		if slices.Contains(c.Cmd.Argv, "unhook") {
			return edge.FakeOutcome{ExitCode: 1, Stderr: "UNHOOK REFUSED vision.ds is not hooked"}
		}
		return edge.FakeOutcome{}
	})
	cli := &CLISprintClient{Exec: fx, SprintBin: "nova-sprint"}
	if err := cli.Unhook(context.Background(), "vision.ds"); !errors.Is(err, ErrRefused) {
		t.Fatalf("a refused unhook is ErrRefused (nothing stale), got %v", err)
	}
	if err := cli.Hook(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	var argv []string
	for _, c := range fx.Calls() {
		argv = append(argv, strings.Join(c.Cmd.Argv, " "))
	}
	want := []string{"nova-sprint unhook --as vision.ds", "nova-sprint hook --as vision.ds --role worker"}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
}

// ---- row 20: durable reports ------------------------------------------------

func TestUnacknowledgedReportIsKeptAndSentOnTheNextWake(t *testing.T) {
	t.Parallel()
	// perPass: a store that is down is tried three times inside one pass; a
	// refusal is one try. Either way the record stays until exit 0.
	for name, tc := range map[string]struct {
		failure error
		perPass int
	}{
		"store down":  {fmt.Errorf("%w: exit 3", ErrStoreDown), 3},
		"sprint said": {fmt.Errorf("%w: consumer p1.r1 is held by amd-2.ds", ErrRefused), 1},
	} {
		sp := &loopSprint{next: readyRead_internal(), take: okReadReceipt_internal}
		for range 3 * tc.perPass {
			sp.reportErrs = append(sp.reportErrs, tc.failure)
		}
		r, _ := loopRunner_internal(t, sp, newFakeWakeHook(), &sandboxWall{})
		r.RefusalLimit = 10 // the resend alone: quarantine at the limit is TestRefusedRecordIsQuarantinedAndThePassGoesOn
		root := r.RootDir
		_ = r.StepRow(context.Background(), "vision.ds")
		rec, err := ReadJobRecord(filepath.Join(root, "jobs", "p1.r1"))
		if err != nil || rec.State != JobReporting || rec.Outcome.Result != "ok" {
			t.Fatalf("%s: a report the sprint did not acknowledge must stay on disk at state reporting, got %+v, %v", name, rec, err)
		}
		// The sprint lists the consumer as working: each wake sends it again, same id.
		sp.mu.Lock()
		sp.next.Ready, sp.next.Working = nil, []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2, Attempt: 1}}
		sp.mu.Unlock()
		_ = r.StepRow(context.Background(), "vision.ds")
		_ = r.StepRow(context.Background(), "vision.ds")
		reps := sp.reported()
		if len(reps) != 3*tc.perPass {
			t.Fatalf("%s: want %d sends over three wakes, got %d", name, 3*tc.perPass, len(reps))
		}
		for _, rep := range reps {
			if rep.ResultID != reps[0].ResultID {
				t.Fatalf("%s: a resend changed the result id: %q then %q", name, reps[0].ResultID, rep.ResultID)
			}
		}
		// Acknowledged at last: exit 0 clears the record, and nothing is sent again.
		_ = r.StepRow(context.Background(), "vision.ds")
		if _, err := os.Stat(filepath.Join(root, "jobs", "p1.r1")); !os.IsNotExist(err) {
			t.Fatalf("%s: the acknowledged report left its job directory: %v", name, err)
		}
		_ = r.StepRow(context.Background(), "vision.ds")
		if n := len(sp.reported()); n != 3*tc.perPass+1 {
			t.Fatalf("%s: %d sends, want %d", name, n, 3*tc.perPass+1)
		}
	}
}

// ---- rows 14, 15: what the receipt carries ----------------------------------

func TestReceiptWithoutModelDeadlineForgeIsRefusedNamingTheFix(t *testing.T) {
	t.Parallel()
	cli := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) {
		return []byte(`{"consumer":"p1.w1","producer":"p1","kind":"work","stream":"s1","tier":"pro","gen":4}`), 0, nil
	}}
	_, err := cli.Take(context.Background(), "vision.ds", "p1.w1", 4)
	if !errors.Is(err, ErrReceipt) {
		t.Fatalf("want ErrReceipt, got %v", err)
	}
	for _, want := range []string{"model, deadline, forge", "nova-sprint take", "--json"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("the fix is one line, got %q", err)
	}
}

func TestReceiptWithAnEmptyForgeIsAHandStream(t *testing.T) {
	t.Parallel()
	cli := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) {
		return []byte(`{"consumer":"p1.w1","model":"claude-x","deadline":2700,"forge":"","commit_name":"Sprint Bot","commit_email":"1+sprint-bot@users.noreply.example.test"}`), 0, nil
	}}
	info, err := cli.Take(context.Background(), "vision.ds", "p1.w1", 4)
	if err != nil || info.Model != "claude-x" || info.Deadline != 2700 || info.Forge != "" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestRefusedReceiptFaultsTheCardAndEndsTheWorker(t *testing.T) {
	t.Parallel()
	old := okReadReceipt_internal
	old.Model, old.Deadline = "", 0
	hook := newFakeWakeHook()
	sp := &loopSprint{next: readyRead_internal(), take: old}
	r, _ := loopRunner_internal(t, sp, hook, &sandboxWall{})
	err := r.RunWakeLoop(context.Background(), "vision.ds")
	if !errors.Is(err, ErrReceipt) {
		t.Fatalf("want ErrReceipt to end the worker, got %v", err)
	}
	reps := sp.reported()
	if len(reps) != 1 || reps[0].Result != "fault" || !strings.Contains(reps[0].Reason, "nova-sprint take") {
		t.Fatalf("the taken card must be reported fault with the fix, got %+v", reps)
	}
	if hook.stops() != 1 {
		t.Fatal("the bus hook was left running")
	}
}

type loopWall struct {
	fakeWall
	timeout time.Duration
	job     Job
	rec     JobRecord
	argv    []string
}

func (w *loopWall) RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error) {
	rec, _ := ReadJobRecord(j.JobDir)
	w.mu.Lock()
	w.timeout, w.job, w.rec, w.argv = timeout, j, rec, slices.Clone(argv)
	w.mu.Unlock()
	return w.fakeWall.RunHarness(ctx, j, argv, env, timeout)
}

func TestDeadlineModelAndForgeComeFromTheReceipt(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	git.rebasedSHA = testRebSHA
	wall := &loopWall{}
	wall.runFn = func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		git.mu.Lock()
		git.headByWT[j.WorktreeDir] = testWorkSHA
		git.mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{ExitCode: 0, PID: 500, Nonce: "n5", Elapsed: time.Second}, nil
	}
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	r, err := NewRunner(Runner{
		Host: "b1", RootDir: t.TempDir(), Sprint: sc, Git: git,
		Wall: wall, DefaultHarness: &OpenCodeHarness{}, Now: clock, Sleep: noSleep_internal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StepRow(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	wall.mu.Lock()
	defer wall.mu.Unlock()
	if want := time.Duration(fakeTierDeadline) * time.Second; wall.timeout != want {
		t.Errorf("the harness ran with timeout %s, the receipt said %s", wall.timeout, want)
	}
	if wall.rec.DeadlineAt-wall.rec.StartedAt != fakeTierDeadline {
		t.Errorf("job.json deadline is %ds after the start, the receipt said %d", wall.rec.DeadlineAt-wall.rec.StartedAt, fakeTierDeadline)
	}
	if wall.job.Model != "deepseek-r1" || !slices.Contains(wall.argv, "deepseek-r1") {
		t.Errorf("the model is the receipt's: job %q argv %q", wall.job.Model, wall.argv)
	}
	// The forge is the receipt's and only names the clone URL (CloneURL):
	// the worker opens no PR; the landing does (edge/git
	// TestLandRungOneGatesTheCardsOwnPR).
	if wall.job.Forge != "github" || wall.rec.Forge != "github" {
		t.Errorf("the forge is the receipt's: job %q record %q", wall.job.Forge, wall.rec.Forge)
	}
}

type recordingSprint struct {
	*StoreSprintClient
	reports []ReportRequest
}

func (s *recordingSprint) Report(ctx context.Context, req ReportRequest) (store.Receipt, error) {
	s.reports = append(s.reports, req)
	return s.StoreSprintClient.Report(ctx, req)
}

func TestHarnessAdaptersHoldNoModel(t *testing.T) {
	t.Parallel()
	for _, h := range []Harness{&ClaudeHarness{}, &CodexHarness{}, &OpenCodeHarness{}} {
		argv, _ := h.Command(Job{JobDir: t.TempDir()})
		if slices.Contains(argv, "--model") {
			t.Errorf("%s names a model of its own for a job with none: %q", h.Name(), argv)
		}
		argv, _ = h.Command(Job{JobDir: t.TempDir(), Model: "m-from-receipt"})
		if i := slices.Index(argv, "--model"); i < 0 || argv[i+1] != "m-from-receipt" {
			t.Errorf("%s did not pass the receipt's model: %q", h.Name(), argv)
		}
	}
}

// ---- row 14: the clock is injected ------------------------------------------

func TestRunnerRefusesToStartWithoutAnInjectedClock(t *testing.T) {
	t.Parallel()
	_, err := NewRunner(Runner{Host: "vision", RootDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "Now and Sleep") {
		t.Fatalf("a runner with no clock must be refused naming Now and Sleep, got %v", err)
	}
	r, err := NewRunner(Runner{Host: "vision", RootDir: t.TempDir(), Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	if a, b := r.launchBackoff_internal(1), r.launchBackoff_internal(1); a == b {
		t.Errorf("launch backoff is not jittered: %s twice", a)
	}
}

// ---- rows 14, 15: no literal in the worker ----------------------------------

func TestNoDeadlineModelHostOrTierLiteralsInTheWorker(t *testing.T) {
	t.Parallel()
	banned := regexp.MustCompile(`studio|api\.(openai|deepseek|anthropic)\.com|claude-(sonnet|opus|haiku)|gpt-[0-9]|deepseek-(r1|reasoner|chat)|"(flash|pro|heavy|frontier)"|TierDeadline|DeadlineFor`)
	for _, glob := range []string{"*.go", "../../cmd/nova-worker/*.go"} {
		files, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				if m := banned.FindString(line); m != "" {
					t.Errorf("%s:%d holds %q: deadlines, models, hosts and tiers come from the take receipt or a flag", f, i+1, m)
				}
			}
		}
	}
}
