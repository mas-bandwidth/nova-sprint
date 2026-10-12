package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// fleetBeforeVerdicts is the fleet table's columns and hidden list before the
// end-class migration runs (git show a2aff60e9:internal/sprint/schema.go): the shape
// a live store's fleet table has until the migration runs, the defect column included
// (its own 2026-10-05 migration already taken).
const fleetBeforeVerdicts = "ready,working,width:text:sum,done:sum(ok+failed),okpct:pct(ok/ok+failed):pooled:ok%,status:text,load:text,withdrawn,ok,failed,defect,ctl:first:none"

// The migrations that ship with the readers' verdicts
// (migrations/0001_fleet_finished_redealt.sh, then 0002_fleet_end_class_columns.sh;
// internal/sprint/TABLES.lock, the change of 2026-10-04 and the end-class change of
// 2026-10-12) give a live store's fleet table, as it stood before, exactly the locked
// shape this build writes: its columns in order and its hidden columns. Each line is
// read as the table layer would run it (nova-table col add <table> <spec> --after
// <col>; nova-table col del <table> <col>; nova-table set <table> --hide <col>) and
// applied to the shape; a line of any other form is refused, so the files hold nothing
// this test does not check.
func TestTheMigrationGivesTheFleetTableItsLockedShape(t *testing.T) {
	t.Parallel()
	cols, err := ntable.ParseColumns(fleetBeforeVerdicts)
	require.NoError(t, err)
	hidden := []string{sprint.Withdrawn, sprint.DoneOK, sprint.DoneFailed, sprint.DoneDefect, sprint.Ctl}
	live := sprint.Names{}.Table(sprint.Fleet)
	ran := 0
	for _, name := range []string{"0001_fleet_finished_redealt.sh", "0002_fleet_end_class_columns.sh"} {
		raw, err := os.ReadFile(filepath.Join("migrations", name))
		require.NoError(t, err)
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) == 0 || f[0] != "nova-table" {
				continue
			}
			ran++
			switch {
			case len(f) == 7 && f[1] == "col" && f[2] == "add" && f[5] == "--after":
				require.Equal(t, live, f[3], "%q: the live store's fleet table", line)
				add, err := ntable.ParseColumn(f[4])
				require.NoError(t, err, line)
				at := slices.IndexFunc(cols, func(c ntable.Column) bool { return c.Name == f[6] })
				require.GreaterOrEqual(t, at, 0, "%q: no column %s to add after", line, f[6])
				cols = slices.Insert(cols, at+1, add)
			case len(f) == 5 && f[1] == "col" && f[2] == "del":
				require.Equal(t, live, f[3], "%q: the live store's fleet table", line)
				at := slices.IndexFunc(cols, func(c ntable.Column) bool { return c.Name == f[4] })
				require.GreaterOrEqual(t, at, 0, "%q: no column %s to drop", line, f[4])
				cols = slices.Delete(cols, at, at+1)
			case len(f) == 5 && f[1] == "set" && f[3] == "--hide":
				require.Equal(t, live, f[2], "%q: the live store's fleet table", line)
				hidden = append(hidden, f[4])
			default:
				t.Fatalf("%q: a migration line of a form this test does not apply", line)
			}
		}
	}
	require.Equal(t, 15, ran, "the migrations' eleven column adds, one drop and six hides")
	var want ntable.Table
	for _, def := range (sprint.Names{}).Definitions() {
		if def.Name == live {
			want = def
		}
	}
	assert.Equal(t, want.Columns, cols, "the migrated columns are the locked fleet table's, in order")
	assert.ElementsMatch(t, want.Hidden, hidden, "the migrated hidden columns are the locked fleet table's")
	locked, err := ntable.ParseColumns(lockedFleet)
	require.NoError(t, err)
	assert.Equal(t, locked, cols, "the migrated columns are the locked literal (locked_fleet_test.go)")
}
