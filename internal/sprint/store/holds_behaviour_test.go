package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestHoldsOnABareStoreReturnsNilAndNoError: Holds (hold.go:121) on a store that
// was never Init'd has no sprint, so it lists nothing and returns no error
// (hold.go:128-129).
func TestHoldsOnABareStoreReturnsNilAndNoError(t *testing.T) {
	t.Parallel()
	st := &Store{B: NewMem(), Names: sprint.Names{Prefix: "t-"}}
	views, err := st.Holds(context.Background())
	require.NoError(t, err)
	assert.Nil(t, views)
}

// TestHoldsListsHeldMemberReaderFriendAndStreamInKindOrder: Holds lists each
// held thing with its reason, members in the fleet table's order, then readers,
// then friends and streams by name, and an unhold empties the list
// (hold.go:117-120).
func TestHoldsListsHeldMemberReaderFriendAndStreamInKindOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s2", Count: 1}))
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)

	names := []string{"m1", "m2", "reader-a", "amy", "s1", "s2"}
	res, err := h.st.Hold(h.ctx, sprint.HoldReq{Names: names, Reason: "r", Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)

	views, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	var got []string
	for _, v := range views {
		got = append(got, v.Kind+":"+v.Name)
		assert.Equal(t, "r", v.Reason, "%s %s has no reason", v.Kind, v.Name)
	}
	require.Equal(t, []string{
		"member:m1", "member:m2", "reader:reader-a", "friend:amy", "stream:s1", "stream:s2",
	}, got)
	byName := map[string]sprint.HoldView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	assert.Equal(t, "coordinator", byName["reader-a"].By, "a reader's hold says who held it")
	assert.Equal(t, "coordinator", byName["amy"].By, "a friend's hold says who held it")

	res, err = h.st.Hold(h.ctx, sprint.HoldReq{Names: names, Release: true, Who: "coordinator"})
	require.NoError(t, err)
	views, err = h.st.Holds(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, views)
}

// TestHoldsReturnFlagShowsWhetherTheHoldHandedBackWork: a member held with
// --return shows Return, and without it the hold's work finishes (HoldView.Return,
// sprint/hold.go:105-107).
func TestHoldsReturnFlagShowsWhetherTheHoldHandedBackWork(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	_, err := h.st.Hold(h.ctx, sprint.HoldReq{Names: []string{"m1"}, Reason: "r", Who: "coordinator", Return: true})
	require.NoError(t, err)
	_, err = h.st.Hold(h.ctx, sprint.HoldReq{Names: []string{"m2"}, Reason: "r", Who: "coordinator"})
	require.NoError(t, err)

	views, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	byName := map[string]sprint.HoldView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	require.Contains(t, byName, "m1")
	require.Contains(t, byName, "m2")
	assert.True(t, byName["m1"].Return, "a hold with --return shows Return")
	assert.False(t, byName["m2"].Return, "a hold without --return shows no Return")
}

// TestHoldsSkipsAnUnreadableReaderRecordAndAFriendsFleetRow: an unreadable
// reader record is no hold, left out with no error (hold.go:182-183), and a
// friend's fleet row is not listed as a member (hold.go:156-158).
func TestHoldsSkipsAnUnreadableReaderRecordAndAFriendsFleetRow(t *testing.T) {
	t.Parallel()
	t.Run("unreadable reader record", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		require.NoError(t, h.m.SetKey(h.ctx, readerAwayKey("reader-a"), "not-json"))
		views, err := h.st.Holds(h.ctx)
		require.NoError(t, err)
		for _, v := range views {
			assert.NotEqual(t, "reader-a", v.Name, "an unreadable reader record is listed as held")
		}
	})
	t.Run("a friend's fleet row", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2, Class: "flash"}})
		require.NoError(t, err)
		h.up("amy")
		brief := "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend amy\n\nThe task."
		h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: brief}}}))
		h.startMachine()
		h.machine()

		_, err = h.st.Hold(h.ctx, sprint.HoldReq{Names: []string{"amy"}, Reason: "r", Who: "coordinator"})
		require.NoError(t, err)
		views, err := h.st.Holds(h.ctx)
		require.NoError(t, err)
		var friendView bool
		for _, v := range views {
			assert.NotEqual(t, "member", v.Kind, "%s %s is listed as a member", v.Kind, v.Name)
			assert.NotContains(t, v.Name, "friend.", "%s %s is a friend's fleet row", v.Kind, v.Name)
			if v.Kind == sprint.HoldFriend && v.Name == "amy" {
				friendView = true
			}
		}
		assert.True(t, friendView, "the held friend is not listed as a friend")
	})
}
