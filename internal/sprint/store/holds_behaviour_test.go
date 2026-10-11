package store

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHoldsOnAStoreWithNoSprintIsEmpty: a bare NewMem with no Init holds nothing,
// nil and no error (Store.Holds, hold.go:117).
func TestHoldsOnAStoreWithNoSprintIsEmpty(t *testing.T) {
	t.Parallel()
	st := &Store{B: NewMem(), Names: sprint.Names{Prefix: "t-"}}
	hs, err := st.Holds(context.Background())
	require.NoError(t, err)
	assert.Nil(t, hs)
}

// TestHoldsListsMembersReadersFriendsAndStreamsInKindOrder: a held member,
// reader, friend and stream are four views in that kind order, each with its
// reason and who held it; members come in fleet-row order and streams sorted by
// name; after unhold the list is empty (Store.Holds, hold.go:117).
func TestHoldsListsMembersReadersFriendsAndStreamsInKindOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2, Class: "pro"}})
	require.NoError(t, err)
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 1}))

	names := []string{"m2", "m1", "reader-a", "amy", "s2", "s1"}
	res, err := h.st.Hold(h.ctx, sprint.HoldReq{Names: names, Reason: "the trim", Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused, "hold refused: %v", res.Refused)

	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	var got []string
	for _, v := range hs {
		got = append(got, v.Kind+" "+v.Name)
		assert.Equal(t, "the trim", v.Reason, "%s", v.Name)
	}
	assert.Equal(t, []string{
		"member m1", "member m2",
		"reader reader-a",
		"friend amy",
		"stream s1", "stream s2",
	}, got)
	for _, v := range hs {
		switch v.Kind {
		case sprint.HoldReader, sprint.HoldFriend:
			assert.Equal(t, "coordinator", v.By, "%s is signed by who held it", v.Name)
		}
	}

	res, err = h.st.Hold(h.ctx, sprint.HoldReq{Names: names, Release: true, Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused, "unhold refused: %v", res.Refused)
	hs, err = h.st.Holds(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, hs, "after unhold the list is empty")
}

// TestHoldsShowsAMembersReturnAsTheHold: a member hold made with Return true
// shows Return true (HoldView.Return says the hold handed the work begun back),
// and without it false (hold.go:160).
func TestHoldsShowsAMembersReturnAsTheHold(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)

	res, err := h.st.Hold(h.ctx, sprint.HoldReq{Names: []string{"m1"}, Reason: "drain it", Return: true, Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	res, err = h.st.Hold(h.ctx, sprint.HoldReq{Names: []string{"m2"}, Reason: "finish it", Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)

	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	byName := map[string]sprint.HoldView{}
	for _, v := range hs {
		byName[v.Name] = v
	}
	require.Contains(t, byName, "m1")
	require.Contains(t, byName, "m2")
	assert.True(t, byName["m1"].Return, "a returned member hold shows Return true (HoldView.Return is --return)")
	assert.False(t, byName["m2"].Return, "a finishing member hold shows Return false")
}

// TestHoldsSkipsAnUnreadableReaderAndAFriendRowOnTheFleetTable: an unreadable
// reader record is left out with no error, and a friend's row on the fleet table
// is not listed as a member (Store.Holds, hold.go:156, hold.go:182).
func TestHoldsSkipsAnUnreadableReaderAndAFriendRowOnTheFleetTable(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2, Class: "pro"}})
	require.NoError(t, err)
	res, err := h.st.Hold(h.ctx, sprint.HoldReq{Names: []string{"reader-a", "amy"}, Reason: "the trim", Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused, "hold refused: %v", res.Refused)

	kv, err := h.st.rootKV()
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(h.ctx, readerAwayKey("reader-c"), "{not json"))

	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	seen := map[string][]string{}
	for _, v := range hs {
		seen[v.Name] = append(seen[v.Name], v.Kind)
	}
	assert.NotContains(t, seen, "reader-c", "an unreadable reader record is left out")
	assert.Equal(t, []string{sprint.HoldReader}, seen["reader-a"])
	assert.Equal(t, []string{sprint.HoldFriend}, seen["amy"], "a friend's row is not listed as a member")
}
