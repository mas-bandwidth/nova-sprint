package store

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestADrainingAbsentRowKeepsItsReader pins the tick's reader-retirement timing
// (internal/sprint/store/tick.go, sprint.TickRetireAbsent): a fleet row whose
// machine record is gone from the inventory but that still holds a withdrawn
// card is drained first and stays on the table, and its reader reader-<old>
// stays with it. Only the reader of a row the tick actually deleted leaves. The
// retired readers are the ones DropMembers returned (the rows it deleted), not
// every absent member: with the wrong list a draining row loses its reader a
// tick early.
func TestADrainingAbsentRowKeepsItsReader(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.startMachine()
	// m2 drains: a withdrawn card stays on it, so memberKeeps(m2) > 0 and the
	// retire part keeps its control card; the row leaves only once none does.
	h.poke(sprint.Fleet, ntable.BatchMemberEntry{
		ID: "s1-1.w9", Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Withdrawn, Score: 1},
		Set:    map[string]string{"kind": "work", "primary": "s1-1", "stream": "s1", "gen": "1"}})
	require.NoError(t, h.m.RowsAdd(h.ctx, "t-readers", []string{sprint.ReaderPrefix + "m2"}))
	// The inventory no longer names m2: it is absent, but it drains.
	h.st.Inventory = func(context.Context) ([]string, bool) { return []string{"m1"}, true }

	// the tick that reads the inventory: m2 stays, and so does its reader
	assert.True(t, h.snap().Fleet.HasRow("m2"), "the fixture: m2 is on the table")
	h.machine()
	require.True(t, h.snap().Fleet.HasRow("m2"), "a draining row stays on the table")
	rows, err := h.st.ReaderRows(h.ctx)
	require.NoError(t, err)
	assert.Contains(t, rows, sprint.ReaderPrefix+"m2", "a draining row keeps its reader until it leaves")
}
