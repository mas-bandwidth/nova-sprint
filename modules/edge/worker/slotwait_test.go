package worker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// reserveWaitOf is the --wait value of the one nova-reserve run in argv.
func reserveWaitOf(t *testing.T, argv []string) time.Duration {
	t.Helper()
	i := slices.Index(argv, "--wait")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("no --wait in %q", argv)
	}
	d, err := time.ParseDuration(argv[i+1])
	if err != nil {
		t.Fatalf("--wait %q: %v", argv[i+1], err)
	}
	return d
}

// slotAfter_internal is nova-reserve on a busy bench, on no clock: a slot
// is held slot after the run starts (never, when slot is past the --wait),
// then the command runs for need. The run is killed at its Timeout: before
// the slot, nova-reserve has printed nothing; after it, its HELD line.
func slotAfter_internal(t *testing.T, slot, need time.Duration) *edge.FakeExec {
	t.Helper()
	fx := edge.NewFakeExec()
	fx.On("nova-reserve", func(c edge.FakeCall) edge.FakeOutcome {
		wait := reserveWaitOf(t, c.Cmd.Argv)
		switch {
		case slot > wait:
			return edge.FakeOutcome{ExitCode: 1, Stderr: "nova-reserve: no slot within " + wait.String() + "\n", Duration: wait}
		case c.Cmd.Timeout < slot:
			return edge.FakeOutcome{Timeout: true, Duration: c.Cmd.Timeout}
		case c.Cmd.Timeout < slot+need:
			return edge.FakeOutcome{Timeout: true, Stderr: "nova-reserve: HELD mode=shared slot=1/4\n", Duration: c.Cmd.Timeout}
		}
		return edge.FakeOutcome{Stderr: "nova-reserve: HELD mode=shared slot=1/4\n", Stdout: "ok\n", Duration: slot + need}
	})
	return fx
}

func slotWaitRunner_internal(t *testing.T, fx *edge.FakeExec) (*Runner, Job) {
	t.Helper()
	dir := t.TempDir()
	r, err := NewRunner(Runner{Host: "b1", RootDir: dir, Now: func() time.Time { return time.Unix(1000, 0) }, Sleep: func(time.Duration) {},
		Wall: &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: 5 * time.Second}}})
	if err != nil {
		t.Fatal(err)
	}
	return r, Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}
}

// A gate whose slot comes at the last moment of its wait still has its
// whole bound to run in: the wait and the run are timed apart (11-11: a
// late slot left a gate seconds, and it was failed "timed out", a bounce
// spent on the card for the bench's queue).
func TestAGateWithALateSlotHasItsWholeBound(t *testing.T) {
	t.Parallel()
	bound := 20 * time.Minute
	wait := stepReserveWait_internal(5*time.Second, bound)
	r, j := slotWaitRunner_internal(t, slotAfter_internal(t, wait, bound))
	if res, err := r.Wall.RunTest(context.Background(), j, "go test ./...", bound); err != nil || res.ExitCode != 0 {
		t.Errorf("a gate held at the end of its %s wait, needing its whole %s bound: exit %d, %v; want it to pass", wait, bound, res.ExitCode, err)
	}
	r2, j2 := slotWaitRunner_internal(t, slotAfter_internal(t, wait, bound))
	if res, err := r2.Wall.RunStep(context.Background(), j2, []string{"true"}, bound); err != nil || res.ExitCode != 0 {
		t.Errorf("a step held at the end of its %s wait, needing its whole %s bound: exit %d, %v; want it to pass", wait, bound, res.ExitCode, err)
	}
}

// No slot within the wait is a fault that blames no one, and says how long
// it waited.
func TestNoSlotWithinTheWaitIsAFaultSayingHowLong(t *testing.T) {
	t.Parallel()
	r, j := slotWaitRunner_internal(t, slotAfter_internal(t, 24*time.Hour, time.Second))
	bound, _, _ := r.doneWhenBound_internal(JobRecord{})
	out, red, testsRed := r.doneWhen_internal(context.Background(), j, JobRecord{}, doneWhenGate, "go test ./...")
	if !red || testsRed || out.Result != "fault" {
		t.Errorf("no slot: %+v red=%v testsRed=%v, want a fault, no tests red", out, red, testsRed)
	}
	if want := "waited " + stepReserveWait_internal(5*time.Second, bound).String(); !strings.Contains(out.Reason, want) {
		t.Errorf("reason %q does not say %q", out.Reason, want)
	}
	// A run killed before its slot was held is the same fault.
	fx := edge.NewFakeExec()
	fx.On("nova-reserve", func(c edge.FakeCall) edge.FakeOutcome {
		return edge.FakeOutcome{Timeout: true, Duration: c.Cmd.Timeout}
	})
	r2, j2 := slotWaitRunner_internal(t, fx)
	if _, err := r2.Wall.RunTest(context.Background(), j2, "go test ./...", time.Minute); !errors.Is(err, ErrNoSlot) {
		t.Errorf("a run timed out with no slot held: %v, want ErrNoSlot", err)
	}
}

// A gate that holds its slot and then runs past its bound is the timeout
// fail, as before.
func TestAGateThatOverrunsItsBoundIsTheTimeoutFail(t *testing.T) {
	t.Parallel()
	r, j := slotWaitRunner_internal(t, slotAfter_internal(t, time.Second, 48*time.Hour))
	out, red, _ := r.doneWhen_internal(context.Background(), j, JobRecord{}, doneWhenGate, "go test ./...")
	if !red || out.Result != "fail" || !strings.Contains(out.Reason, "timed out after") {
		t.Errorf("an overrun after the slot: %+v, want the timeout fail", out)
	}
}

// The harness's own run keeps the short wait: a job with no slot for it is
// staged and run again on a later pass, holding nothing.
func TestTheHarnessKeepsTheShortWait(t *testing.T) {
	t.Parallel()
	fx := cr2OKExec_internal()
	dir := t.TempDir()
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: 5 * time.Second}}
	if _, err := w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, []string{"opencode", "run"}, nil, 20*time.Minute); err != nil {
		t.Fatal(err)
	}
	if wait := reserveWaitOf(t, fx.Calls()[0].Cmd.Argv); wait != 5*time.Second {
		t.Errorf("the harness's run waits %s for a slot, want the configured 5s", wait)
	}
}
