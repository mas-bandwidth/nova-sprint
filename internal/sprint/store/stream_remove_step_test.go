package store

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestStreamRemoveIsOneFencedOperation: the stream remove verb is one store
// step (store.StreamRemoveStep). It runs inside the store's fence and takes
// the rows of the work and the merge table off in one operation: a crash
// before the exchange leaves both rows, repair finishes the delete and
// leaves no rows behind, a late remover leaves a stream that came back
// untouched, and the step removes both or neither.
func TestStreamRemoveIsOneFencedOperation(t *testing.T) {
	t.Parallel()
	t.Run("a crash between the two deletes leaves both rows", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		// the crash: the one exchange that takes the rows off does not answer
		h.m.Fail = func(point string) error {
			if point == "guarded-rows" {
				return errors.New("the store did not answer")
			}
			return nil
		}
		res, err := h.st.Run(h.ctx, StreamRemoveStep([]string{"s1"}))
		require.Error(t, err, "the crash is reported")
		require.NotEmpty(t, res.Pending, "the operation stays in the fence")
		h.m.Fail = nil
		s := h.snap()
		require.True(t, s.Work.HasRow("s1"), "the work row is still there")
		require.True(t, s.Merge.HasRow("s1"), "the merge row is still there")
	})
	t.Run("repair finishes a crashed remove and leaves no rows behind", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		h.m.Fail = func(point string) error {
			if point == "guarded-rows" {
				return errors.New("the store did not answer")
			}
			return nil
		}
		res, err := h.st.Run(h.ctx, StreamRemoveStep([]string{"s1"}))
		require.Error(t, err, "the crash is reported")
		require.NotEmpty(t, res.Pending, "the operation stays in the fence")
		h.m.Fail = nil
		rep, err := h.st.Repair(h.ctx)
		require.NoError(t, err)
		require.Len(t, rep, 1)
		require.Equal(t, RepairFinished, rep[0].Done)
		fence, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		require.Nil(t, fence.Pending, "the fence is empty after repair")
		s := h.snap()
		require.False(t, s.Work.HasRow("s1"), "repair took the work row off")
		require.False(t, s.Merge.HasRow("s1"), "repair took the merge row off")
	})
	t.Run("the step takes both rows off in one operation", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		fence, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		exchanges := h.m.Calls["guarded-rows"]
		h.must(StreamRemoveStep([]string{"s1"}))
		s := h.snap()
		require.False(t, s.Work.HasRow("s1"), "the work row is off the table")
		require.False(t, s.Merge.HasRow("s1"), "the merge row is off the table")
		require.Equal(t, exchanges+1, h.m.Calls["guarded-rows"], "the rows of both tables went in one exchange")
		after, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		require.Equal(t, fence.Gen+1, after.Gen, "the removal was one operation")
	})
	t.Run("a late remover leaves a stream that came back", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		h.m.Fail = func(point string) error {
			if point == "guarded-rows" {
				return errors.New("the store did not answer")
			}
			return nil
		}
		_, err := h.st.Run(h.ctx, StreamRemoveStep([]string{"s1"}))
		require.Error(t, err)
		h.m.Fail = nil
		fence, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		require.NotNil(t, fence.Pending)
		held := *fence.Pending
		// repair finishes the operation and releases the fence
		rep, err := h.st.Repair(h.ctx)
		require.NoError(t, err)
		require.Len(t, rep, 1)
		require.Equal(t, RepairFinished, rep[0].Done)
		// the stream comes back before the late remover runs its delete again
		h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
		// the late remover takes the release path, not a test-only delete
		require.NoError(t, h.st.B.Release(h.ctx, held, true))
		s := h.snap()
		require.True(t, s.Work.HasRow("s1"), "the stream that came back keeps its work row")
		require.True(t, s.Merge.HasRow("s1"), "the stream that came back keeps its merge row")
	})
	t.Run("the refusals are kept", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		// a stream that holds a card is not removed
		h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 1, Brief: proBrief}))
		res := h.run(StreamRemoveStep([]string{"s1", "s2", "zz"}))
		require.Len(t, res.Refused, 2, "the stream that holds a card and the name that is no row are refused")
		require.Equal(t, "s2", res.Refused[0].Key)
		require.Equal(t, "zz", res.Refused[1].Key)
		s := h.snap()
		require.False(t, s.Work.HasRow("s1"), "the stream that may leave is off the work table")
		require.False(t, s.Merge.HasRow("s1"), "the stream that may leave is off the merge table")
		require.True(t, s.Work.HasRow("s2"), "the stream that holds a card keeps its row")
		require.True(t, s.Merge.HasRow("s2"), "the stream that holds a card keeps its row")
	})
}

