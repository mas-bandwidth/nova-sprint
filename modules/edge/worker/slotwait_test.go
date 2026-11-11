package worker

import (
	"context"
	"slices"
	"testing"
	"time"
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

// A step after the harness (the seal, the apply of the rebased head, a
// done-when gate, a stream gate) that finds every nova-reserve slot held
// waits for one within its own bound: refused after 5 s, the attempt's
// work was thrown away as a wall execution fault (8 attempts on 11-10,
// all "no slot within 5s" in the apply step). The harness's own run keeps
// the short wait: a job with no slot for it is staged and run again on a
// later pass, holding nothing.
func TestStepsAfterTheHarnessWaitForASlotWithinTheirBound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j := Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}
	bound := 20 * time.Minute
	for _, tc := range []struct {
		name string
		run  func(w *OSReserveWall) error
	}{
		{"done-when gate", func(w *OSReserveWall) error {
			_, err := w.RunTest(context.Background(), j, "go test ./...", bound)
			return err
		}},
		{"step", func(w *OSReserveWall) error {
			_, err := w.RunStep(context.Background(), j, []string{"true"}, bound)
			return err
		}},
	} {
		fx := cr2OKExec_internal()
		w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: 5 * time.Second}}
		if err := tc.run(w); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		calls := fx.Calls()
		if len(calls) != 1 {
			t.Fatalf("%s: want one exec, got %d", tc.name, len(calls))
		}
		wait := reserveWaitOf(t, calls[0].Cmd.Argv)
		if wait <= 5*time.Second || wait >= bound {
			t.Errorf("%s: waits %s for a slot under a %s bound: want more than the harness's 5s and less than the bound, so nova-reserve's own refusal (a fault, dealt again) comes before the step's timeout (a fail)", tc.name, wait, bound)
		}
	}
	fx := cr2OKExec_internal()
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: 5 * time.Second}}
	if _, err := w.RunHarness(context.Background(), j, []string{"opencode", "run"}, nil, bound); err != nil {
		t.Fatal(err)
	}
	if wait := reserveWaitOf(t, fx.Calls()[0].Cmd.Argv); wait != 5*time.Second {
		t.Errorf("the harness's run waits %s for a slot, want the configured 5s", wait)
	}
}

func TestStepReserveWait(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ conf, bound, want time.Duration }{
		{5 * time.Second, 20 * time.Minute, 20*time.Minute - 30*time.Second},
		{5 * time.Second, 2 * time.Minute, 2*time.Minute - 12*time.Second},
		{5 * time.Second, 4 * time.Second, 5 * time.Second},
		{0, 0, 5 * time.Second},
	} {
		if got := stepReserveWait_internal(tc.conf, tc.bound); got != tc.want {
			t.Errorf("stepReserveWait(%s, %s) = %s, want %s", tc.conf, tc.bound, got, tc.want)
		}
	}
}
