package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// The done-when gates of a work job (runner.go rebaseAndTestStep_internal),
// 2026-11-11: each ran under the worker's two-minute step bound, which the
// card contract never stated, so a gate of 524 s was killed on every bench,
// reported as a fault that "did not run", and dealt again. A gate runs until
// the card's deadline, the one the deal carries; and a red gate's reason
// keeps the lines that say what failed, not only the output's end.

// gateWall_internal records the timeout of every RunTest and RunStep and
// answers each done-when from run (nil: exit 0), which may move the test's
// clock: no gate here takes real time.
type gateWall_internal struct {
	cr2Wall
	mu           sync.Mutex
	tests, steps []time.Duration
	run          func(cmd string, timeout time.Duration) (WallRunResult, error)
}

func (w *gateWall_internal) RunTest(_ context.Context, _ Job, cmd string, timeout time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.tests = append(w.tests, timeout)
	run := w.run
	w.mu.Unlock()
	if run == nil {
		return WallRunResult{}, nil
	}
	return run(cmd, timeout)
}

func (w *gateWall_internal) RunStep(_ context.Context, _ Job, _ []string, timeout time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.steps = append(w.steps, timeout)
	w.mu.Unlock()
	return WallRunResult{}, nil
}

// gateClock_internal is a clock the test moves; it starts where the test
// says (never 0) and reads no real time.
type gateClock_internal struct {
	mu  sync.Mutex
	sec int64
}

func (c *gateClock_internal) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Unix(c.sec, 0).UTC()
}

func (c *gateClock_internal) add(d time.Duration) {
	c.mu.Lock()
	c.sec += int64(d / time.Second)
	c.mu.Unlock()
}

