package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOkPctCountsEveryFinishAtFinish pins OkCounts and the where path's friends
// row to the owner's rule of 2026-10-14 (docs/SPEC-SPRINT.md section 1, done
// and ok%): ok% counts every finish at the finish, a pending read is ok and
// pending, a bounce counts nowhere, and a HOLD turns the finish into a failure.
func TestOkPctCountsEveryFinishAtFinish(t *testing.T) {
	t.Parallel()
	since := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) string { return time.Date(2030, 1, 1, 0, 0, sec, 0, time.UTC).Format(time.RFC3339) }
	put := func(f *Table, row, id, col string, fields map[string]string) {
		f.Put(&Card{ID: id, Row: row, Col: col, Fields: fields})
	}
	fleet := func(row string) *Table {
		f := NewTable(Fleet)
		f.SetRows([]string{row})
		return f
	}
	counts := func(f *Table, row string) [3]int {
		ok, failed, pending := OkCounts(f, row, since)
		return [3]int{ok, failed, pending}
	}

	t.Run("an ok finish waiting for its read", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", Finished, map[string]string{"ok": "yes", "head": "h", "finished": at(1)})
		assert.Equal(t, [3]int{1, 0, 1}, counts(f, "m1"), "ok=1, failed=0, pending=1: ok% 100")
	})

	t.Run("the same card after a broken read", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", DoneFailed, map[string]string{"ok": "no", "head": "h", "finished": at(1)})
		assert.Equal(t, [3]int{0, 1, 0}, counts(f, "m1"), "ok=0, failed=1, pending=0")
	})

	t.Run("a worker's own failed finish", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", Finished, map[string]string{"ok": "no", "head": "", "finished": at(1)})
		assert.Equal(t, [3]int{0, 1, 0}, counts(f, "m1"), "finished with ok=no is failed")
	})

	t.Run("an ok finish followed by a passing verdict", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", DoneOK, map[string]string{"ok": "yes", "head": "h", "finished": at(1)})
		assert.Equal(t, [3]int{1, 0, 0}, counts(f, "m1"), "ok=1, pending=0 once read ok")
	})

	t.Run("a bounce, a withdrawal and a redeal count nothing", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", Ready, map[string]string{"ok": "yes", "head": "h", "finished": at(1)}) // working -> ready by the deal
		put(f, "m1", "s2.w1", Withdrawn, map[string]string{"finished": at(1)})
		put(f, "m1", "s3.w1", Redealt, map[string]string{"finished": at(1)})
		assert.Equal(t, [3]int{0, 0, 0}, counts(f, "m1"))
	})

	t.Run("a defect counts nothing", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", DoneDefect, map[string]string{"finished": at(1)})
		assert.Equal(t, [3]int{0, 0, 0}, counts(f, "m1"))
	})

	t.Run("a finish stamped before since counts nothing", func(t *testing.T) {
		f := fleet("m1")
		put(f, "m1", "s1.w1", Finished, map[string]string{"ok": "yes", "head": "h", "finished": at(-1)})
		assert.Equal(t, [3]int{0, 0, 0}, counts(f, "m1"))
	})

	t.Run("the where path formats the friends row", func(t *testing.T) {
		f := fleet(FriendRow("amy"))
		put(f, FriendRow("amy"), "s1.w1", Finished, map[string]string{"ok": "yes", "head": "h", "finished": at(1)})
		put(f, FriendRow("amy"), "s2.w1", Finished, map[string]string{"ok": "no", "head": "", "finished": at(2)})
		ft := FriendsDef()
		ft.Rows = []ntable.Row{{Key: "amy", Cells: make([]ntable.Cell, len(ft.Columns))}}
		ft = OkPctCells(ft, f, since, FriendRow)
		row := ft.Rows[0]
		require.Equal(t, "50.0%", ntable.CellText(ft.Columns, row, ft.Column(OkPct)))
		require.Equal(t, "1", ntable.CellText(ft.Columns, row, ft.Column("pending")))
	})
}
