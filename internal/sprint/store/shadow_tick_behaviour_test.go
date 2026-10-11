package store

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// A store that bumps the fence's generation on every read of its shapes, as a
// competing writer keeps moving the sprint under a shadow tick's read.
type movingMem struct{ *Mem }

func (m *movingMem) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	f, err := m.Mem.ReadFence(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := m.Mem.Acquire(ctx, f.Gen, OpRecord{ID: "move", Verb: "move"}); err != nil {
		return nil, err
	}
	if err := m.Mem.Release(ctx, OpRecord{ID: "move"}, false); err != nil {
		return nil, err
	}
	return m.Mem.Shapes(ctx, tables)
}

// partByName is the first part of plan with the given name, or nil.
func partByName(plan ShadowPlan, name string) *ShadowPart {
	for i := range plan.Parts {
		if plan.Parts[i].Name == name {
			return &plan.Parts[i]
		}
	}
	return nil
}

// TestShadowTickPlansARunningSprintAndWritesNothing: ShadowTick (tick.go:2047,
// docs/SPEC-SPRINT.md section 14) on a running sprint with ready cards reports
// RUNNING, plans a deal on the work table, sizes the plan as the sum of its
// parts, and leaves the store byte for byte as it was.
func TestShadowTickPlansARunningSprintAndWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.startMachine()
	before, err := h.m.Snapshot()
	require.NoError(t, err)

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, Running, plan.State)
	deal := partByName(plan, "deal")
	require.NotNil(t, deal, "the plan has no deal part: %+v", plan.Parts)
	assert.Equal(t, sprint.Work, deal.Table)

	var sum int
	for _, p := range plan.Parts {
		sum += p.Size
	}
	assert.Equal(t, sum, plan.Size, "the plan's size is the sum of its parts")

	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a shadow tick writes nothing")
}

// TestShadowTickPlansOnAStoppedMachine: a STOPPED machine still plans
// (docs/SPEC-SPRINT.md section 14): the state is reported, nothing is refused,
// and the store is unchanged.
func TestShadowTickPlansOnAStoppedMachine(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	before, err := h.m.Snapshot()
	require.NoError(t, err)

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, Stopped, plan.State)

	after, err := h.m.Snapshot()
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a shadow tick writes nothing")
}

// TestShadowTickRefusesAnEpochThatOwesItsRestore: an epoch advanced without its
// settle still owes the restore, and a shadow tick says so, performs nothing,
// and leaves it owed (tick.go:2060).
func TestShadowTickRefusesAnEpochThatOwesItsRestore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	ok, err := h.m.AdvanceEpoch(h.ctx, 0, h.st.Now())
	require.NoError(t, err)
	require.True(t, ok)

	_, err = h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owes the restore")

	es, err := h.m.Epoch(h.ctx)
	require.NoError(t, err)
	assert.True(t, es.Owed, "the epoch still owes its restore after a shadow tick")
}

// TestShadowTickRefusesAPendingOperation: a shadow tick repairs nothing, so an
// operation left in the fence is a refusal that names the repair it does not do,
// and the operation is still pending after (tick.go:2175).
func TestShadowTickRefusesAPendingOperation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.pendingOp(OpRecord{ID: "cut-1", Verb: "test", At: h.st.Now()})

	_, err := h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repairs nothing")

	require.NotNil(t, h.m.Pending(), "the operation was released")
}

// TestShadowTickPlansQueuedWorkWithoutDrainingTheQueue: queued work-table
// changes are planned as the drain part, and a shadow tick leaves them queued
// (tick.go:2096).
func TestShadowTickPlansQueuedWorkWithoutDrainingTheQueue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(4)
	h.startMachine()
	h.machine()
	h.run(TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 100}, Who: "m1"}))
	s := h.snap()
	var ids []string
	gens := map[string]int{}
	for _, c := range s.Fleet.Cell("m1", sprint.Working) {
		ids = append(ids, c.ID)
		gens[c.ID] = c.Int("gen")
	}
	h.must(FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: ids}, Gens: gens, Who: "m1"}))
	q, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, q, "the finish queued nothing")

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	drain := partByName(plan, sprint.PartDrain)
	require.NotNil(t, drain, "the plan has no drain part: %+v", plan.Parts)

	after, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, len(q), len(after), "a shadow tick drained the queue")
}

// TestShadowTickReadsNamesThePrimaryWaitingOnReads: a primary whose reads are
// not all in is named in the plan's Reads (sprint.ReadCardsWhy, tick.go:2117).
func TestShadowTickReadsNamesThePrimaryWaitingOnReads(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.must(SetStep(sprint.SetReq{ReadCards: sprint.ReadCardsOnWord, Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	s := h.snap()
	c := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(plan.Reads, " "), "s1-1", "the plan's reads do not name the waiting primary: %v", plan.Reads)
}

// TestShadowTickSaysTheSprintIsBusyWhenTheStoreMovesOnEveryRead: a store that
// keeps moving under the read makes the shadow tick say the sprint is busy,
// after its tries (tick.go:2175).
func TestShadowTickSaysTheSprintIsBusyWhenTheStoreMovesOnEveryRead(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	moving := &movingMem{Mem: h.m}
	h.st.B = moving

	_, err := h.st.ShadowTick(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the sprint is busy")
}

// TestShadowTickCustomUpdates: a custom Updates list is planned as given: an
// empty part is left out, a due part with an empty plan is listed with its due
// count, and a part that panics makes ShadowTick panic (it recovers nothing,
// tick.go:2045).
func TestShadowTickCustomUpdates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.startMachine()
	emptyPlan := func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 0 }
	duePlan := func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 7 }
	h.st.Updates = []sprint.TableUpdate{
		{Table: sprint.Work, Parts: []sprint.TickPartDef{{Name: "empty", Fn: emptyPlan}, {Name: "due", Fn: duePlan}}},
	}

	plan, err := h.st.ShadowTick(h.ctx)
	require.NoError(t, err)
	assert.Nil(t, partByName(plan, "empty"), "the empty part is left out: %+v", plan.Parts)
	due := partByName(plan, "due")
	require.NotNil(t, due, "the due part is not listed: %+v", plan.Parts)
	assert.Zero(t, due.Size, "the due part's empty plan is listed with no size")
	assert.Equal(t, 7, due.Due, "the due part lists its due count")

	panicPlan := func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { panic("boom") }
	h.st.Updates = []sprint.TableUpdate{
		{Table: sprint.Work, Parts: []sprint.TickPartDef{{Name: "boom", Fn: panicPlan}}},
	}
	assert.Panics(t, func() { _, _ = h.st.ShadowTick(h.ctx) })
}
