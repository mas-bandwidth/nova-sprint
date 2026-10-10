package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// midFlight is a sprint with cards in every column of every table: work
// waiting, ready, working, review, merging and landed; fleet ready, working
// and done; readers asked, reading, ok and broken; merge queued, merged and
// stuck.
func midFlight(t *testing.T) *harness {
	h := newHarness(t)
	h.setup(7)
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"later"}, Needs: []string{"s1-6"}}))
	h.through("s1-1", "s1-2", "s1-3")
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Conflict: "s1-2"}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-4", "s1-5"}}}))
	s := h.snap()
	c := s.Fleet.Card("s1-4.w1")
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
	h.must(FinishStep(sprint.FinishReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
	rs := h.pairAsked("s1-4") // one read begun, one asked: both columns mid-flight
	h.must(ReadStep(sprint.ReadReq{As: rs[0].F("reader"), Begin: true, Sel: sprint.Sel{IDs: []string{rs[0].ID}}}))
	c = h.snap().Fleet.Card("s1-5.w1")
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-6"}}}))
	h.clean("mid-flight")
	s = h.snap()
	for table, cols := range map[*sprint.Table][]string{
		s.Work:    {sprint.Waiting, sprint.Ready, sprint.Working, sprint.Review, sprint.Merging, sprint.Landed},
		s.Fleet:   {sprint.Ready, sprint.Working, sprint.DoneOK},
		s.Readers: {sprint.Asked, sprint.Reading, sprint.OK},
		s.Merge:   {sprint.Queued, sprint.Merged, sprint.Stuck},
	} {
		for _, col := range cols {
			require.NotEmpty(t, table.Column(col), "mid-flight: nothing in %s %s", table.Name, col)
		}
	}
	return h
}

// settleMidFlightStop returns each captured lease by its original owner before
// a clear fixture advances the epoch. START then permits the next run.
func settleMidFlightStop(h *harness) {
	h.t.Helper()
	_, _, _, err := h.st.SetMachine(h.ctx, false)
	require.NoError(h.t, err)
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(h.t, err)
	require.NotEmpty(h.t, m.StopDebt, "the fixture must exercise owner returns")
	for _, debt := range m.StopDebt {
		h.must(StopReturnStep(sprint.StopReturnReq{As: debt.Row, IDs: []string{debt.ID}, Gens: map[string]int{debt.ID: debt.Gen}, Reason: "owner child stopped"}))
	}
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.NoError(h.t, err, "START follows the captured same-owner cancellation receipts")
}

func TestClearStartsAnEmptyEpochAfterAStopReturn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.through("s1-1")
	_, err := h.st.ArchiveStreams(h.ctx, []string{"s1"})
	require.NoError(t, err)
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 1}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s2-1"}}}))
	h.startMachine()
	wc := h.snap().Fleet.Card("s2-1.w1")
	require.NotNil(t, wc)
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: 1}}))
	_, stopped, _, err := h.st.SetMachine(h.ctx, false)
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	h.must(StopReturnStep(sprint.StopReturnReq{As: wc.Row, IDs: []string{wc.ID}, Gens: map[string]int{wc.ID: 1}, Reason: "child exited"}))
	old, err := h.st.At(0).Load(h.ctx, All, nil)
	require.NoError(t, err)
	require.Contains(t, old.Work.Rows(), "s1", "the old epoch retains its archived stream")
	require.Contains(t, old.Work.Rows(), "s2", "the old epoch retains its work stream")

	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err, "a stop-returned lease does not block clear")
	after := h.snap()
	for _, table := range []*sprint.Table{after.Work, after.Merge, after.Readers, after.Fleet} {
		require.Empty(t, table.Rows(), "%s rows leaked into epoch %d", table.Name, after.Epoch)
	}
}

func TestClearRefusesWithEveryLiveLeaseAndOwner(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1", "s1-2"}}}))
	h.startMachine()
	owners := map[string]bool{}
	for _, id := range []string{"s1-1", "s1-2"} {
		wc := h.snap().Fleet.Card(id + ".w1")
		require.NotNil(t, wc)
		owners[wc.Row] = true
		h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: 1}}))
	}
	res, err := h.st.Clear(h.ctx)
	require.Error(t, err)
	require.Len(t, res.Refused, 2)
	for _, id := range []string{"s1-1.w1@1", "s1-2.w1@1"} {
		require.Contains(t, err.Error(), id, "clear refusal must list all live leases and their owner rows")
	}
	for owner := range owners {
		require.Contains(t, err.Error(), owner, "clear refusal must list each lease's owner row")
	}
}

