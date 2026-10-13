package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// TestRedisCoverGuardedRowsReadsTheRestoreState pins the guard of Redis.GuardedRows on the
// in-process fake (no socket): the epoch hash's n and defaults fields and the marker on the
// fleet table's properties at the pinned epoch, read under WATCH, decide whether anything is
// written. The writes themselves are the table layer's (the functional tier,
// clear_restore_once_functional_test.go); every case here writes nothing, so none reaches it.
func TestRedisCoverGuardedRowsReadsTheRestoreState(t *testing.T) {
	t.Parallel()
	del := []RowWrite{{Op: "del", Table: "t-readers", Rows: []string{"reader-b"}}}
	cases := []struct {
		name    string
		pinned  uint64
		hash    []any
		marked  bool
		pending bool
		want    GuardOutcome
	}{
		{"a verb at an epoch the sprint left", 2, []any{"n", "3"}, false, false, GuardMoved},
		{"a verb while the restore is pending", 2, []any{"n", "2", "defaults", "2"}, false, false, GuardPending},
		{"the restore after another clear's marker", 2, []any{"n", "2", "defaults", "2"}, true, true, GuardSettled},
		{"the restore over an epoch an older binary made (a stale flag)", 3, []any{"n", "3", "defaults", "2"}, false, true, GuardSettled},
		{"the restore at epoch 0, never owed", 0, nil, false, true, GuardSettled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, ctx := coverRedis(t, tc.pinned)
			r.Pinned = tc.pinned
			if tc.hash != nil {
				require.NoError(t, r.C.HSet(ctx, r.Names.EpochKey(), tc.hash...).Err())
			}
			fleet := r.Names.Table(sprint.Fleet)
			if tc.marked {
				require.NoError(t, r.C.HSet(ctx, ntable.PropsKeyAt(fleet, tc.pinned), sprint.PropMembersRestored, "1").Err())
			}
			got, err := r.GuardedRows(ctx, RestoreGuard{Pending: tc.pending, MarkerTable: fleet, Marker: sprint.PropMembersRestored}, del)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
