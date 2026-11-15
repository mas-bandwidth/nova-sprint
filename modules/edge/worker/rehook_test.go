package worker

import (
	"context"
	"fmt"
	"testing"
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
