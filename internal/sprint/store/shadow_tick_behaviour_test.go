package store

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shadow plans the tick with nothing applied (Store.ShadowTick, docs/SPEC-SPRINT.md
// section 14, install-canary-shadow-tick-r.w1): the plan and its error, as the
// canary verb and the server switch read them.
func shadow(t *testing.T, h *harness) (ShadowPlan, error) {
	t.Helper()
	return h.st.ShadowTick(h.ctx)
}

// mustShadow is the plan a shadow tick must return.
func mustShadow(t *testing.T, h *harness) ShadowPlan {
	t.Helper()
	p, err := shadow(t, h)
	require.NoError(t, err)
	return p
}

// part is the named part of a plan, if it is planned.
func part(p ShadowPlan, name string) (ShadowPart, bool) {
	for _, sp := range p.Parts {
		if sp.Name == name {
			return sp, true
		}
	}
	return ShadowPart{}, false
}

// sumParts is the plan's size as its parts sum it.
func sumParts(p ShadowPlan) int {
	n := 0
	for _, sp := range p.Parts {
		n += sp.Size
	}
	return n
}

// fingerBytes is Mem's snapshot, to prove a read wrote nothing.
func fingerBytes(t *testing.T, h *harness) string {
	t.Helper()
	b, err := h.m.Snapshot()
	require.NoError(t, err)
	return string(b)
}

// TestShadowTickPlansARunningSprintAndWritesNothing: a running sprint with a
// card ready plans a deal on the work table, the size is its parts' sum, and
// the store is unchanged (ShadowTick's doc, tick.go:2031).
func TestShadowTickPlansARunningSprintAndWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.startMachine()
	before := fingerBytes(t, h)
	p := mustShadow(t, h)
	assert.Equal(t, "RUNNING", p.State)
	deal, ok := part(p, "deal")
	require.True(t, ok, "a ready card plans a deal: %+v", p.Parts)
	assert.Positive(t, deal.Size, "the deal writes something")
	assert.Equal(t, sumParts(p), p.Size, "the size is its parts' sum")
	assert.Equal(t, before, fingerBytes(t, h), "a shadow tick writes nothing")
}

// TestShadowTickStillPlansOnAStoppedMachine: a stopped machine is planned too,
// the state reported STOPPED, and the store unchanged (ShadowTick's doc).
func TestShadowTickStillPlansOnAStoppedMachine(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.stopMachine()
	before := fingerBytes(t, h)
	p := mustShadow(t, h)
	assert.Equal(t, "STOPPED", p.State)
	assert.Equal(t, sumParts(p), p.Size)
	assert.Equal(t, before, fingerBytes(t, h), "a shadow tick writes nothing")
}

// TestShadowTickRefusesAnEpochThatOwesItsRestore: a clear cut between its
// advance and its restore is refused, the restore is not performed, and the
// epoch still owes it (ShadowTick's doc: no "restore a clear owes").
func TestShadowTickRefusesAnEpochThatOwesItsRestore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	ok, err := h.m.AdvanceEpoch(h.ctx, 0, h.now)
	require.NoError(t, err)
	require.True(t, ok, "the sprint advanced")
	es, err := h.m.Epoch(h.ctx)
	require.NoError(t, err)
	require.True(t, es.Owed)
	_, err = shadow(t, h)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owes the restore")
	after, err := h.m.Epoch(h.ctx)
	require.NoError(t, err)
	assert.True(t, after.Owed, "the shadow tick performed no restore")
}

// TestShadowTickRefusesWhileAnOperationIsPending: an operation in flight is
// never repaired here; the read says so and the operation is still pending
// (ShadowTick's doc: no "repair of a pending operation").
func TestShadowTickRefusesWhileAnOperationIsPending(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	f, err := h.m.ReadFence(h.ctx)
	require.NoError(t, err)
	got, err := h.m.Acquire(h.ctx, f.Gen, OpRecord{ID: "held"})
	require.NoError(t, err)
	require.True(t, got, "the operation was taken")
	_, err = shadow(t, h)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repairs nothing")
	assert.NotNil(t, h.m.Pending(), "the shadow tick repaired no pending operation")
}

