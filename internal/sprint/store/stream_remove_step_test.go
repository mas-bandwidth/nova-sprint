package store

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestStreamRemoveIsOneFencedOperation: the stream remove verb is one store
// step (store.StreamRemoveStep). It runs inside the store's fence and takes
// the rows of the work and the merge table off in one operation, in one
// exchange: a crash between the two deletes leaves both rows, and the step
// removes both or neither.
func TestStreamRemoveIsOneFencedOperation(t *testing.T) {
	t.Parallel()
	t.Run("a crash between the two deletes leaves both rows", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.stream("s1")
		// the crash: the exchange that takes the rows off does not answer
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
		dels := []sprint.RowDel{{Table: sprint.Merge, Rows: []string{"s1"}}, {Table: sprint.Work, Rows: []string{"s1"}}}
		fence, err := h.m.ReadFence(h.ctx)
		require.NoError(t, err)
		op := OpRecord{ID: "late-remove", Verb: "stream remove", At: h.st.Now()}
		ok, err := h.m.Acquire(h.ctx, fence.Gen, op)
		require.NoError(t, err)
		require.True(t, ok)
		// the removal the operation planned runs, and another writer finishes it
		require.NoError(t, h.st.deleteRows(h.ctx, op.ID, dels))
		require.NoError(t, h.m.Release(h.ctx, op, false))
		// the stream comes back before the late remover runs its delete again
		h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
		err = h.st.deleteRows(h.ctx, op.ID, dels)
		require.Error(t, err, "a delete whose operation left the fence is refused")
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