func TestClearStopsTheSprintAndClearsAllWork(t *testing.T) {
	t.Parallel()
	h := midFlight(t)
	before := h.snap()
	res, err := h.st.Clear(h.ctx)
	require.ErrorContains(t, err, "captured owner work/read leases", "clear must not erase work while STOP still owes an owner cancellation receipt")
	require.Contains(t, err.Error(), "s1-4.w1@1", "clear names a live lease")
	require.Contains(t, err.Error(), "m1", "clear names the lease's owner row")
	assert.Equal(t, uint64(0), h.snap().Epoch, "a refused clear keeps the old epoch for the owner to return its children")
	m, _, merr := h.st.Machine(h.ctx)
	require.NoError(t, merr)
	require.Equal(t, Stopped, m.State)
	require.NotEmpty(t, m.StopDebt)
	for _, debt := range m.StopDebt {
		h.must(StopReturnStep(sprint.StopReturnReq{As: debt.Row, IDs: []string{debt.ID}, Gens: map[string]int{debt.ID: debt.Gen}, Reason: "owner child stopped"}))
	}
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "START follows the captured same-owner cancellation receipts")
	res, err = h.st.Clear(h.ctx)
	m, _, merr = h.st.Machine(h.ctx)
	stopped := merr == nil && res.Machine == Running && m.State == Stopped
	require.NoError(t, err, "clear: %v", err)
	if !stopped || res.From != 0 || res.To != 1 || res.Held["primaries"] != 8 || res.Held["merge cards"] != 3 {
		require.Fail(t, fmt.Sprintf("clear: stopped %v %+v", stopped, res))
	}
	after := h.snap()
	require.Equal(t, uint64(1), after.Epoch, "epoch %d", after.Epoch)
	for _, table := range []*sprint.Table{after.Work, after.Readers, after.Merge, after.Fleet} {
		require.Empty(t, table.Rows(), "%s rows leaked into the new epoch", table.Name)
	}
	if open, _ := h.st.Inbox(h.ctx, 0, 0, 100); len(open.Groups) != 1 || open.Groups[0].Type != sprint.NMachineStopped {
		require.Fail(t, fmt.Sprintf("the new epoch's inbox is not the one line that the machine is STOPPED: %+v", open.Groups))
	}
	h.clean("cleared")

	// Every writer holding the old epoch is refused, naming the clear.
	held := uint64(0)
	for name, step := range map[string]Step{
		"finish": FinishStep(sprint.FinishReq{As: before.Fleet.Card("s1-5.w1").Row, Sel: sprint.Sel{IDs: []string{"s1-5.w1"}}, Gens: map[string]int{"s1-5.w1": 1}}),
		"read":   ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: before.Readers.Of("s1-4")[1].F("reader"), Verdict: "ok", Sel: sprint.Sel{IDs: []string{before.Readers.Of("s1-4")[1].ID}}}),
		"merge":  MergeStep(sprint.MergeReq{Stream: "s1"}),
	} {
		step.Epoch = &held
		res, err := h.st.Run(h.ctx, step)
		require.NoError(t, err, "a late %s of the old epoch: %+v %v", name, res, err)
		require.Empty(t, res.Moved, "a late %s of the old epoch: %+v %v", name, res, err)
		require.Len(t, res.Refused, 1, "a late %s of the old epoch: %+v %v", name, res, err)
		require.Contains(t, res.Refused[0].Why, "cleared at", "a late %s of the old epoch: %+v %v", name, res, err)
		require.Contains(t, res.Refused[0].Why, "epoch is now 1", "a late %s of the old epoch: %+v %v", name, res, err)
	}

	// The old epoch stays readable.
	old, err := h.st.At(0).Load(h.ctx, All, nil)
	require.NoError(t, err, "the old epoch: %v", err)
	require.Equal(t, sprint.Landed, old.StateOf("s1-1"), "the old epoch: %v", err)
	require.Equal(t, sprint.Stuck, old.Merge.Placed("s1-2").Col, "the old epoch: %v", err)
	card, err := h.st.At(0).CardOf(h.ctx, "s1-3")
	require.NoError(t, err, "card at the old epoch: %+v %v", card, err)
	require.NotNil(t, card.Primary, "card at the old epoch: %+v %v", card, err)
	require.Equal(t, sprint.Merging, card.Primary.Col, "card at the old epoch: %+v %v", card, err)
	v, err := h.st.At(0).Inbox(h.ctx, 0, 0, 1000)
	require.NoError(t, err, "the old epoch's inbox: %+v %v", v, err)
	require.NotEmpty(t, v.Groups, "the old epoch's inbox: %+v %v", v, err)

	// The same ids run again from the blank new epoch.
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "new work starts only after the cleared sprint starts its new run")
	h.through("s1-1", "s1-2", "s1-3")
	h.must(MergeStep(sprint.MergeReq{Stream: "s1"}))
	if s := h.snap(); s.StateOf("s1-1") != sprint.Landed || s.StateOf("s1-3") != sprint.Landed {
		require.Failf(t, "", "the same ids again: %s %s", s.StateOf("s1-1"), s.StateOf("s1-3"))
	}
	h.clean("landed again")

	// Clear twice in a row.
	for want := uint64(2); want <= 3; want++ {
		res, err := h.st.Clear(h.ctx)
		require.NoError(t, err, "clear to %d: %+v %v", want, res, err)
		require.Equal(t, want, res.To, "clear to %d: %+v %v", want, res, err)
		h.clean("cleared again")
	}
}

