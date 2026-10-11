package store

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// init writes the coordinator key (docs/SPEC-SPRINT.md, "Handing over the
// seat", seat-key-follows-record.w5). Over a held key init does not move the
// seat (the verb refuses it: coordinatorsAlone in cmd/nova-sprint); over a
// key that holds none while a friend-health record survives, init's write
// records the move as a seat change, so an observation made under the earlier
// seat lapses; a fresh store's first init writes no seat record.

// friendRow is the friend's row, by name.
func (h *harness) friendRow(name string) FriendRow {
	h.t.Helper()
	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(h.t, err)
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	require.Fail(h.t, "no row for "+name)
	return FriendRow{}
}

// (a) The coordinator's sequence: init with coordinator A, friend f observed
// up by A, then init --coordinator B over the held key. The verb refuses it
// ("init does not change the coordinator", coordinatorsAlone, pinned in
// cmd/nova-sprint/coordinator_test.go); the store's InitSeat keeps the holder
// and writes no seat record, so the observation stands. This pins the store
// half of the refusal.
func TestInitOverAHeldKeyKeepsTheHolder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.SetCoordinator(h.ctx, "A"))
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "f", Width: 2}})
	require.NoError(t, err)
	_, status, _, err := h.health("f", "A", sprint.Up, h.now, 1)
	require.NoError(t, err)
	assert.Equal(t, sprint.Up, status)

	name, err := h.st.InitSeat(h.ctx, "B")
	require.NoError(t, err)
	assert.Equal(t, "A", name, "init over a held key keeps the holder")
	key, err := h.m.Coordinator(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "A", key, "init over a held key does not move the seat")
	_, ok, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no seat record is written")
	assert.Equal(t, sprint.Up, h.friendStatus("f"), "the observation under A still stands")
}

// (b) The keyless case: A's observation of f survives while the coordinator
// key is gone (no verb path reaches it: teardown removes the key and every
// rostered friend's health together, and a dropped friend's health is removed
// by SyncFriends; so the store's KV removes the key here). init --coordinator
// B records the move as a seat change, so A's observation lapses: f is down,
// its evidence naming no session pong, until B observes her herself.
func TestInitOverAKeylessStoreRecordsTheSeatMove(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.SetCoordinator(h.ctx, "A"))
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "f", Width: 2}})
	require.NoError(t, err)
	_, status, _, err := h.health("f", "A", sprint.Up, h.now, 1)
	require.NoError(t, err)
	assert.Equal(t, sprint.Up, status)

	// the coordinator key is gone: by the store's KV, no verb path reaches it
	require.NoError(t, h.m.SetCoordinator(h.ctx, ""))

	init := *h.st
	init.Actor = "B"
	name, err := init.InitSeat(h.ctx, "B")
	require.NoError(t, err)
	assert.Equal(t, "B", name)

	key, err := h.m.Coordinator(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "B", key)
	rec, ok, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	require.True(t, ok, "the move is recorded")
	assert.Equal(t, "B", rec.Holder)
	assert.Equal(t, uint64(2), rec.Generation, "the generation stepped past A's observation")

	row := h.friendRow("f")
	assert.Equal(t, sprint.Down, row.Status, "A's observation lapses under B")
	assert.Contains(t, row.Evidence, "no wake ping answered by her session")

	// the move is logged with its note, as the seat's own step does
	var seatLines []string
	for _, l := range h.lines() {
		if l.Note != nil && l.Note.Type == sprint.NSeat && strings.Contains(l.Note.What, "init --coordinator B") {
			seatLines = append(seatLines, sprint.Render(l))
		}
	}
	require.Len(t, seatLines, 1, "one seat line records the init move")
	assert.Contains(t, seatLines[0], "- -> B: init --coordinator B, by B")

	// B's own observation makes her up
	h.tick(time.Second)
	_, status, _, err = h.health("f", "B", sprint.Up, h.now, 2)
	require.NoError(t, err)
	assert.Equal(t, sprint.Up, status)
	assert.Equal(t, sprint.Up, h.friendStatus("f"))
}

// A fresh store's first init writes no seat record: the key is its record,
// and the seat's generation is FirstSeatGeneration.
func TestAFreshStoresFirstInitWritesNoSeatRecord(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.SetCoordinator(h.ctx, ""))
	name, err := h.st.InitSeat(h.ctx, "A")
	require.NoError(t, err)
	assert.Equal(t, "A", name)
	_, ok, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a fresh store's first init writes no seat record")
	assert.Equal(t, uint64(sprint.FirstSeatGeneration), h.seat().Generation)
}
