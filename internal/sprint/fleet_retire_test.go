package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbsentMembersNamesOnlyTheGone: the tick retires only a row whose record
// the inventory no longer names; a row the inventory still names is never
// absent, however its record reads (an offline machine keeps its row down).
func TestAbsentMembersNamesOnlyTheGone(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2"}))
	assert.Equal(t, []string{"m1"}, AbsentMembers(w.s, []string{"m2"}))
	assert.Empty(t, AbsentMembers(w.s, []string{"m1", "m2"}))
	assert.Equal(t, []string{"m1", "m2"}, AbsentMembers(w.s, nil))
}

// TestTickRetireAbsentRetiresAnEmptyAbsentRow: the part takes an empty row with
// no record off the table (its control card), and leaves a row with a record
// alone. Machines nil (no read) retires nothing.
func TestTickRetireAbsentRetiresAnEmptyAbsentRow(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2"}))

	p, due := TickRetireAbsent(w.s, TickReq{Machines: []string{"m1"}})
	assert.Zero(t, due)
	require.NotEmpty(t, p.Units, "m2 is retired")
	w.must(p)
	assert.Nil(t, w.s.MemberCtl("m2"), "m2's control card is off the table")
	assert.NotNil(t, w.s.MemberCtl("m1"), "m1 stays")

	// no read: nothing retires
	empty, _ := TickRetireAbsent(w.s, TickReq{})
	assert.Empty(t, empty.Units)
}
