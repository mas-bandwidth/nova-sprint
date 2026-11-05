//go:build functional

package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestRefusalsEarlyOnRedis exercises the early core refusals against a Redis
// started for this test. R3 and R6 already have real-store coverage in
// redis_sprint_functional_test.go (TestRedisCallerOperationReplay and
// TestRedisALateWriterAfterAClearIsRefused respectively).
func TestRefusalsEarlyOnRedis(t *testing.T) {
	t.Parallel()
	c := liveClient(t)
	n := 0
	newH := func(t *testing.T) *harness {
		t.Helper()
		n++
		return liveHarnessOn(t, c, fmt.Sprintf("real-a-%d-", n))
	}

	t.Run("R1 op id holds tilde", func(t *testing.T) {
		h := newH(t)
		step := AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"x"}})
		step.CallerOp = "a~b"
		_, err := h.st.Run(h.ctx, step)
		require.ErrorContains(t, err, "holds '~'")
	})
	t.Run("R2 op id not one word", func(t *testing.T) {
		h := newH(t)
		step := AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"x"}})
		step.CallerOp = "a/b"
		_, err := h.st.Run(h.ctx, step)
		require.ErrorContains(t, err, "is not one word of letters, digits")
	})
	t.Run("R3 covered", func(t *testing.T) {
		t.Skip("covered on Redis by redis_sprint_functional_test.go: TestRedisCallerOperationReplay")
	})
	t.Run("R4 unreadable recorded result", func(t *testing.T) {
		h := newH(t)
		key := h.st.Names.KeyAt(keyDone, 0)
		require.NoError(t, c.HSet(context.Background(), key, "broken", "{").Err())
		step := AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"x"}})
		step.CallerOp = "broken"
		_, err := h.st.Run(h.ctx, step)
		require.ErrorContains(t, err, "recorded result is unreadable")
	})
	t.Run("R5 earlier epoch operation", func(t *testing.T) {
		h := newH(t)
		step := AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"x"}})
		step.CallerOp = "old-op"
		_, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		_, err = h.st.Clear(h.ctx)
		require.NoError(t, err)
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		require.Equal(t, "op old-op", res.Refused[0].Key)
		require.Contains(t, res.Refused[0].Why, "belongs to epoch 0")
	})
	t.Run("R6 covered", func(t *testing.T) {
		t.Skip("covered on Redis by redis_sprint_functional_test.go: TestRedisALateWriterAfterAClearIsRefused")
	})
	t.Run("R7 cleared error after display sync failure", func(t *testing.T) {
		t.Skip("the post-commit display-sync failure requires a Redis command failure after commit; testutil.StartRaw exposes no per-command fault hook, and production code must remain unchanged")
	})
	t.Run("R8 stale halted run", func(t *testing.T) {
		h := newH(t)
		h.startMachine()
		observed, _, err := h.st.Machine(h.ctx)
		require.NoError(t, err)
		_, _, _, err = h.st.StopUntil(h.ctx, "test stop", h.now.Add(time.Hour))
		require.NoError(t, err)
		_, restarted, _, err := h.st.SetMachine(h.ctx, true)
		require.NoError(t, err)
		require.Greater(t, restarted.RunSeq, observed.RunSeq)
		seq := observed.RunSeq
		res, err := h.st.Run(h.ctx, Step{Verb: "stale tick", Halts: true, TickRunSeq: &seq, Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{} }})
		require.NoError(t, err)
		require.True(t, res.StaleRun)
	})
	t.Run("R9 starts work after stop", func(t *testing.T) {
		h := newH(t)
		h.startMachine()
		_, _, _, err := h.st.StopUntil(h.ctx, "test stop", h.now.Add(time.Hour))
		require.NoError(t, err)
		res, err := h.st.Run(h.ctx, Step{Verb: "start work probe", StartsWork: true, Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{} }})
		require.NoError(t, err)
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "no new work or read may start")
	})
	t.Run("R10 reports work after stop", func(t *testing.T) {
		h := newH(t)
		h.startMachine()
		_, _, _, err := h.st.StopUntil(h.ctx, "test stop", h.now.Add(time.Hour))
		require.NoError(t, err)
		res, err := h.st.Run(h.ctx, Step{Verb: "report probe", ReportsWork: true, Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{} }})
		require.NoError(t, err)
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "late work or read report cannot finish")
	})
	t.Run("R11 requires stopped", func(t *testing.T) {
		h := newH(t)
		h.startMachine()
		res, err := h.st.Run(h.ctx, Step{Verb: "requires stopped probe", RequiresStopped: true, Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{} }})
		require.NoError(t, err)
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "no halted run")
	})
	t.Run("R12 pre-step queue drain error", func(t *testing.T) {
		h := newH(t)
		key := h.st.Names.KeyAt(keyQueue, 0)
		require.NoError(t, c.RPush(context.Background(), key, "not-json").Err())
		_, err := h.st.Run(h.ctx, AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"x"}}))
		require.ErrorContains(t, err, "draining the work table's queue")
	})
	t.Run("R13 restore pending", func(t *testing.T) {
		h := newH(t)
		h.st.beforeRestore = func(*Store) error { return errors.New("cut before the restore marker") }
		_, err := h.st.Clear(h.ctx)
		require.ErrorContains(t, err, "cut before the restore marker")
		err = h.st.SetReaderTiers(h.ctx, []string{"reader-a"}, "heavy")
		require.ErrorIs(t, err, ErrRestorePending)
	})
	t.Run("R14 STOP owns lease", func(t *testing.T) {
		h := newH(t)
		wc, _ := h.taken()
		_, _, _, err := h.st.StopUntil(h.ctx, "test stop", h.now.Add(time.Hour))
		require.NoError(t, err)
		res := h.run(FleetStep(sprint.FleetReq{Op: "down", Member: wc.Row}))
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "STOP owns")
	})
	t.Run("R15 named all or none", func(t *testing.T) {
		h := newH(t)
		res, err := h.st.Run(h.ctx, Step{Verb: "named probe", Named: true, Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Units: []sprint.Unit{{Key: "one", Moved: "one"}}, Refused: []sprint.Refusal{{Key: "bad", Why: "blocked"}}}
		}})
		require.NoError(t, err)
		require.Len(t, res.Refused, 2)
		require.Equal(t, "bad", res.Refused[0].Key)
		require.Contains(t, res.Refused[1].Why, "applies all or none")
	})
	t.Run("R16 changed twice", func(t *testing.T) {
		h := newH(t)
		h.setup(1)
		step := Step{Verb: "twice probe", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
			c := s.Work.Card("s1-1")
			return sprint.Plan{Units: []sprint.Unit{
				{Key: "first", Moved: "first", Changes: []sprint.Change{{Table: sprint.Work, Entry: setOf(c, map[string]string{"probe": "first"})}}},
				{Key: "second", Moved: "second", Changes: []sprint.Change{{Table: sprint.Work, Entry: setOf(c, map[string]string{"probe": "second"})}}},
			}}
		}}
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err)
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "changed twice")
	})
	t.Run("R17 queued text over bound", func(t *testing.T) {
		h := newH(t)
		h.startMachine()
		res, err := h.st.Run(h.ctx, AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"too-long"}, Brief: strings.Repeat("b", MaxCardTextBytes+1)}))
		require.NoError(t, err)
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "over the bound")
	})
	t.Run("R18 text bound", func(t *testing.T) {
		h := newH(t)
		res, err := h.st.Run(h.ctx, AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"too-long"}, Brief: strings.Repeat("b", MaxBriefBytes+1)}))
		require.NoError(t, err)
		require.NotEmpty(t, res.Refused)
		require.Contains(t, res.Refused[0].Why, "field brief")
		require.Contains(t, res.Refused[0].Why, fmt.Sprintf("over the bound of %d bytes", MaxBriefBytes))
	})
}