// stream is a stream of the work and the merge table that holds no card: its
// rows are there, so a stream remove may take them off.
func (h *harness) stream(name string) {
	h.t.Helper()
	require.NoError(h.t, h.m.RowsAdd(h.ctx, "t-work", []string{name}))
	require.NoError(h.t, h.m.RowsAdd(h.ctx, "t-merge", []string{name}))
}

// TestRedisReleaseOfAnUnheldRemovalIsANoOp: G5. A release of a stream
// remove whose fence no longer holds it writes nothing (no row delete) and
// succeeds, as a release of any operation the fence no longer holds does.
func TestRedisReleaseOfAnUnheldRemovalIsANoOp(t *testing.T) {
	t.Parallel()
	t.Run("another operation holds the fence", func(t *testing.T) {
		t.Parallel()
		r, ctx := coverRedis(t, 0)
		require.NoError(t, r.C.Set(ctx, r.key(keyFence), mustJSON(t, OpRecord{ID: "op9", Verb: "tick"}), 0).Err())
		late := OpRecord{ID: "op1", Verb: "stream remove", RowDels: []sprint.RowDel{{Table: sprint.Work, Rows: []string{"s1"}}}}
		require.NoError(t, r.Release(ctx, late, true), "a release of an unheld operation is success")
		held, err := r.heldOp(ctx)
		require.NoError(t, err)
		require.Equal(t, "op9", held, "the other operation keeps the fence")
	})
	t.Run("the fence is empty", func(t *testing.T) {
		t.Parallel()
		r, ctx := coverRedis(t, 0)
		late := OpRecord{ID: "op1", Verb: "stream remove", RowDels: []sprint.RowDel{{Table: sprint.Work, Rows: []string{"s1"}}}}
		require.NoError(t, r.Release(ctx, late, true), "a release of an unheld operation is success")
	})
}

// TestMemReleaseOfAnUnheldRemovalIsANoOp: G5 on Mem. The owner check and the
// two row deletes are one step under Mem's lock, so a release whose fence no
// longer holds the operation writes no row and succeeds, as on Redis.
func TestMemReleaseOfAnUnheldRemovalIsANoOp(t *testing.T) {
	t.Parallel()
	late := OpRecord{ID: "op1", Verb: "stream remove", RowPrefix: "t-",
		RowDels: []sprint.RowDel{{Table: sprint.Work, Rows: []string{"s1"}}}}
	t.Run("another operation holds the fence", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		fence, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		ok, err := h.m.Acquire(h.ctx, fence.Gen, OpRecord{ID: "op9", Verb: "tick"})
		require.NoError(t, err)
		require.True(t, ok, "the other operation takes the fence")
		require.NoError(t, h.st.B.Release(h.ctx, late, true), "a release of an unheld operation is success")
		held, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		require.NotNil(t, held.Pending, "the fence is not emptied")
		require.Equal(t, "op9", held.Pending.ID, "the other operation keeps the fence")
		requireMemRow(t, h, "t-work", "s1", true)
		requireMemRow(t, h, "t-merge", "s1", true)
	})
	t.Run("the fence is empty", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		require.NoError(t, h.st.B.Release(h.ctx, late, true), "a release of an unheld operation is success")
		requireMemRow(t, h, "t-work", "s1", true)
		requireMemRow(t, h, "t-merge", "s1", true)
	})
}

// requireMemRow says a row is on or off a Mem table, read without a fenced
// store read that would finish a pending operation.
func requireMemRow(t *testing.T, h *harness, table, row string, want bool) {
	t.Helper()
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	tb, err := h.m.table(table)
	require.NoError(t, err)
	has := false
	for _, r := range tb.at(h.m.active(tb)).rows {
		if r == row {
			has = true
			break
		}
	}
	require.Equal(t, want, has, "%s row %s", table, row)
}

// Row deletes are ordinary plan data; Run must not need constructor side
// effects or a synthetic close-timer write to notice this operation.
func TestRowOnlyPlanRunsWithoutTimerOrBackendCarrier(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.stream("s1")
	h.must(Step{Verb: "remove rows", Args: ArgsOf([]string{"s1"}), Load: tables(sprint.Work, sprint.Merge),
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.StreamRemove(s, []string{"s1"}) }})
	s := h.snap()
	require.False(t, s.Work.HasRow("s1"), "Run ignored a row-only plan without a synthetic timer or backend carrier")
	require.False(t, s.Merge.HasRow("s1"))
}

func TestStreamRemoveSharedTwinSeesDeletedRows(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.stream("s1")
	h.st.ShareTwin(NewTwin())
	h.must(StreamRemoveStep([]string{"s1"}))
	res := h.run(StreamRemoveStep([]string{"s1"}))
	require.Len(t, res.Refused, 1, "a second remove must see the stream is absent, not a cached row")
	require.Equal(t, "s1", res.Refused[0].Key)
}
