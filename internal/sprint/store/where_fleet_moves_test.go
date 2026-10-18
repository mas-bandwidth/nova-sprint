package store

import (
	"context"
	"fmt"
	"testing"
	"time"

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

// moveFleetRecord writes a field of m1's control card by hand: a fleet record moved.
func moveFleetRecord(h *harness, st *Store, note string) {
	h.t.Helper()
	s := h.snap()
	c := s.Fleet.Card(sprint.CtlID("m1"))
	require.NotNil(h.t, c)
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(s.Fleet.Revision), OperationID: "by-hand-" + note,
		Members: []ntable.BatchMemberEntry{{ID: st.sid(c.ID), Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{"note": note}}}})
	require.NoError(h.t, err)
}

// setClock sets the harness's clock, no beat.
func (h *harness) setClock(at time.Time) {
	h.mu.Lock()
	h.now = at
	h.mu.Unlock()
}

// loadCellKeep writes m1's load cell and keeps the where record: whether it was counted.
func loadCellKeep(h *harness, st *Store, load string) bool {
	h.t.Helper()
	n := whereCounted(st)
	require.NoError(h.t, h.m.RowSet(h.ctx, st.Names.Table(sprint.Fleet), "m1", map[string]string{sprint.Load: load}))
	require.NoError(h.t, st.keepWhere(h.ctx, Machine{}))
	rec, ok := h.whereRecord()
	require.True(h.t, ok)
	require.Equal(h.t, h.fleetRevision(st), rec.FleetRev)
	return whereCounted(st) > n
}

// The count reads the clock (the reads' 30-minute window, the day's reads, the series), the
// readers and the merge tables, none of which a fleet revision moves: the record is written
// again uncounted only while the count is younger than WhereRestampFor on its UTC day and
// the readers and merge tables are where it was counted.
func TestAWhereRecordWrittenAgainIsRecountedPastItsMinute(t *testing.T) {
	t.Parallel()
	h, st := whereIdle(t)
	at := h.now
	moveFleetRecord(h, st, "count")
	require.NoError(t, st.keepWhere(h.ctx, Machine{}))
	h.setClock(at.Add(30 * time.Second))
	assert.False(t, loadCellKeep(h, st, "11.0%"), "T+30 s: written again")
	h.setClock(at.Add(61 * time.Second))
	assert.True(t, loadCellKeep(h, st, "12.0%"), "T+61 s: past WhereRestampFor, counted")
	h.setClock(at.Add(90 * time.Second))
	assert.False(t, loadCellKeep(h, st, "13.0%"), "T+90 s: 29 s after the count at T+61 s")

	// a property of the readers table moved (no fleet record): counted
	s := h.snap()
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Readers), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(s.Readers.Revision), OperationID: "readers-prop",
		Members: []ntable.BatchMemberEntry{}, Props: map[string]string{"probe_index": "1"}})
	require.NoError(t, err)
	assert.True(t, loadCellKeep(h, st, "14.0%"), "the readers table moved: counted")
	// and the merge table
	s = h.snap()
	_, err = h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Merge), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(s.Merge.Revision), OperationID: "merge-prop",
		Members: []ntable.BatchMemberEntry{}, Props: map[string]string{"probe_index": "1"}})
	require.NoError(t, err)
	assert.True(t, loadCellKeep(h, st, "15.0%"), "the merge table moved: counted")
	assert.False(t, loadCellKeep(h, st, "16.0%"), "then written again")
}

// A count on one UTC day is not written again on the next, however young: the day's reads
// (ReadsToday) are the count's day's.
func TestAWhereRecordIsRecountedOnANewUTCDay(t *testing.T) {
	t.Parallel()
	h, st := whereIdle(t)
	y, mo, d := h.now.UTC().Date()
	eve := time.Date(y, mo, d, 23, 59, 50, 0, time.UTC)
	require.True(t, eve.After(h.now))
	h.setClock(eve)
	moveFleetRecord(h, st, "eve")
	require.NoError(t, st.keepWhere(h.ctx, Machine{}))
	h.setClock(eve.Add(5 * time.Second))
	assert.False(t, loadCellKeep(h, st, "21.0%"), "23:59:55: the same day, written again")
	h.setClock(eve.Add(20 * time.Second))
	assert.True(t, loadCellKeep(h, st, "22.0%"), "00:00:10 the next day: counted")
}

// noChanges is a store with no change stream (no TableChanger): every table that moved is
// read whole, as after a GapError or a GrantError.
type noChanges struct{ Backend }

// twinReadOnce reads the sprint through tw on st with the extras.
func twinReadOnce(t *testing.T, st *Store, tw *Twin, extras func(*sprint.Snapshot) map[string][]string) {
	t.Helper()
	tw.mu.Lock()
	defer tw.mu.Unlock()
	_, _, err := st.twinRead(context.Background(), tw, All, extras, nil)
	require.NoError(t, err)
}

// Each place the twin counts a move is pinned (Twin.moves): a table read whole, a table read
// whole for want of its change stream, and a change of the kept records shown, each the one
// move it counts.
func TestEveryTwinMoveIsCounted(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	holesUp(h, 2)
	st, err := h.st.Pinned(h.ctx)
	require.NoError(t, err)

	t.Run("a table read whole", func(t *testing.T) {
		tw := NewTwin()
		twinReadOnce(t, st, tw, nil)
		assert.EqualValues(t, 1, tw.moves[sprint.Fleet], "the first read: whole")
		tw.drop("the test")
		twinReadOnce(t, st, tw, nil)
		assert.EqualValues(t, 2, tw.moves[sprint.Fleet], "read whole again after a drop")
		twinReadOnce(t, st, tw, nil)
		assert.EqualValues(t, 2, tw.moves[sprint.Fleet], "nothing moved")
	})

	t.Run("a table read whole for want of its change stream", func(t *testing.T) {
		ns := st.clone()
		ns.B = noChanges{st.B}
		tw := NewTwin()
		twinReadOnce(t, &ns, tw, nil)
		m0 := tw.moves[sprint.Fleet]
		require.NoError(t, h.m.RowSet(h.ctx, st.Names.Table(sprint.Fleet), "m2", map[string]string{sprint.Load: "3.0%"}))
		twinReadOnce(t, &ns, tw, nil)
		assert.Equal(t, m0+1, tw.moves[sprint.Fleet], "a text cell, read whole: the whole read is a move")
	})

	t.Run("the kept records shown", func(t *testing.T) {
		// a work card taken off the table, kept: an extra a step names
		s := h.snap()
		c := s.Work.Card("s1-1")
		require.NotNil(t, c)
		_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: fmt.Sprint(st.PinnedEpoch()),
			ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "keep-s1-1",
			Members: []ntable.BatchMemberEntry{{ID: st.sid(c.ID), Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Remove: true}}})
		require.NoError(t, err)
		tw := NewTwin()
		twinReadOnce(t, st, tw, nil)
		m0 := tw.moves[sprint.Work]
		named := func(*sprint.Snapshot) map[string][]string { return map[string][]string{sprint.Work: {"s1-1"}} }
		twinReadOnce(t, st, tw, named)
		assert.Equal(t, m0+1, tw.moves[sprint.Work], "shown")
		twinReadOnce(t, st, tw, named)
		assert.Equal(t, m0+1, tw.moves[sprint.Work], "shown again, the same record: no move")
		twinReadOnce(t, st, tw, nil)
		assert.Equal(t, m0+2, tw.moves[sprint.Work], "no longer shown")
	})
}
