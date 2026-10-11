package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotText is the Mem store's whole state as text, a shadow tick's store
// unchanged if it is byte for byte the same before and after.
func snapshotText(t *testing.T, h *harness) string {
	t.Helper()
	b, err := h.m.Snapshot()
	require.NoError(t, err)
	return string(b)
}

// TestShadowTickPlansARunningSprintAndWritesNothing: on a RUNNING sprint with
// ready cards the shadow plans the tick's deal on the work table, its whole size
// is the sum of its parts, and the store is unchanged (Store.ShadowTick,
// tick.go:2031).
func TestShadowTickPlansARunningSprintAndWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(3)
	h.startMachine()
	before := snapshotText(t, h)

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "RUNNING", plan.State)
	var deal *ShadowPart
	for i := range plan.Parts {
		if plan.Parts[i].Table == sprint.Work && plan.Parts[i].Name == "deal" {
			deal = &plan.Parts[i]
		}
	}
	require.NotNil(t, deal, "the deal is planned on the work table: %+v", plan.Parts)
	sum := 0
	for _, p := range plan.Parts {
		sum += p.Size
	}
	assert.Equal(t, sum, plan.Size, "size is the sum of its parts")
	assert.Equal(t, before, snapshotText(t, h), "a shadow tick writes nothing")
}

// TestShadowTickPlansAStoppedMachineAndWritesNothing: a STOPPED machine is
// planned too, the state reported, the store unchanged (Store.ShadowTick,
// tick.go:2037).
func TestShadowTickPlansAStoppedMachineAndWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(3)
	before := snapshotText(t, h)

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "STOPPED", plan.State)
	assert.NotEmpty(t, plan.Parts, "a stopped machine still plans")
	assert.Equal(t, before, snapshotText(t, h), "a shadow tick writes nothing")
}

// TestShadowTickRefusesAnEpochThatOwesItsRestore: a clear cut between its
// advance and its restore is refused with its sentence and not performed
// (Store.ShadowTick, tick.go:2054).
func TestShadowTickRefusesAnEpochThatOwesItsRestore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	_, err := h.m.AdvanceEpoch(h.ctx, 0, t0)
	require.NoError(t, err)

	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owes the restore")
	es, err := h.m.Epoch(h.ctx)
	require.NoError(t, err)
	assert.True(t, es.Owed, "the epoch still owes the restore after")
}

// TestShadowTickRefusesToRepairAPendingOperation: an operation left pending is
// not repaired and is still pending after the refusal (Store.ShadowTick,
// tick.go:2142).
func TestShadowTickRefusesToRepairAPendingOperation(t *testing.T) {
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
	require.NotNil(t, h.m.Pending(), "an operation is pending")

	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repairs nothing")
	assert.NotNil(t, h.m.Pending(), "the operation is still pending after")
}

// TestShadowTickPlansADrainWithoutEmptyingTheQueue: queued work-table changes
// are planned as the drain part and the queue is not emptied (Store.ShadowTick,
// tick.go:2090).
func TestShadowTickPlansADrainWithoutEmptyingTheQueue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.startMachine()
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{Limit: 1}}))
	q, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, q, "the deal queued work-table changes")

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	found := false
	for _, p := range plan.Parts {
		if p.Table == sprint.Work && p.Name == sprint.PartDrain {
			found = true
		}
	}
	assert.True(t, found, "a drain part is planned: %+v", plan.Parts)
	after, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	assert.Len(t, after, len(q), "the queue is not emptied")
}

// TestShadowTickNamesTheReadAPrimaryWaitsOn: a primary waiting on reads is named
// in the plan's Reads (Store.ShadowTick, tick.go:2110).
func TestShadowTickNamesTheReadAPrimaryWaitsOn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.must(SetStep(sprint.SetReq{ReadCards: sprint.ReadCardsOnWord, Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	s := h.snap()
	c := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	require.NotNil(t, c)
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	// every reader is away, so the primary waiting on its reads cannot be dealt one: the plan's Reads says so
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		require.NoError(t, h.st.SetReaderAway(h.ctx, r, true, "coordinator"))
	}

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.Join(plan.Reads, "\n"), "s1-1"), "Reads names the primary: %v", plan.Reads)
}

// TestShadowTickSaysTheSprintIsBusyWhenTheStoreMovesOnEveryRead: a store that
// moves between the fence and the load is refused as busy (Store.ShadowTick,
// tick.go:2169).
func TestShadowTickSaysTheSprintIsBusyWhenTheStoreMovesOnEveryRead(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	mover := &shadowMover{Mem: h.m}
	h.st.B = mover
	_, err := h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the sprint is busy")
}

// shadowMover is a store that moves under the read: it advances the fence's
// generation through the inner Mem on every Shapes read (a write through the
// inner Mem, never Mem's Fail hook, which runs under the lock).
type shadowMover struct {
	*Mem
}

func (w *shadowMover) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	w.Mem.mu.Lock()
	w.Mem.log().gen++
	w.Mem.mu.Unlock()
	return w.Mem.Shapes(ctx, tables)
}

// TestShadowTickLeavesAnEmptyPartOutAndListsADuePart: a custom Updates list
// plans an empty part not at all and a part due with an empty plan with its due
// count (Store.ShadowTick, tick.go:2114).
func TestShadowTickLeavesAnEmptyPartOutAndListsADuePart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.st.Updates = []sprint.TableUpdate{{Parts: []sprint.TickPartDef{
		{Name: "empty-part", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 0 }},
		{Name: "due-part", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 3 }},
	}}}
	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	byName := map[string]ShadowPart{}
	for _, p := range plan.Parts {
		byName[p.Name] = p
	}
	_, empty := byName["empty-part"]
	assert.False(t, empty, "an empty part is left out: %+v", plan.Parts)
	due, ok := byName["due-part"]
	require.True(t, ok, "a due part with an empty plan is listed: %+v", plan.Parts)
	assert.Equal(t, 3, due.Due)
	assert.Equal(t, 0, due.Size)
}

// TestShadowTickDoesNotRecoverAPanickingPart: a part that panics makes
// ShadowTick panic; it recovers nothing (Store.ShadowTick, tick.go:2039).
func TestShadowTickDoesNotRecoverAPanickingPart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.st.Updates = []sprint.TableUpdate{{Parts: []sprint.TickPartDef{
		{Name: "boom", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { panic("the shadow is the canary of a crash") }},
	}}}
	require.Panics(t, func() { _, _ = h.st.ShadowTick(h.ctx) })
}
