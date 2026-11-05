package store

import (
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRefusalsAOnMem(t *testing.T) {
	t.Parallel()
	t.Run("R4", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		pinned, err := h.st.pin(h.ctx)
		require.NoError(t, err)
		m := pinned.B.(*Mem)
		f, err := m.ReadFence(h.ctx)
		require.NoError(t, err)
		op := OpRecord{ID: "seed", CallerOp: "bad", Result: "{"}
		ok, err := m.Acquire(h.ctx, f.Gen, op)
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, m.Release(h.ctx, op, true))
		_, err = h.st.Run(h.ctx, Step{Verb: "probe", CallerOp: "bad"})
		require.ErrorContains(t, err, "operation bad: its recorded result is unreadable:")
	})
	t.Run("R11", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		res, err := h.st.Run(h.ctx, Step{Verb: "probe", RequiresStopped: true, Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{} }})
		require.NoError(t, err)
		require.Equal(t, []sprint.Refusal{{Key: "probe", Why: "the machine has no halted run: stop-return follows cancellation during STOP"}}, res.Refused)
	})
	t.Run("R12", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		pinned, err := h.st.pin(h.ctx)
		require.NoError(t, err)
		m := pinned.B.(*Mem)
		f, err := m.ReadFence(h.ctx)
		require.NoError(t, err)
		op := OpRecord{ID: "seed", Queue: []sprint.QueuedChange{{Entry: &ntable.BatchMemberEntry{ID: "card"}}}}
		ok, err := m.Acquire(h.ctx, f.Gen, op)
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, m.Release(h.ctx, op, true))
		injected := errors.New("queue unavailable")
		h.m.Fail = func(point string) error {
			if point == "queue" {
				return injected
			}
			return nil
		}
		_, err = h.st.Run(h.ctx, Step{Verb: "probe", Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{} }})
		require.ErrorIs(t, err, injected)
		require.ErrorContains(t, err, "draining the work table's queue: queue: the store did not answer: queue unavailable")
	})
	t.Run("R17", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.startMachine()
		step := Step{Verb: "probe", Load: []string{sprint.Work}, Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Units: []sprint.Unit{{Key: "card", Changes: []sprint.Change{{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: "card", Set: map[string]string{"note": strings.Repeat("x", MaxCardTextBytes+1)}}}}}}}
		}}
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		require.Equal(t, []sprint.Refusal{{Key: "card", Why: fmt.Sprintf("the step cannot be written, nothing was written: card card: field note is %d bytes, over the bound of %d bytes; shorten it, or point to a file or a comment", MaxCardTextBytes+1, MaxCardTextBytes)}}, res.Refused)
	})
}
