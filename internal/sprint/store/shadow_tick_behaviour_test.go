package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/storetest"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// planPart is the part of a shadow plan named name, and whether it was planned.
func planPart(res ShadowPlan, name string) (ShadowPart, bool) {
	for _, p := range res.Parts {
		if p.Name == name {
			return p, true
		}
	}
	return ShadowPart{}, false
}

// ShadowTick plans every part of the tick on one fenced read and writes
// nothing (tick.go:2031, docs/SPEC-SPRINT.md section 14).
//

// A running sprint with ready cards: State RUNNING, a deal part on the work
// table, the plan's size the sum of its parts, and the store unchanged.
func TestAShadowTickOfARunningSprintPlansTheDealAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.setup(3)
	h.startMachine()
	before := h.snap()
	res, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	require.Equal(t, Running, res.State, "the state the shadow read")
	deal, ok := planPart(res, "deal")
	require.True(t, ok, "no deal part planned: %+v", res.Parts)
	require.Equal(t, sprint.Work, deal.Table, "the deal part's table: %+v", deal)
	sum := 0
	for _, p := range res.Parts {
		sum += p.Size
	}
	require.Equal(t, sum, res.Size, "the plan's size is the sum of its parts")
	after := h.snap()
	require.Empty(t, storetest.TwinDiff(before, after), "the shadow tick changed the store")
}

// A stopped machine: State STOPPED, it still plans, the store unchanged.
func TestAShadowTickOfAStoppedMachineStillPlansAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.setup(2)
	before := h.snap()
	res, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	require.Equal(t, Stopped, res.State, "the state the shadow read")
	after := h.snap()
	require.Empty(t, storetest.TwinDiff(before, after), "the shadow tick changed the store")
}

// An epoch that owes its restore: the shadow refuses, saying it owes the
// restore and does not perform it, and the epoch still owes it after.
func TestAShadowTickRefusesAnEpochThatOwesItsRestore(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	es, err := h.m.Epoch(h.ctx)
	require.NoError(t, err)
	_, err = h.m.AdvanceEpoch(h.ctx, es.N, h.now)
	require.NoError(t, err)
	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "owes the restore", "the shadow's refusal: %v", err)
	after, err := h.m.Epoch(h.ctx)
	require.NoError(t, err)
	require.True(t, after.Owed, "the shadow tick performed the restore it owes")
}

// An operation left pending: the shadow refuses, saying a shadow tick repairs
// nothing, and the operation is still pending after.
func TestAShadowTickRefusesAPendingOperationAndRepairsNothing(t *testing.T) {
	h := newHarness(t)
	h.setup(3)
	h.m.Fail = func(p string) error {
		if p == "apply t-work before" {
			return errors.New("connection reset")
		}
		return nil
	}
	_, err := h.st.Run(h.ctx, DealStep(sprint.DealReq{Sel: sprint.Sel{Limit: 1}}))
	require.ErrorIs(t, err, ErrUnknown, "a lost reply on the work table: %v", err)
	h.m.Fail = nil
	require.NotNil(t, h.m.Pending(), "the operation is pending")
	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "a shadow tick repairs nothing", "the shadow's refusal: %v", err)
	require.NotNil(t, h.m.Pending(), "the shadow tick repaired the pending operation")
}

// Queued work-table changes: a drain part is planned and the queue is not
// emptied.
func TestAShadowTickPlansTheDrainOfQueuedWorkTableChanges(t *testing.T) {
	h := newHarness(t)
	h.setup(4)
	h.startMachine()
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{Limit: 2}}))
	q, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, q, "the running deal queued no work-table change")
	res, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	drain, ok := planPart(res, sprint.PartDrain)
	require.True(t, ok, "no drain part planned: %+v", res.Parts)
	require.Equal(t, sprint.Work, drain.Table, "the drain part's table: %+v", drain)
	after, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, after, "the shadow tick emptied the queue")
}

// A primary waiting on reads: the plan's Reads names it.
func TestAShadowTickNamesThePrimaryWaitingOnReads(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	h.must(SetStep(sprint.SetReq{ReadCards: sprint.ReadCardsOnWord, Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{Limit: 1}}))
	s := h.snap()
	c := s.Fleet.Card("s1-1.w1")
	require.NotNil(t, c, "the deal placed s1-1.w1")
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	// no reader is up to read it: every read the primary wants waits
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		require.NoError(t, h.st.SetReaderAway(h.ctx, r, true, h.st.Actor))
	}
	res, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	require.Contains(t, fmt.Sprint(res.Reads), "s1-1", "the plan's Reads names the waiting primary: %v", res.Reads)
}

// movingBackend is a store that moves under every read: each Shapes through it
// writes through the inner Mem (never through Mem's Fail hook, which runs
// under the Mem's lock), so the fence moves as the shadow reads.
type movingBackend struct {
	*Mem
}

func (m *movingBackend) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	if f, err := m.Mem.ReadFence(ctx); err == nil {
		_, _ = m.Mem.Acquire(ctx, f.Gen, OpRecord{ID: "mover", Verb: "shadow"})
	}
	return m.Mem.Shapes(ctx, tables)
}

// A store that moves on every read: the shadow says the sprint is busy.
func TestAShadowTickSaysTheSprintIsBusyWhenTheStoreMovesUnderIt(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	st := *h.st
	st.B = &movingBackend{Mem: h.m}
	_, err := st.ShadowTick(h.ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "the sprint is busy", "the shadow's refusal: %v", err)
}

// A custom Updates list: an empty part is left out, a due part with an empty
// plan is listed with its due count, and a part that panics makes ShadowTick
// panic (it recovers nothing, by its doc).
func TestAShadowTickPlansItsUpdatesAndRecoversNoPanic(t *testing.T) {
	h := newHarness(t)
	h.setup(1)
	h.st.Updates = []sprint.TableUpdate{{Table: sprint.Work, Parts: []sprint.TickPartDef{
		{Name: "empty", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 0 }},
		{Name: "due", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 3 }},
	}}}
	res, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	_, ok := planPart(res, "empty")
	require.False(t, ok, "an empty part with nothing due was listed: %+v", res.Parts)
	due, ok := planPart(res, "due")
	require.True(t, ok, "a due part with an empty plan was left out: %+v", res.Parts)
	require.Equal(t, 3, due.Due, "the due count")
	require.Equal(t, sprint.Work, due.Table, "the due part's table: %+v", due)
	// a part that panics is not recovered: the shadow is the canary of a crash
	h.st.Updates = []sprint.TableUpdate{{Table: sprint.Work, Parts: []sprint.TickPartDef{
		{Name: "boom", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { panic("boom") }},
	}}}
	require.Panics(t, func() { _, _ = h.st.ShadowTick(h.ctx) }, "the shadow recovered a part's panic")
}
