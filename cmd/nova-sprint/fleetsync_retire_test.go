package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// readerRowSet is the readers table's row names (the where view's table).
func (ta *testApp) readerRowSet() map[string]bool {
	ta.t.Helper()
	var w whereView
	ta.json("where", &w)
	out := map[string]bool{}
	for name := range w.Tables["readers"] {
		out[name] = true
	}
	return out
}

// TestTickRetiresARenamedMachinesRow: a machine renamed in nova-config leaves its
// old fleet row on the table forever today, held, because nothing ever removes a
// row. The tick now retires it: it reads the same machine records fleet sync
// reads (no new store), and a row whose record is GONE and whose set is empty
// leaves the fleet table and the dashboard, its reader reader-<old> is retired,
// its stats stay queryable under the old name, and one HAPPENED note records it.
// No verb is run: the tick alone does it (the owner, 2026-10-11).
func TestTickRetiresARenamedMachinesRow(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	inv.set("m2", 4)
	ta.ok("fleet sync")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("reader add reader-m1")
	ta.ok("reader add reader-m2")
	require.Contains(t, ta.readerRowSet(), "reader-m1", "the test wants m1's reader")
	require.Contains(t, ta.readerRowSet(), "reader-m2", "the test wants m2's reader")
	inv.remove("m2")
	out := ta.ok("tick")
	assert.Contains(t, out, "m2 removed", "the tick retires the row: %s", out)
	rows := ta.fleetRows()
	assert.Nil(t, rows["m2"], "m2's row leaves the fleet table: %v", rows)
	assert.Contains(t, rows, "m1", "m1 stays")
	assert.NotContains(t, ta.readerRowSet(), "reader-m2", "m2's reader is retired")
	assert.Contains(t, ta.readerRowSet(), "reader-m1", "m1's reader stays")
	// its stats stay queryable under the old name
	assert.Contains(t, ta.ok("log --member m2"), "m2", "m2's history stays under the old name")
	ta.clean()
}

// TestTickRetiresNothingOnAFailedConfigRead: a config read that fails retires
// nothing; never act on a missing read. The row stays exactly as it was.
func TestTickRetiresNothingOnAFailedConfigRead(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	inv.set("m2", 4)
	ta.ok("fleet sync")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("reader add reader-m2")
	inv.remove("m2")
	inv.fail = errors.New("the config cannot be read")
	out := ta.ok("tick")
	assert.NotContains(t, out, "m2 removed", "a failed read retires nothing: %s", out)
	rows := ta.fleetRows()
	require.NotNil(t, rows["m2"], "m2's row stays: %v", rows)
	assert.Contains(t, ta.readerRowSet(), "reader-m2", "m2's reader stays")
	ta.clean()
}

// TestTickLeavesAnOfflineMachineWithARecord: a machine whose record exists but is
// offline stays down, never retired (the owner, 2026-10-10: down is not an error
// when actually down). Only a record GONE from the inventory retires.
func TestTickLeavesAnOfflineMachineWithARecord(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	inv.set("m2", 4)
	ta.ok("fleet sync")
	ta.ok("start")
	ta.ok("tick")
	// m2 stops beating: its record stays in the inventory
	ta.live = []string{"m1"}
	out := ta.ok("tick")
	assert.NotContains(t, out, "m2 removed", "an offline machine with a record is never retired: %s", out)
	rows := ta.fleetRows()
	require.NotNil(t, rows["m2"], "m2's row stays: %v", rows)
	ta.clean()
}

// TestTickDrainsThenRetiresARowWithCards: a row whose record is GONE but that
// still holds cards is drained first (no new deals, its cards dealt to the
// members that stay), and retired on the first tick its set is empty.
func TestTickDrainsThenRetiresARowWithCards(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 8)
	inv.set("m2", 8)
	ta.ok("fleet sync")
	ta.ok("add --stream s1 --count 8")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	require.NotEqual(t, "0", ta.fleetRows()["m2"]["ready"], "the test wants cards on m2: %v", ta.fleetRows())
	inv.remove("m2")
	out := ta.ok("tick")
	assert.Contains(t, out, "-> m1:ready", "m2's cards are dealt to m1: %s", out)
	assert.Contains(t, out, "m2 removed", "and the row is retired once none stays: %s", out)
	rows := ta.fleetRows()
	assert.Nil(t, rows["m2"], "m2's row is gone: %v", rows)
	assert.Equal(t, "8", rows["m1"]["ready"], "every card on m1: %v", rows)
	ta.clean()
}

// TestTickRetireAbsentIsNoOpWithNoInventoryRead pins the core: the pure part
// retires nothing on a nil or empty machine read, so a tick with no inventory
// (every other tick test) never retires a row.
func TestTickRetireAbsentIsNoOpWithNoInventoryRead(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 4)
	ta.ok("fleet sync")
	ta.ok("start")
	ta.ok("tick")
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	snap, err := st.Load(context.Background(), []string{sprint.Fleet, sprint.Work}, nil)
	require.NoError(t, err)
	p, due := sprint.TickRetireAbsent(snap, sprint.TickReq{Who: "tester"})
	assert.Empty(t, p.Units, "no machines read: nothing retires")
	assert.Empty(t, p.Notes)
	assert.Zero(t, due)
}
