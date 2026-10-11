package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// hold must apply: the store's hold of names with no refusal.
func (h *harness) hold(req sprint.HoldReq) {
	h.t.Helper()
	res, err := h.st.Hold(h.ctx, req)
	require.NoError(h.t, err, "hold %v", req.Names)
	require.Empty(h.t, res.Refused, "hold %v refused", req.Names)
}

// holdNamed is the hold in force on name, its kind; false for none.
func holdNamed(hs []sprint.HoldView, name string) (sprint.HoldView, bool) {
	for _, hv := range hs {
		if hv.Name == name {
			return hv, true
		}
	}
	return sprint.HoldView{}, false
}

// A store with no sprint (a bare NewMem, no Init) holds nothing: nil, no error
// (hold.go:128-130).
func TestHoldsOnABareStoreIsNilAndNoError(t *testing.T) {
	t.Parallel()
	st := &Store{B: NewMem(), Names: sprint.Names{Prefix: "t-"}}
	hs, err := st.Holds(context.Background())
	require.NoError(t, err)
	assert.Nil(t, hs)
}

// Holds is every hold in force, members in fleet-row order, then readers in
// theirs, then friends and streams by name (hold.go:117-120): a held member,
// reader, friend and streams come back in that kind order, each with its
// reason and who held it; after unhold the list is empty.
func TestHoldsListsMembersReadersFriendsAndStreamsInKindOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 1}))
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 3}})
	require.NoError(t, err)
	h.up("amy")

	for _, req := range []sprint.HoldReq{
		{Names: []string{"m1"}, Reason: "rm1", Who: "boss"},
		{Names: []string{"m2"}, Reason: "rm2", Who: "boss"},
		{Names: []string{"reader-a"}, Reason: "rr", Who: "boss"},
		{Names: []string{"amy"}, Reason: "rf", Who: "boss"},
		{Names: []string{"s1"}, Reason: "rs1", Who: "boss"},
		{Names: []string{"s2"}, Reason: "rs2", Who: "boss"},
	} {
		h.hold(req)
	}

	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	want := []sprint.HoldView{
		{Kind: sprint.HoldMember, Name: "m1", Reason: "rm1"},
		{Kind: sprint.HoldMember, Name: "m2", Reason: "rm2"},
		{Kind: sprint.HoldReader, Name: "reader-a", Reason: "rr", By: "boss"},
		{Kind: sprint.HoldFriend, Name: "amy", Reason: "rf", By: "boss"},
		{Kind: sprint.HoldStream, Name: "s1", Reason: "rs1"},
		{Kind: sprint.HoldStream, Name: "s2", Reason: "rs2"},
	}
	require.Len(t, hs, len(want))
	for i, w := range want {
		assert.Equal(t, w.Kind, hs[i].Kind, "view %d kind", i)
		assert.Equal(t, w.Name, hs[i].Name, "view %d name", i)
		assert.Equal(t, w.Reason, hs[i].Reason, "view %d reason", i)
		assert.Equal(t, w.By, hs[i].By, "view %d who held it", i)
	}

	h.hold(sprint.HoldReq{Names: []string{"m1", "m2", "reader-a", "amy", "s1", "s2"}, Release: true, Who: "boss"})
	hs, err = h.st.Holds(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, hs, "after unhold the list is empty")
}

// A member hold made with --return hands its working cards back now, so its
// view's Return is set; without it the work finishes and Return is not
// (sprint.HoldView.Return; hold.go:160).
func TestAMemberHoldWithReturnShowsReturnAndWithoutItDoesNot(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)

	h.hold(sprint.HoldReq{Names: []string{"m1"}, Reason: "r1", Who: "boss", Return: true})
	h.hold(sprint.HoldReq{Names: []string{"m2"}, Reason: "r2", Who: "boss"})

	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	m1, ok := holdNamed(hs, "m1")
	require.True(t, ok, "m1 is held")
	m2, ok := holdNamed(hs, "m2")
	require.True(t, ok, "m2 is held")
	assert.True(t, m1.Return, "a hold with --return shows Return")
	assert.False(t, m2.Return, "a hold without --return shows no Return")
}

// An unreadable reader record is no hold, and no error (hold.go:182-183); a
// friend's row on the fleet table is not listed as a member (hold.go:156).
func TestHoldsLeavesOutAnUnreadableReaderRecordAndAFriendsFleetRow(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 3}})
	require.NoError(t, err)
	h.up("amy")

	require.NoError(t, h.m.SetKey(h.ctx, readerAwayKey("reader-a"), "not valid json"))
	h.hold(sprint.HoldReq{Names: []string{"amy"}, Reason: "rf", Who: "boss"})

	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	_, ok := holdNamed(hs, "reader-a")
	assert.False(t, ok, "an unreadable reader record is left out")
	_, ok = holdNamed(hs, "friend.amy")
	assert.False(t, ok, "a friend's fleet row is not listed as a member")
	amy, ok := holdNamed(hs, "amy")
	require.True(t, ok, "the friend is held by name")
	assert.Equal(t, sprint.HoldFriend, amy.Kind)
}
