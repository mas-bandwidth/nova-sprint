package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// rehookSprint is a sprint whose next says the row is not hooked in until a
// hook goes through; the first refusals hooks are refused, as the hook verb
// refuses a row nova-config does not name. Only Next and Hook are called.
type rehookSprint struct {
	SprintClient
	unhooked bool
	refusals int
	hooks    int
}

func (s *rehookSprint) Next(_ context.Context, row string) (NextResponse, error) {
	return NextResponse{Epoch: 1, Row: row, Unhooked: s.unhooked}, nil
}

func (s *rehookSprint) Hook(_ context.Context, row string) error {
	s.hooks++
	if s.refusals > 0 {
		s.refusals--
		return fmt.Errorf("%w: nova-sprint hook --as %s: exit 1: SPRINT REFUSED hook: %s is not in nova-config", ErrRefused, row, row)
	}
	s.unhooked = false
	return nil
}

// A retired bench comes back by itself (Glenn, 2026-11-15: the sprint
// follows nova-config; tla/RowAlive.tla Rehook, ReNamedComesBack): every
// pass whose next says the row is not hooked in hooks again; the hook is
// refused while nova-config does not name the row (machine removed), and
// the pass after nova-config names it again (machine added) goes through.
// A pass on a hooked row never hooks.
func TestPassHooksAgainWhenNextSaysUnhooked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sprint := &rehookSprint{unhooked: true, refusals: 2}
	r, err := NewRunner(Runner{Host: "amd-3", RootDir: t.TempDir(), Sprint: sprint, Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	for pass := 1; pass <= 2; pass++ {
		if err := r.StepRow(ctx, "amd-3"); err != nil {
			t.Fatalf("pass %d: a refused hook is not a failed pass: %v", pass, err)
		}
		if sprint.hooks != pass || !sprint.unhooked {
			t.Fatalf("pass %d: hooks %d unhooked %t, want a refused hook per pass", pass, sprint.hooks, sprint.unhooked)
		}
	}
	if err := r.StepRow(ctx, "amd-3"); err != nil {
		t.Fatal(err)
	}
	if sprint.hooks != 3 || sprint.unhooked {
		t.Fatalf("hooks %d unhooked %t: the pass after nova-config names the row again must hook it", sprint.hooks, sprint.unhooked)
	}
	if err := r.StepRow(ctx, "amd-3"); err != nil {
		t.Fatal(err)
	}
	if sprint.hooks != 3 {
		t.Fatalf("a hooked row hooked again: %d hooks", sprint.hooks)
	}
}

// The CLI client reads next --json's "unhooked" (verbs nextView.Unhooked):
// the typed answer the pass acts on, never the message's words.
func TestNextResponseReadsUnhooked(t *testing.T) {
	t.Parallel()
	c := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) {
		return []byte(`{"message":"amd-3 is not hooked in to the sprint","row":"amd-3","unhooked":true}`), 0, nil
	}}
	resp, err := c.Next(context.Background(), "amd-3")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Unhooked {
		t.Fatalf("unhooked not read: %+v", resp)
	}
}

// startSprint refuses the start's first hooks, as the hook verb refuses a
// machine nova-config does not name, then takes one and calls hooked.
type startSprint struct {
	probeSprint
	refusals int
	hooked   func()
}

func (s *startSprint) Hook(_ context.Context, row string) error {
	s.hooks++
	if s.refusals > 0 {
		s.refusals--
		return fmt.Errorf("%w: nova-sprint hook --as %s: exit 1: SPRINT REFUSED hook: %s is not in nova-config", ErrRefused, row, row)
	}
	s.hooked()
	return nil
}

// A start whose hook is refused never exits (an exit would have the service
// start it again and pay for the probe each time): it waits the probe's
// backoff, 30 s doubling, and hooks again without probing again; once the
// hook goes through it serves. One probe, three hooks, a clean stop.
func TestServeRowWaitsOutARefusedHookWithOneProbe(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sp := &startSprint{probeSprint: probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-flash"}}}}}, refusals: 2, hooked: cancel}
	probes := 0
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		probes++
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{Elapsed: time.Second}, nil
	}}
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	r.BusHook = func(context.Context, string) (WakeHook, error) { return newFakeWakeHook(), nil }
	var waits []time.Duration
	r.Sleep = func(d time.Duration) { waits = append(waits, d) }
	var warned []string
	r.Warn = func(s string) { warned = append(warned, s) }
	if err := r.ServeRow(ctx, "b1", "opencode"); err != nil {
		t.Fatalf("a refused hook must not end the start: %v", err)
	}
	if sp.hooks != 3 || probes != 1 {
		t.Fatalf("hooks %d probes %d: want 3 hooks and the one probe", sp.hooks, probes)
	}
	if !slices.Equal(waits, []time.Duration{ProbeBackoff, 2 * ProbeBackoff}) {
		t.Fatalf("waits %v, want the probe's backoff", waits)
	}
	if len(warned) != 2 || !strings.Contains(warned[0], "not in nova-config") {
		t.Fatalf("warned %q", warned)
	}
}

// The pass logs a hook refusal when it changes, never on every pass: the
// first refusal, then the clearing; the same refusal again is not logged.
func TestPassLogsAHookRefusalOnlyWhenItChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sprint := &rehookSprint{unhooked: true, refusals: 3}
	root := t.TempDir()
	r, err := NewRunner(Runner{Host: "amd-3", RootDir: root, StateDir: root, Sprint: sprint, Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := r.StepRow(ctx, "amd-3"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "nova-worker", "amd-3.log"))
	if err != nil {
		t.Fatal(err)
	}
	var hookLines []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, " step=hook ") {
			hookLines = append(hookLines, l)
		}
	}
	if len(hookLines) != 3 || !strings.Contains(hookLines[0], "not in nova-config") || !strings.Contains(hookLines[1], "ok") || !strings.Contains(hookLines[2], "the refusal cleared") {
		t.Fatalf("hook lines %q: want the refusal once, the hook, and the clearing", hookLines)
	}
}
