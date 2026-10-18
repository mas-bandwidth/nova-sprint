package store

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// The where record and the fleet's text cells, 2026-10-17 (a 12-tick idle profile of the
// server, the machine STOPPED): SyncFleet writes a member's load cell whenever its highest
// load of the last 10 s changes, each write a revision of the fleet table, and keepWhere
// counted the whole sprint again at every one for a cell its count never reads. A write of
// text cells alone now writes the record again at the fleet's revision, uncounted; a write
// of a fleet record, row or property counts it.

// whereIdle is a harness ticked until a tick is idle: the where record counted and kept.
func whereIdle(t *testing.T) (*harness, *Store) {
	t.Helper()
	h := newHarness(t)
	holesUp(h, 2)
	for i := 0; ; i++ {
		require.Less(t, i, 20, "the tick never went idle")
		if h.machine().Idle {
			break
		}
	}
	st, err := h.st.Pinned(h.ctx)
	require.NoError(t, err)
	return h, st
}

// whereRaw is the where record as stored, raw.
func (h *harness) whereRaw() string {
	h.t.Helper()
	raw, ok, err := h.m.GetKey(h.ctx, keyWhere)
	require.NoError(h.t, err)
	require.True(h.t, ok)
	return raw
}

// fleetRevision is the fleet table's revision now.
func (h *harness) fleetRevision(st *Store) uint64 {
	h.t.Helper()
	sh, err := st.B.Shapes(h.ctx, []string{st.Names.Table(sprint.Fleet)})
	require.NoError(h.t, err)
	return sh[0].Revision
}

// whereCounted is how many times the twin's keepWhere counted the record.
func whereCounted(st *Store) int {
	tw := st.twin()
	tw.whereMu.Lock()
	defer tw.whereMu.Unlock()
	return tw.whereCounts
}

// A load cell written (as SyncFleet writes it) moves the fleet's revision and no record:
// the record is written again at the new revision, the same record, uncounted; again for
// a status cell. Then a record of the fleet written by hand is counted.
func TestALoadCellMovesNoWhereRecount(t *testing.T) {
	t.Parallel()
	h, st := whereIdle(t)
	before, ok := h.whereRecord()
	require.True(t, ok)
	counted := whereCounted(st)
	fleet := st.Names.Table(sprint.Fleet)

	for i, cell := range []map[string]string{{sprint.Load: "42.0%"}, {sprint.Load: "7.5%"}, {sprint.Status: sprint.Up}} {
		require.NoError(t, h.m.RowSet(h.ctx, fleet, "m1", cell))
		rev := h.fleetRevision(st)
		require.Greater(t, rev, before.FleetRev)
		require.NoError(t, st.keepWhere(h.ctx, Machine{}))
		assert.Equal(t, counted, whereCounted(st), "cell %d: not counted", i)
		after, ok := h.whereRecord()
		require.True(t, ok)
		assert.Equal(t, rev, after.FleetRev, "cell %d: written at the fleet's revision now, so where takes its working cards", i)
		after.FleetRev = before.FleetRev
		assert.Equal(t, before, after, "cell %d: the same record", i)
		// and the next keep reads it current: nothing written
		raw := h.whereRaw()
		require.NoError(t, st.keepWhere(h.ctx, Machine{}))
		assert.Equal(t, raw, h.whereRaw())
	}

	// a record of the fleet moved: counted
	s := h.snap()
	c := s.Fleet.Card(sprint.CtlID("m1"))
	require.NotNil(t, c)
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: fleet, Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(s.Fleet.Revision), OperationID: "by-hand-fleet",
		Members: []ntable.BatchMemberEntry{{ID: st.sid(c.ID), Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{"note": "moved"}}}})
	require.NoError(t, err)
	require.NoError(t, st.keepWhere(h.ctx, Machine{}))
	assert.Equal(t, counted+1, whereCounted(st), "a fleet record moved: counted")
	rec, ok := h.whereRecord()
	require.True(t, ok)
	assert.Equal(t, h.fleetRevision(st), rec.FleetRev)
}

// A fleet property (a provider's reconciliation lives in one, which the streams' figures
// read) and a fleet row are counted, though no record moved with them.
func TestAFleetPropertyOrRowIsCountedInWhere(t *testing.T) {
	t.Parallel()
	h, st := whereIdle(t)
	counted := whereCounted(st)
	fleet := st.Names.Table(sprint.Fleet)
	s := h.snap()
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: fleet, Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(s.Fleet.Revision), OperationID: "fleet-prop",
		Members: []ntable.BatchMemberEntry{}, Props: map[string]string{"probe_index": "1"}})
	require.NoError(t, err)
	require.NoError(t, st.keepWhere(h.ctx, Machine{}))
	assert.Equal(t, counted+1, whereCounted(st), "a property moved: counted")

	require.NoError(t, h.m.RowsAdd(h.ctx, fleet, []string{"m9"}))
	require.NoError(t, st.keepWhere(h.ctx, Machine{}))
	assert.Equal(t, counted+2, whereCounted(st), "a row added: counted")
	rec, ok := h.whereRecord()
	require.True(t, ok)
	assert.Equal(t, h.fleetRevision(st), rec.FleetRev)

	// a record this process did not write (another writer's, or a restart's) is counted
	require.NoError(t, h.m.RowSet(h.ctx, fleet, "m1", map[string]string{sprint.Load: "1.0%"}))
	rec.Held = 99
	h.putWhere(rec)
	require.NoError(t, st.keepWhere(h.ctx, Machine{}))
	assert.Equal(t, counted+3, whereCounted(st), "not the record it wrote: counted")
	again, ok := h.whereRecord()
	require.True(t, ok)
	assert.NotEqual(t, 99, again.Held)
}
