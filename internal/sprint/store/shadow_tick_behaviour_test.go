package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// partNamed is the shadow plan's part of name on table, false for none.
func partNamed(plan ShadowPlan, table, name string) (ShadowPart, bool) {
	for _, p := range plan.Parts {
		if p.Table == table && p.Name == name {
			return p, true
		}
	}
	return ShadowPart{}, false
}

// partSum is the sum of the plan's parts' sizes.
func partSum(plan ShadowPlan) int {
	n := 0
	for _, p := range plan.Parts {
		n += p.Size
	}
	return n
}

// ShadowTick is the tick's plan with nothing applied (tick.go:2041,
// docs/SPEC-SPRINT.md section 14): on a running sprint with ready cards it
// reports RUNNING, plans a deal on the work table, its size the sum of its
// parts, and writes nothing.
func TestShadowTickPlansADealForARunningSprintAndWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(3)
	h.startMachine()
	before, err := h.m.Snapshot()
	require.NoError(t, err)

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)

	assert.Equal(t, Running, plan.State)
	deal, ok := partNamed(plan, sprint.Work, "deal")
	require.True(t, ok, "no deal part: %+v", plan.Parts)
	assert.Positive(t, deal.Size)
	assert.Equal(t, partSum(plan), plan.Size, "the plan's size is the sum of its parts")

	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a shadow tick writes nothing")
}

// ShadowTick plans whether the machine is RUNNING or STOPPED, the state
// reported (tick.go:2039): a stopped machine reports STOPPED and still plans.
func TestShadowTickStillPlansWhenTheMachineIsStopped(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(3)
	before, err := h.m.Snapshot()
	require.NoError(t, err)

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)

	assert.Equal(t, Stopped, plan.State)
	assert.Positive(t, plan.Size, "a stopped machine still plans: %+v", plan.Parts)

	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a shadow tick writes nothing")
}

// A shadow tick writes nothing, so it does not perform a restore the epoch
// still owes (tick.go:2054-2056): it refuses, naming the owed restore, and the
// epoch still owes it after.
func TestShadowTickRefusesAnEpochThatOwesItsRestore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	ok, err := h.m.AdvanceEpoch(h.ctx, es.N, h.now)
	require.NoError(t, err)
	require.True(t, ok)

	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still owes the restore")
	assert.Contains(t, err.Error(), "does not perform it")

	after, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.True(t, after.Owed, "the epoch still owes its restore")
}

// A shadow tick repairs no pending operation (tick.go:2036): one left in the
// fence is refused as a busy read that repairs nothing, and it stays pending.
func TestShadowTickRefusesAPendingOperationAndRepairsNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.m.Fail = func(p string) error {
		if p == "apply t-work before" {
			return errors.New("connection reset")
		}
		return nil
	}
	_, err := h.st.Run(h.ctx, DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	require.Error(t, err)
	h.m.Fail = nil
	require.NotNil(t, h.m.Pending())

	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a shadow tick repairs nothing")

	assert.NotNil(t, h.m.Pending(), "the operation is still pending")
}

// The shadow tick plans the pump's drain of a queued work-table change
// (tick.go:2090) and empties no queue: the queue still holds it after.
func TestShadowTickPlansTheDrainAndLeavesTheQueue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.startMachine()
	h.machine()
	s := h.snap()
	c := s.Fleet.Card("s1-1.w1")
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	q, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, q, "the finish queued a work-table change")

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	drain, ok := partNamed(plan, sprint.Work, "drain")
	require.True(t, ok, "no drain part: %+v", plan.Parts)
	assert.Positive(t, drain.Size)

	q, err = h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, q, "the shadow tick does not empty the queue")
}

// The shadow plan's Reads names a primary waiting on reads (tick.go:2110-2112):
// a primary in review with no reader up to read it is named in Reads.
func TestShadowTickNamesThePrimaryWaitingOnReads(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.must(SetStep(sprint.SetReq{ReadCards: "on", Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	s := h.snap()
	c := s.Fleet.Card("s1-1.w1")
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, plan.Reads, "a waiting primary is named in Reads")
	assert.True(t, strings.Contains(strings.Join(plan.Reads, "\n"), "s1-1"), "Reads names the primary: %v", plan.Reads)
}

// fenceBusy is the in-memory store with another writer at work beside the
// shadow tick: every read of the fence, while moves are left, is followed by
// that writer's step, so the read after it finds the fence moved.
type fenceBusy struct {
	*Mem
	moves int
	other func()
}

func (m *fenceBusy) ReadFence(ctx context.Context) (Fence, error) {
	f, err := m.Mem.ReadFence(ctx)
	if m.moves > 0 {
		m.moves--
		m.other()
	}
	return f, err
}

// A store that moves on every read is refused as busy (tick.go:2169): the
// shadow tick says the sprint is busy and repairs nothing.
func TestShadowTickSaysTheSprintIsBusyWhenItMovesUnderTheRead(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	other := &Store{B: h.m, Names: h.st.Names, Actor: "other", Now: h.st.Now, NewID: h.st.NewID, Sleep: h.st.Sleep}
	mf := &fenceBusy{Mem: h.m, moves: 1 << 30, other: func() {
		_, err := other.Run(h.ctx, Step{Verb: "world", Actor: "other", Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: "world", At: s.Now}}}
		}})
		require.NoError(t, err, "the other writer's step")
	}}
	st := *h.st
	st.B = mf
	st.tw = nil
	st.CheckTwin = nil

	_, err := st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the sprint is busy")
}

// A custom Updates list drives the shadow tick's parts (tick.go:2123): a part
// with an empty plan and nothing due is left out of the plan.
func TestShadowTickLeavesOutAnEmptyPart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.st.Updates = []sprint.TableUpdate{
		{Table: "custom", Parts: []sprint.TickPartDef{
			{Name: "empty", Fn: func(s *sprint.Snapshot, r sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 0 }},
		}},
	}

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	_, ok := partNamed(plan, "custom", "empty")
	assert.False(t, ok, "an empty part is left out: %+v", plan.Parts)
}

// A part with an empty plan but moves due is listed with its due count
// (tick.go:2114-2118), its size the nothing it would write.
func TestShadowTickListsADuePartWithItsCount(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.st.Updates = []sprint.TableUpdate{
		{Table: "custom", Parts: []sprint.TickPartDef{
			{Name: "due", Fn: func(s *sprint.Snapshot, r sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 7 }},
		}},
	}

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	p, ok := partNamed(plan, "custom", "due")
	require.True(t, ok, "the due part is listed: %+v", plan.Parts)
	assert.Equal(t, 7, p.Due)
	assert.Zero(t, p.Size)
}

// A part that panics is not recovered (tick.go:2039-2040): the shadow is the
// canary of a crash too.
func TestShadowTickDoesNotRecoverAPanickingPart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.st.Updates = []sprint.TableUpdate{
		{Table: "custom", Parts: []sprint.TickPartDef{
			{Name: "boom", Fn: func(s *sprint.Snapshot, r sprint.TickReq) (sprint.Plan, int) { panic("boom") }},
		}},
	}

	defer func() {
		r := recover()
		require.NotNil(t, r, "a panicking part makes ShadowTick panic")
	}()
	h.st.ShadowTick(h.ctx)
}