// TestShadowTickPlansAQueuedDrainAndKeepsTheQueue: a queued work-table change
// plans a drain part and the queue is not emptied (ShadowTick's doc: it writes
// nothing, the drain a part of its plan).
func TestShadowTickPlansAQueuedDrainAndKeepsTheQueue(t *testing.T) {
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
	require.NotEmpty(t, ids)
	h.must(FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: ids}, Gens: gens, Who: "m1"}))
	q, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, q, "the finish queued nothing")
	p := mustShadow(t, h)
	drain, ok := part(p, "drain")
	require.True(t, ok, "queued work plans a drain: %+v", p.Parts)
	assert.Positive(t, drain.Size)
	after, err := h.m.QueueRead(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, len(q), len(after), "a shadow tick does not drain the queue")
}

// TestShadowTickNamesTheReadsAPrimaryWaitsOn: a primary waiting for its reads
// is named in the plan's Reads (ShadowTick's doc, ShadowPlan.Reads).
func TestShadowTickNamesTheReadsAPrimaryWaitsOn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.must(SetStep(sprint.SetReq{ReadCards: "on", Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	s := h.snap()
	c := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}}))
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	p := mustShadow(t, h)
	require.NotEmpty(t, p.Reads, "a primary waiting on reads is named")
	found := false
	for _, line := range p.Reads {
		if strings.HasPrefix(line, "s1-1") {
			found = true
		}
	}
	assert.True(t, found, "the plan names s1-1: %v", p.Reads)
}

// movingReads is a Mem that moves on every read: its Shapes reads the inner Mem
// and then writes through it, so the fence's generation changes under the read
// (never Mem's Fail hook, which runs under the Mem's lock and deadlocks).
type movingReads struct {
	*Mem
}

func (m *movingReads) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	out, err := m.Mem.Shapes(ctx, tables)
	f, ferr := m.Mem.ReadFence(ctx)
	if ferr == nil {
		_, _ = m.Mem.Acquire(ctx, f.Gen, OpRecord{ID: "move"})
	}
	return out, err
}

// TestShadowTickRefusesAStoreThatMovesOnEveryRead: the read never holds still,
// so the shadow says the sprint is busy and repairs nothing (ShadowTick's doc;
// shadowRead, tick.go:2169).
func TestShadowTickRefusesAStoreThatMovesOnEveryRead(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.st.B = &movingReads{Mem: h.m}
	_, err := shadow(t, h)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the sprint is busy")
	assert.Contains(t, err.Error(), "a shadow tick repairs nothing")
}

// TestShadowTickPlansCustomUpdatesAndPanicsOnAPanickingPart: a custom Updates
// list leaves an empty part out, lists a due part with an empty plan with its
// due count, and a part that panics is not recovered (ShadowTick's doc).
func TestShadowTickPlansCustomUpdatesAndPanicsOnAPanickingPart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	empty := func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 0 }
	due := func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { return sprint.Plan{}, 3 }
	h.st.Updates = []sprint.TableUpdate{{Table: "", Parts: []sprint.TickPartDef{
		{Name: "empty", Fn: empty},
		{Name: "due", Fn: due},
	}}}
	p := mustShadow(t, h)
	_, hasEmpty := part(p, "empty")
	assert.False(t, hasEmpty, "an empty part is left out: %+v", p.Parts)
	got, ok := part(p, "due")
	require.True(t, ok, "a due part is listed: %+v", p.Parts)
	assert.Equal(t, 3, got.Due, "with its due count")
	assert.Zero(t, got.Size, "and an empty plan")

	h.st.Updates = []sprint.TableUpdate{{Table: "", Parts: []sprint.TickPartDef{
		{Name: "boom", Fn: func(*sprint.Snapshot, sprint.TickReq) (sprint.Plan, int) { panic("boom") }},
	}}}
	assert.Panics(t, func() { _, _ = shadow(t, h) }, "a panicking part is not recovered")
}
