package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// The start gives up the row's hand (2026-11-10): tla/Runner.tla
// KillLeftover, UnhookRefused, Exit and Restart.

func startRunner_internal(t *testing.T, sc SprintClient, procs ProcessTable, slept *[]time.Duration) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: newFakeGit_internal(), Wall: &fakeWall{}, Procs: procs, Now: testClock_internal(),
		Sleep:   func(d time.Duration) { *slept = append(*slept, d) },
		BusHook: func(context.Context, string) (WakeHook, error) { return newFakeWakeHook(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func heldBy_internal(t *testing.T, sc *StoreSprintClient, row string) []string {
	t.Helper()
	snap, err := sc.Store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var held []string
	for id, c := range snap.Cards {
		if c.Holder == row && (c.State == card.Ready || c.State == card.Working) {
			held = append(held, id)
		}
	}
	return held
}

// A refused unhook (the CLI's exit 1: here a commit refused because a card
// moved between the unhook's read and its commit) is tried again; the start
// hooks in only once the hand is given up.
func TestAStartTriesARefusedUnhookAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, row := seedFakeStore_internal(t, testClock_internal())
	sc := &StoreSprintClient{Store: st, Now: testClock_internal()}
	if err := sc.Hook(ctx, row); err != nil {
		t.Fatal(err)
	}
	sc.UnhookRefusals = 2
	var slept []time.Duration
	hook, err := startRunner_internal(t, sc, nil, &slept).Start(ctx, row)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	hook.Stop()
	if sc.UnhookCalls != 3 || len(slept) != 2 {
		t.Errorf("two refusals then the unhook: unhooks %d (want 3), waits %v (want 2)", sc.UnhookCalls, slept)
	}
	if held := heldBy_internal(t, sc, row); len(held) != 0 {
		t.Errorf("the start hooked in holding %v: a restart never runs with a kept hand", held)
	}
}

// An unhook refused every time ends the start with the refusal's words and
// no sprint hook: the worker exits non-zero and its service starts it again.
func TestAStartThatCannotGiveUpTheHandEndsTheWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, row := seedFakeStore_internal(t, testClock_internal())
	sc := &StoreSprintClient{Store: st, Now: testClock_internal()}
	if err := sc.Hook(ctx, row); err != nil {
		t.Fatal(err)
	}
	sc.UnhookRefusals, sc.HookCalls = StartUnhookTries, 0
	var slept []time.Duration
	_, err := startRunner_internal(t, sc, nil, &slept).Start(ctx, row)
	if err == nil || !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "a card moved") || !strings.Contains(err.Error(), "never runs with a kept hand") {
		t.Fatalf("Start must end the worker with the refusal's words, got %v", err)
	}
	if sc.UnhookCalls != StartUnhookTries || sc.HookCalls != 0 {
		t.Errorf("unhooks %d (want %d), sprint hooks %d (want 0)", sc.UnhookCalls, StartUnhookTries, sc.HookCalls)
	}
}

// killFirstSprint_internal records, at the start's unhook, which harnesses
// the process table had killed.
type killFirstSprint_internal struct {
	*StoreSprintClient
	procs        *fakeProcTable
	killedBefore []int
	trees        *[]string // the job directories whose processes were ended
	treesBefore  []string
}

func (s *killFirstSprint_internal) Unhook(ctx context.Context, row string) error {
	s.procs.mu.Lock()
	s.killedBefore = slices.Clone(s.procs.killed)
	s.procs.mu.Unlock()
	s.treesBefore = slices.Clone(*s.trees)
	return s.StoreSprintClient.Unhook(ctx, row)
}

// A harness a dead worker left running is killed from its job record, and
// every process of each of the row's job directories is ended, before the
// unhook (and so before the probe, which has no upper bound).
func TestAStartKillsALeftoverHarnessBeforeTheUnhook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, row := seedFakeStore_internal(t, testClock_internal())
	inner := &StoreSprintClient{Store: st, Now: testClock_internal()}
	if err := inner.Hook(ctx, row); err != nil {
		t.Fatal(err)
	}
	procs := &fakeProcTable{alive: map[string]bool{"777:n777": true, "778:n778": true}}
	var trees []string
	sc := &killFirstSprint_internal{StoreSprintClient: inner, procs: procs, trees: &trees}
	var slept []time.Duration
	r := startRunner_internal(t, sc, procs, &slept)
	r.KillJob = func(jobDir string) (int, error) { trees = append(trees, filepath.Base(jobDir)); return 1, nil }
	for _, rec := range []JobRecord{
		{Epoch: 1, Consumer: "p1.w1", Kind: "work", Row: row, Gen: 1, State: JobRunning, PID: 777, Nonce: "n777"},
		{Epoch: 1, Consumer: "p2.w1", Kind: "work", Row: row, Gen: 1, State: JobHarvested},                               // a gate of it may still run
		{Epoch: 1, Consumer: "p9.w1", Kind: "work", Row: "other.ds", Gen: 1, State: JobRunning, PID: 778, Nonce: "n778"}, // another row's
	} {
		dir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := WriteJobRecord(dir, rec); err != nil {
			t.Fatal(err)
		}
	}
	hook, err := r.Start(ctx, row)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	hook.Stop()
	if !slices.Equal(sc.killedBefore, []int{777}) {
		t.Errorf("killed before the unhook: %v, want [777] (this row's leftover only)", sc.killedBefore)
	}
	if !slices.Equal(sc.treesBefore, []string{"p1.w1", "p2.w1"}) {
		t.Errorf("job processes ended before the unhook: %v, want [p1.w1 p2.w1] (this row's jobs only)", sc.treesBefore)
	}
}