// A clear cut after the epoch advanced leaves the new epoch blank; the next
// clear advances again without restoring the closed epoch's shape.
func TestACutClearIsFinishedByTheNext(t *testing.T) {
	t.Parallel()
	h := midFlight(t)
	settleMidFlightStop(h)
	h.m.Fail = func(p string) error {
		if strings.HasPrefix(p, "apply ") {
			return errors.New("cut")
		}
		return nil
	}
	_, err := h.st.Clear(h.ctx)
	require.Error(t, err, "not cut")
	h.m.Fail = nil
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err, "the next clear: %+v %v", res, err)
	require.False(t, res.Restored, "a blank-slate clear has no shape restore")
	require.Equal(t, uint64(1), res.From, "the next clear: %+v %v", res, err)
	require.Equal(t, uint64(2), res.To, "the next clear: %+v %v", res, err)
	s := h.snap()
	for _, table := range []*sprint.Table{s.Work, s.Readers, s.Merge, s.Fleet} {
		require.Empty(t, table.Rows(), "%s rows leaked into epoch %d", table.Name, s.Epoch)
	}
	h.clean("finished and cleared")
}

// Teardown after clears names every epoch's keys: the store is left as it was
// before init.
func TestTeardownAfterClearsLeavesNoKey(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	m := NewMem()
	h.m, h.st.B = m, m
	before := m.Keys(h.st.Names)
	require.NoError(t, h.st.Init(h.ctx))
	require.NoError(t, m.RowsAdd(h.ctx, "t-readers", []string{"reader-a", "reader-b", "reader-c"}))
	h.beat()
	h.setup(2)
	h.through("s1-1")
	for i := 0; i < 2; i++ {
		_, err := h.st.Clear(h.ctx)
		require.NoError(t, err)
		h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 2}))
		h.startMachine() // clear leaves STOPPED; the next epoch needs its own run
		h.through("s1-1")
	}
	_, err := h.st.Teardown(h.ctx)
	require.NoError(t, err)
	if after := m.Keys(h.st.Names); !slices.Equal(after, before) {
		require.Fail(t, fmt.Sprintf("after teardown:\n%s\nbefore init:\n%s", strings.Join(after, "\n"), strings.Join(before, "\n")))
	}
}

// clearAtTick is the store as a tick sees it when a clear lands between the
// tick's read and its first write: the clear runs, by another writer, just
// before the tick's first part takes the fence.
type clearAtTick struct {
	Backend
	kv    KV
	clear func()
	done  *bool
}

func (c clearAtTick) GetKey(ctx context.Context, name string) (string, bool, error) {
	return c.kv.GetKey(ctx, name)
}

func (c clearAtTick) SetKey(ctx context.Context, name, value string) error {
	return c.kv.SetKey(ctx, name, value)
}

func (c clearAtTick) SetKeyShowing(ctx context.Context, name, value, view, state string) error {
	return c.kv.SetKeyShowing(ctx, name, value, view, state)
}

