package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// init's write of the coordinator key is the seat's record: the seat never
// moves unrecorded (docs/SPEC-SPRINT.md, "Handing over the seat";
// init-coordinator-moves-the-seat-recorded). The coordinator's own sequence is
// refused by the command (cmd/nova-sprint/coordinator.go coordinatorsAlone:
// "init does not change the coordinator", pinned by
// TestInitOnASprintWithACoordinatorIsTheCoordinators in
// cmd/nova-sprint/coordinator_test.go, unchanged). The store's half of that
// rule is here: while the seat has a record, InitSeat writes the record's
// holder and ignores the name it was given, so init cannot move a recorded
// seat.

// TestInitSeatKeepsTheRecordsHolder pins (a): after the seat has moved to
// stella, an init naming tester writes stella, not tester.
func TestInitSeatKeepsTheRecordsHolder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.give("tester", "stella")

	name, err := h.st.InitSeat(h.ctx, "tester")
	require.NoError(t, err)
	assert.Equal(t, "stella", name, "init keeps the record's holder")
	key, err := h.m.Coordinator(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "stella", key, "the key follows the record")
	_, moved, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	assert.True(t, moved)
}

// TestInitAfterTheSeatKeyIsLostLapsesOldObservations pins (b), red first: A's
// observation of amy is kept, the coordinator key is gone, init --coordinator B
// runs, and within FriendPongWindow of A's observation amy is not up on it.
// The key is emptied by the store's KV: no verb path reaches an empty key
// beside a surviving observation on the twin store (teardown removes the key
// and every rostered friend's health together; a friend dropped before
// teardown has no roster row to reach her by). The report says so.
func TestInitAfterTheSeatKeyIsLostLapsesOldObservations(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)
	_, status, _, err := h.health("amy", "tester", sprint.Up, h.now, 1)
	require.NoError(t, err)
	require.Equal(t, sprint.Up, status, "A's observation makes amy up")
	require.Equal(t, sprint.Up, h.friendStatus("amy"))

	// the coordinator key is gone, the observation outlived its seat
	require.NoError(t, h.m.SetCoordinator(h.ctx, ""))

	name, err := h.st.InitSeat(h.ctx, "stella")
	require.NoError(t, err)
	assert.Equal(t, "stella", name)
	key, err := h.m.Coordinator(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, "stella", key, "init writes the name it was given when no record exists")

	// the move is recorded: a seat record with the generation stepped
	rec, moved, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	assert.True(t, moved, "init recorded the move as a seat change")
	assert.Equal(t, "stella", rec.Holder)
	assert.Equal(t, uint64(sprint.FirstSeatGeneration+1), rec.Generation, "the generation stepped")

	// within FriendPongWindow of A's observation, amy is not up on it
	h.tick(sprint.FriendPongWindow - time.Second)
	assert.Equal(t, sprint.Down, h.friendStatus("amy"), "A's old observation does not count under B")

	// B's own observation of amy makes her up
	_, status, _, err = h.health("amy", "stella", sprint.Up, h.now, sprint.FirstSeatGeneration+1)
	require.NoError(t, err)
	assert.Equal(t, sprint.Up, status, "B's observation makes amy up")
	assert.Equal(t, sprint.Up, h.friendStatus("amy"))
}

// TestFirstInitOnAFreshStoreWritesNoSeatRecord pins that a fresh store's first
// init stays as it is today: no seat record, the key init's alone.
func TestFirstInitOnAFreshStoreWritesNoSeatRecord(t *testing.T) {
	t.Parallel()
	m := NewMem()
	n := 0
	st := &Store{B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "alice",
		Now:   func() time.Time { return t0 },
		NewID: func() string { n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	ctx := context.Background()
	require.NoError(t, st.Init(ctx))
	name, err := st.InitSeat(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", name)
	key, err := m.Coordinator(ctx)
	require.NoError(t, err)
	assert.Equal(t, "alice", key)
	_, moved, err := st.Seat(ctx)
	require.NoError(t, err)
	assert.False(t, moved, "a fresh store's first init writes no seat record")
}