// gateStep_internal runs the stream merge and done-when step of rec, a
// harvested ok work job taken at 1000, on wall at clock, and returns the
// record it left.
func gateStep_internal(t *testing.T, wall ReserveWall, clock *gateClock_internal, rec JobRecord) JobRecord {
	t.Helper()
	r, err := NewRunner(Runner{Now: clock.now, Sleep: noSleep_internal, Host: "vision", RootDir: t.TempDir(), Sprint: &cr2Sprint{}, Wall: wall, Git: newFakeGit_internal(), DefaultHarness: &ScriptHarness{}, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	rec.Consumer, rec.Kind, rec.Row, rec.Repo, rec.Forge, rec.Gen = "p1.w1", "work", "vision.ds", "o/r", "github", 2
	rec.Branch, rec.BaseSHA, rec.PushedHead, rec.StartedAt = "sprint/s1/p1", testPriorSHA, testCommitSHA, 1000
	rec.Outcome = Outcome{Result: "ok", Head: testCommitSHA}
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

// gateTimeout_internal is the wall's error for a command its timeout
// killed, as runWallCmd_internal returns it.
func gateTimeout_internal(timeout time.Duration) error {
	return edge.Failure([]string{"nova-reserve", "run"}, nil, -1, "", "", &edge.ExecError{Program: "nova-reserve", Kind: edge.ErrTimeout, Reason: fmt.Sprintf("timed out after %s", timeout)})
}

// A done-when runs until the card's deadline, each gate with what is left
// of it; the seal and the apply of the merged head keep the step bound.
func TestDoneWhenRunsUntilTheCardsDeadline(t *testing.T) {
	t.Parallel()
	clock := &gateClock_internal{sec: 1600} // the harness took ten minutes of a 90-minute deadline
	wall := &gateWall_internal{}
	wall.run = func(string, time.Duration) (WallRunResult, error) {
		clock.add(524 * time.Second) // the gate measured on 2026-11-11
		return WallRunResult{}, nil
	}
	rec := gateStep_internal(t, wall, clock, JobRecord{StreamRef: "sprint/s1", DeadlineAt: 1000 + 5400, DoneWhen: []string{"make test", "go test -timeout 600s ./..."}})
	if rec.Outcome.Result != "ok" || rec.State != JobRebased {
		t.Fatalf("outcome %+v at %s, want ok: two gates of 524 s inside the deadline", rec.Outcome, rec.State)
	}
	want := []time.Duration{4800 * time.Second, (4800 - 524) * time.Second}
	if len(wall.tests) != 2 || wall.tests[0] != want[0] || wall.tests[1] != want[1] {
		t.Errorf("done-when timeouts %v, want %v: what is left of the card's deadline", wall.tests, want)
	}
	if len(wall.steps) == 0 {
		t.Fatalf("no wall step ran: the merged head is applied in the wall before the gates")
	}
	for _, d := range wall.steps {
		if d != DefaultTimeouts().RebaseAndTest {
			t.Errorf("a wall step ran with %s, want the step bound %s: only a done-when runs to the deadline", d, DefaultTimeouts().RebaseAndTest)
		}
	}
}

// A done-when the deadline cut short says it timed out and after how long,
// never that it "did not run", and is a fail: it bounces, so the bounce
// limit holds the card, where a fault was dealt again for ever.
func TestDoneWhenThatTimesOutSaysSoAndIsAFail(t *testing.T) {
	t.Parallel()
	clock := &gateClock_internal{sec: 1600}
	wall := &gateWall_internal{}
	wall.run = func(_ string, timeout time.Duration) (WallRunResult, error) {
		clock.add(timeout)
		return WallRunResult{ExitCode: -1, Output: []byte("ok  \tpkg/a\t1.2s\n=== RUN   TestSlow\n")}, gateTimeout_internal(timeout)
	}
	rec := gateStep_internal(t, wall, clock, JobRecord{DeadlineAt: 1000 + 5400, DoneWhen: []string{"go test ./..."}})
	if rec.Outcome.Result != "fail" || rec.Outcome.Head != testCommitSHA {
		t.Errorf("outcome %+v, want a fail keeping the pushed head", rec.Outcome)
	}
	for _, want := range []string{"go test ./...", "timed out after 1h20m0s", "deadline", "=== RUN   TestSlow"} {
		if !strings.Contains(rec.Outcome.Reason, want) {
			t.Errorf("reason %q does not say %q", rec.Outcome.Reason, want)
		}
	}
	if strings.Contains(rec.Outcome.Reason, "did not run") {
		t.Errorf("reason %q says the gate did not run; it ran for 1h20m", rec.Outcome.Reason)
	}
}

// With the deadline already past, no done-when is started (the wall's exec
// reads a zero timeout as its default and a negative one as expired): that
// one truly did not run, a fault saying so.
func TestDoneWhenPastTheCardsDeadlineIsNotStarted(t *testing.T) {
	t.Parallel()
	wall := &gateWall_internal{}
	rec := gateStep_internal(t, wall, &gateClock_internal{sec: 6400}, JobRecord{DeadlineAt: 6400, DoneWhen: []string{"go test ./..."}})
	if len(wall.tests) != 0 {
		t.Errorf("a done-when was started with %v left of a deadline already past", wall.tests)
	}
	if rec.Outcome.Result != "fault" || !strings.Contains(rec.Outcome.Reason, "did not run") || !strings.Contains(rec.Outcome.Reason, "deadline") {
		t.Errorf("outcome %+v, want a fault saying the gate did not run and why", rec.Outcome)
	}
}

// A record with no deadline (written before deadlines were recorded) keeps
// the step bound: a gate is never run unbounded.
func TestDoneWhenWithNoRecordedDeadlineKeepsTheStepBound(t *testing.T) {
	t.Parallel()
	wall := &gateWall_internal{}
	gateStep_internal(t, wall, &gateClock_internal{sec: 1600}, JobRecord{DoneWhen: []string{"go test ./..."}})
	if len(wall.tests) != 1 || wall.tests[0] != DefaultTimeouts().RebaseAndTest {
		t.Errorf("done-when timeouts %v, want the step bound %s", wall.tests, DefaultTimeouts().RebaseAndTest)
	}
}

// redGate_internal runs one red done-when printing out and returns the
// report's reason and its bound: the "tests red" words, then at most
// MaxReasonBytes of the output.
func redGate_internal(t *testing.T, cmd, out string) (reason string, bound int) {
	t.Helper()
	wall := &gateWall_internal{}
	wall.run = func(string, time.Duration) (WallRunResult, error) {
		return WallRunResult{ExitCode: 2, Output: []byte(out)}, nil
	}
	rec := gateStep_internal(t, wall, &gateClock_internal{sec: 1600}, JobRecord{DeadlineAt: 6400, DoneWhen: []string{cmd}})
	if rec.Outcome.Result != "fail" {
		t.Fatalf("outcome %+v, want fail: the gate is red", rec.Outcome)
	}
	return rec.Outcome.Reason, len(fmt.Sprintf("tests red (%s): ", cmd)) + MaxReasonBytes
}

// A red `make -k` goes on past the failing target, so the output's end is
// the targets after it (here a read-only module cache's warnings): the
// reason keeps the failing target's own lines, then the end, in the same
// bytes (sm-b7-java-tables.w19 lost its javac error).
func TestDoneWhenRedReasonKeepsTheLinesThatSayWhatFailed(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("javac -d build src/Tables.java\n")
	b.WriteString("src/Tables.java:12: error: cannot find symbol\n")
	b.WriteString("make: *** [Makefile:40: java-tables] Error 1\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "go: writing stat cache: open /home/nova/go/pkg/mod/cache/download/example.com/m%03d/@v/v1.0.0.info: permission denied\n", i)
	}
	b.WriteString("make: Target 'all' not remade because of errors.\n")
	if b.Len() < 2*MaxReasonBytes {
		t.Fatalf("the fixture is %d bytes: its failing lines must be far outside the last %d", b.Len(), MaxReasonBytes)
	}
	reason, bound := redGate_internal(t, "make -k all", b.String())
	for _, want := range []string{"src/Tables.java:12: error: cannot find symbol", "make: *** [Makefile:40: java-tables] Error 1", "m079/@v/v1.0.0.info: permission denied"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason lost %q:\n%s", want, reason)
		}
	}
	if !strings.HasSuffix(reason, "make: Target 'all' not remade because of errors.") {
		t.Errorf("the reason does not end with the output's end:\n%s", reason)
	}
	if len(reason) > bound {
		t.Errorf("the reason is %d bytes, over its bound of %d", len(reason), bound)
	}
}

// More failing lines than half the bytes: the first are kept (a compiler's
// first error is the cause), the rest counted, never cut silently, and the
// output's end is still there.
func TestDoneWhenRedReasonCountsTheFailingLinesItCut(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "--- FAIL: TestTable%03d (0.00s)\n", i)
	}
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "warning: unused variable v%03d in generated code, line %d\n", i, i)
	}
	b.WriteString("FAIL\tpkg/tables\t0.412s\n")
	reason, bound := redGate_internal(t, "go test ./...", b.String())
	for _, want := range []string{"--- FAIL: TestTable000 (0.00s)", "more failing lines", "unused variable v199", "FAIL\tpkg/tables\t0.412s"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason lost %q:\n%s", want, reason)
		}
	}
	if strings.Contains(reason, "TestTable199") {
		t.Errorf("the reason holds the 200th failing line: the failing lines took more than their half")
	}
	if len(reason) > bound {
		t.Errorf("the reason is %d bytes, over its bound of %d", len(reason), bound)
	}
}

// Output that fits is kept whole, and output with no line that says what
// failed is cut as before: its end.
func TestDoneWhenRedReasonOfPlainOutputIsItsEnd(t *testing.T) {
	t.Parallel()
	short := "--- FAIL: TestX (0.00s)\n    x_test.go:9: got 1, want 2\nFAIL"
	if reason, _ := redGate_internal(t, "go test ./...", short+"\n"); reason != "tests red (go test ./...): "+short {
		t.Errorf("a short output was changed: %q", reason)
	}
	long := strings.Repeat("assertion did not hold at step 12 of the walk\n", 300)
	if reason, _ := redGate_internal(t, "make walk", long); reason != "tests red (make walk): "+TailBytes([]byte(long), MaxReasonBytes) {
		t.Errorf("an output with no failing line is not its last %d bytes:\n%s", MaxReasonBytes, reason)
	}
}
