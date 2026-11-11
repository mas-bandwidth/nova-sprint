package store

import (
	"context"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statsSince is the board's stats mark (nova-sprint stats reset): the
// consumers an accepted event moved to done after the mark, each with that
// event's seq, and the time of the event that set the mark. A card done
// before the mark, a refused op and a done card written again add nothing;
// a later mark counts from itself.
func statsSince(h *harness) {
	r, ok := h.s.(interface {
		StatsSince(ctx context.Context, mark int64) (StatsMark, error)
	})
	require.True(h.t, ok, "the store reads the stats mark")
	h.seed()                                                                         // seq 1
	h.ok(h.op("report", nil, SetCardState{"u1", "done"}, SetCardResult{"u1", "ok"})) // seq 2: before the mark
	reset := h.op("reset", nil, SetCursor{StatsCursor, 2})
	h.ok(reset)                                                                             // seq 3
	h.refused(h.op("same", nil, SetCursor{StatsCursor, 2}), "cursor stats is already at 2") // seq 4
	h.ok(h.op("read", nil, SetCardState{"u2", "done"}, SetCardResult{"u2", "ok"}))          // seq 5
	h.ok(h.op("again", nil, SetCardReason{"u1", "written again"}))                          // seq 6: done before and after
	h.ok(h.op("fail", nil, SetCardState{"u3", "done"}, SetCardResult{"u3", "fail"}))        // seq 7
	got, err := r.StatsSince(h.ctx, 2)
	require.NoError(h.t, err)
	assert.Equal(h.t, map[string]int64{"u2": 5, "u3": 7}, got.Done)
	assert.Equal(h.t, int64(2), got.Seq)
	assert.True(h.t, got.At.Equal(reset.At.Truncate(time.Microsecond)), "the mark's time is its reset's: %s", got.At)

	again := h.op("reset", nil, SetCursor{StatsCursor, 6}) // seq 8
	h.ok(again)
	got, err = r.StatsSince(h.ctx, 6)
	require.NoError(h.t, err)
	assert.Equal(h.t, map[string]int64{"u3": 7}, got.Done)
	assert.True(h.t, got.At.Equal(again.At.Truncate(time.Microsecond)), "a later mark's time is its own: %s", got.At)

	got, err = r.StatsSince(h.ctx, 8)
	require.NoError(h.t, err)
	assert.Equal(h.t, map[string]int64{}, got.Done, "nothing done since")
	assert.True(h.t, got.At.IsZero(), "no event set the cursor to 8")
}
