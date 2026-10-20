package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestEveryStreamAndHashIsBounded: appending past the bound leaves each stream
// and hash at most at its bound, and the log index names only lines the log
// still holds (tla/StoreRetention.tla Bounded, IndexNeverDangles).
func TestEveryStreamAndHashIsBounded(t *testing.T) {
	t.Parallel()
	m := NewMem()
	ctx := context.Background()
	const bound = 6
	m.retention = bound

	appendOp := func(i int) {
		f, err := m.ReadFence(ctx)
		require.NoError(t, err)
		op := OpRecord{
			ID:   fmt.Sprintf("op%d", i),
			Verb: "test",
			At:   t0,
			Log: []sprint.Line{{
				Kind:  sprint.LineMove,
				At:    t0,
				Epoch: 0,
				Card:  fmt.Sprintf("c%d", i),
			}},
			Notes: []sprint.Note{{
				Kind: sprint.Judgment,
				Type: "test",
				ID:   fmt.Sprintf("n%d", i),
				Who:  "tester",
				At:   t0,
				What: "a judgment",
			}},
			CallerOp: fmt.Sprintf("caller%d", i),
			Result:   "{}",
		}
		ok, err := m.Acquire(ctx, f.Gen, op)
		require.NoError(t, err)
		require.True(t, ok, "acquire op%d", i)
		require.NoError(t, m.Release(ctx, op, true))
	}
	for i := 0; i < bound*3; i++ {
		appendOp(i)
	}
	// index every line, then append past the bound again: the index must not
	// keep an entry for a trimmed line.
	entries := map[string][]string{}
	m.mu.Lock()
	for _, x := range m.log().lines {
		entries["c0"] = append(entries["c0"], x.id)
	}
	m.mu.Unlock()
	require.NoError(t, m.indexLog(ctx, entries, ""))
	for i := bound * 3; i < bound*4; i++ {
		appendOp(i)
	}

	m.mu.Lock()
	l := m.log()
	require.LessOrEqual(t, len(l.lines), bound, "the log is over its bound")
	require.LessOrEqual(t, len(l.inbox), bound, "the inbox is over its bound")
	require.LessOrEqual(t, len(l.notes), bound, "the notes hash is over its bound")
	require.LessOrEqual(t, len(l.done), bound, "the done hash is over its bound")
	require.LessOrEqual(t, len(l.aliases), bound, "the aliases hash is over its bound")
	require.LessOrEqual(t, len(l.answered), bound, "the answered hash is over its bound")
	require.LessOrEqual(t, len(l.open), bound, "the open hash is over its bound")
	held := map[string]bool{}
	for _, x := range l.lines {
		held[x.id] = true
	}
	m.mu.Unlock()

	_, ids, err := m.cardLogIDs(ctx, "c0")
	require.NoError(t, err)
	for _, id := range ids {
		require.True(t, held[id], "the log index names trimmed line %s", id)
	}
}

// TestPastEpochsAreRetired: three clears leave the current and previous epochs'
// keys only, and a caller op from a retired epoch is refused by name, never
// re-run (tla/StoreRetention.tla Retire, ReplayAnswered).
func TestPastEpochsAreRetired(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	first := AddStep(sprint.AddReq{Stream: "s2", Count: 1})
	first.CallerOp = "old-op"
	h.must(first)
	_, ok, err := h.st.B.Done(h.ctx, "old-op")
	require.NoError(t, err)
	require.True(t, ok, "the op is recorded at epoch 0")

	for i := 0; i < 3; i++ {
		_, err := h.st.Clear(h.ctx)
		require.NoError(t, err, "clear %d", i+1)
	}
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), es.N)

	h.m.mu.Lock()
	for e := uint64(0); e < OldestRetained(es.N); e++ {
		require.Nil(t, h.m.logs[e], "epoch %d was not retired", e)
	}
	require.NotNil(t, h.m.logs[es.N], "the current epoch's keys are gone")
	require.NotNil(t, h.m.logs[es.N-1], "the previous epoch's keys are gone")
	h.m.mu.Unlock()

	if _, err := h.st.At(0).Log(h.ctx); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("a retired epoch's log: %v, want a named retirement", err)
	}

	old := uint64(0)
	again := AddStep(sprint.AddReq{Stream: "s3", Count: 1})
	again.CallerOp, again.Epoch = "old-op", &old
	res, err := h.st.Run(h.ctx, again)
	require.NoError(t, err)
	require.Len(t, res.Refused, 1)
	require.Contains(t, res.Refused[0].Why, "older than the store keeps")
	require.Contains(t, res.Refused[0].Why, "not run again")
}

// TestTrimNeverPassesTheCursor: a trim that drops the line at the cursor
// records the first kept line as the gap the coordinator is told of, and a
// trim before the cursor records none (tla/StoreRetention.tla NoTrimPastCursor).
func TestTrimNeverPassesTheCursor(t *testing.T) {
	t.Parallel()
	lines := []memLine{{id: "1-0"}, {id: "2-0"}, {id: "3-0"}, {id: "4-0"}}
	id := func(x memLine) string { return x.id }

	kept, gap := trimRetained(lines, 2, id, "1-0")
	require.Equal(t, []memLine{{id: "3-0"}, {id: "4-0"}}, kept)
	require.Equal(t, "3-0", gap, "a trim past the cursor recorded no gap")

	kept, gap = trimRetained(lines, 2, id, "3-0")
	require.Equal(t, []memLine{{id: "3-0"}, {id: "4-0"}}, kept)
	require.Empty(t, gap, "a cursor the trim kept was called a gap")

	kept, gap = trimRetained(lines, 8, id, "1-0")
	require.Equal(t, lines, kept)
	require.Empty(t, gap)
}

// TestTheLogGapIsTold pins the memory store's gap: trimming past its cursor
// leaves MemLogGap set to the first kept line, never an error.
func TestTheLogGapIsTold(t *testing.T) {
	t.Parallel()
	m := NewMem()
	m.retention = 2
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		f, err := m.ReadFence(ctx)
		require.NoError(t, err)
		op := OpRecord{ID: fmt.Sprintf("op%d", i), Verb: "test", At: t0, Log: []sprint.Line{{Kind: sprint.LineMove, At: t0, Epoch: 0}}}
		ok, err := m.Acquire(ctx, f.Gen, op)
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, m.Release(ctx, op, true))
		if i == 0 {
			// the coordinator read only the first line
			require.NoError(t, m.SetCursor(ctx, "1-0"))
		}
	}
	m.mu.Lock()
	require.NotEmpty(t, m.log().gap, "the trim passed the cursor with no gap recorded")
	m.mu.Unlock()
}