func (c clearAtTick) ShowState(ctx context.Context, view, state string) error {
	return c.kv.ShowState(ctx, view, state)
}

func (c clearAtTick) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	if !*c.done && strings.HasPrefix(op.Verb, "tick ") {
		*c.done = true
		c.clear()
	}
	return c.Backend.Acquire(ctx, gen, op)
}

// A tick in flight at a clear is refused as stale and writes nothing; the
// loop goes on at the new epoch, where the machine is STOPPED until start.
func TestATickInFlightAtAClearIsRefusedAsStale(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(4)
	h.startMachine()
	var cleared ClearResult
	done := false
	loop := *h.st
	loop.Actor = sprint.MachineActor
	loop.B = clearAtTick{Backend: h.m, kv: h.m, done: &done, clear: func() {
		var err error
		cleared, err = h.st.Clear(h.ctx)
		assert.NoError(t, err, "clear: %v", err)
	}}
	res, err := loop.Tick(h.ctx)
	if err != nil || !done || res.Stale == "" || len(res.Moved()) != 0 {
		require.Failf(t, "", "the tick at a clear: stale %q moved %v err %v", res.Stale, res.Moved(), err)
	}
	require.Equal(t, Running, cleared.Machine, "clear: %+v", cleared)
	require.Equal(t, uint64(1), cleared.To, "clear: %+v", cleared)
	old := h.st.At(0)
	s, err := old.Load(h.ctx, All, nil)
	require.NoError(t, err)
	n := len(s.Work.Column(sprint.Working))
	require.Equal(t, 0, n, "the stale tick dealt %d cards at the old epoch", n)
	// The loop goes on: the machine is STOPPED at the new epoch.
	res, err = loop.Tick(h.ctx)
	require.NoError(t, err, "the next tick: %+v %v", res, err)
	require.Equal(t, Stopped, res.State, "the next tick: %+v %v", res, err)
	require.Empty(t, res.Parts, "the next tick: %+v %v", res, err)
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 4}))
	h.startMachine()
	res, err = loop.Tick(h.ctx)
	require.NoError(t, err, "the first tick at the new epoch: %+v %v", res, err)
	require.Empty(t, res.Stale, "the first tick at the new epoch: %+v %v", res, err)
	require.NotEmpty(t, res.Moved(), "the first tick at the new epoch: %+v %v", res, err)
	s = h.snap()
	require.Equal(t, uint64(1), s.Epoch, "nothing dealt at epoch %d", s.Epoch)
	require.NotEmpty(t, s.Work.Column(sprint.Working), "nothing dealt at epoch %d", s.Epoch)
	h.clean("after the stale tick")
}

// A step that answers a judgment of another epoch is refused whole: nothing
// moves, and the refusal names the id's epoch and when the sprint was
// cleared.
func TestAnAnswerOfAnotherEpochRefusesTheWholeStep(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	c := h.snap().Fleet.Card("s1-1.w1")
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}, Failed: true}))
	open, err := h.m.OpenNotes(h.ctx)
	require.NoError(t, err, "no judgment at epoch 0: %v", err)
	require.NotEmpty(t, open, "no judgment at epoch 0: %v", err)
	old := open[0].Note.ID
	h.tick(time.Minute)
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 2}))
	got := h.run(DropStep(sprint.DropReq{Sel: sprint.Sel{IDs: []string{"s1-1", "s1-2"}}, Reason: "obsolete", Answers: []string{old}}))
	require.Empty(t, got.Moved, "the drop answering %s: %+v", old, got)
	require.Len(t, got.Refused, 1, "the drop answering %s: %+v", old, got)
	require.Equal(t, old, got.Refused[0].Key, "the drop answering %s: %+v", old, got)
	require.Contains(t, got.Refused[0].Why, "epoch 0", "the drop answering %s: %+v", old, got)
	require.Contains(t, got.Refused[0].Why, res.At.UTC().Format(time.RFC3339), "the drop answering %s: %+v", old, got)
	require.Equal(t, sprint.Ready, h.state("s1-1"), "the refused step moved: %s %s", h.state("s1-1"), h.state("s1-2"))
	require.Equal(t, sprint.Ready, h.state("s1-2"), "the refused step moved: %s %s", h.state("s1-1"), h.state("s1-2"))
}
