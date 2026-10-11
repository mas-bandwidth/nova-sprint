package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// holdNames holds or unholds names through the store's own verb pair
// (store/hold.go:32, docs/SPEC-SPRINT.md section 11) and fails the test on a
// refusal.
func holdNames(t *testing.T, h *harness, req sprint.HoldReq) {
	t.Helper()
	if req.Who == "" {
		req.Who = "coordinator"
	}
	res, err := h.st.Hold(h.ctx, req)
	require.NoError(t, err, "hold %v", req.Names)
	require.Empty(t, res.Refused, "hold %v refused: %v", req.Names, res.Refused)
}

// viewOf is the hold view of name, and fails when there is none.
func viewOf(t *testing.T, hs []sprint.HoldView, name string) sprint.HoldView {
	t.Helper()
	for _, v := range hs {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("no hold view for %s: %+v", name, hs)
	return sprint.HoldView{}
}

// Holds is every hold in force, for where --json and handover (hold.go:117,
// docs/SPEC-SPRINT.md section 11).

// A store with no sprint (a bare NewMem, no Init): nil and no error.
func TestHoldsOfAStoreWithNoSprintIsNil(t *testing.T) {
	st := &Store{B: NewMem(), Names: sprint.Names{Prefix: "t-"}}
	hs, err := st.Holds(t.Context())
	require.NoError(t, err)
	require.Nil(t, hs, "a store with no sprint shows nothing held: %+v", hs)
}

// A held member, reader, friend and stream: four views in that kind order,
// each with its reason and who held it; two members come back in fleet-row
// order and two streams sorted by name; after unhold the list is empty.
func TestHoldsListsMembersReadersFriendsAndStreamsInOrder(t *testing.T) {
	h := newHarness(t)
	h.setup(4)
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 2}))
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "zoe", Width: 1}})
	require.NoError(t, err)
	names := []string{"m1", "m2", "reader-a", "zoe", "s2", "s1"}
	holdNames(t, h, sprint.HoldReq{Names: names, Reason: "maintenance", Who: "coordinator"})
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	var got []string
	for _, v := range hs {
		got = append(got, v.Kind+" "+v.Name)
	}
	require.Equal(t, []string{
		"member m1", "member m2", "reader reader-a", "friend zoe", "stream s1", "stream s2",
	}, got)
	for _, v := range hs {
		require.Equal(t, "maintenance", v.Reason, "%s %s: its reason", v.Kind, v.Name)
	}
	require.Equal(t, "coordinator", viewOf(t, hs, "reader-a").By, "the reader's hold names who held it")
	require.Equal(t, "coordinator", viewOf(t, hs, "zoe").By, "the friend's hold names who held it")
	holdNames(t, h, sprint.HoldReq{Names: names, Release: true, Who: "coordinator"})
	hs, err = h.st.Holds(h.ctx)
	require.NoError(t, err)
	require.Empty(t, hs, "unhold releases every kind: %+v", hs)
}

// A member hold made with Return true shows Return true; without it, false
// (sprint.HoldView.Return, hold.go:105).
func TestHoldsShowsTheReturnOfAMemberHold(t *testing.T) {
	h := newHarness(t)
	h.setup(2)
	holdNames(t, h, sprint.HoldReq{Names: []string{"m1"}, Reason: "hand it back", Return: true})
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	require.True(t, viewOf(t, hs, "m1").Return, "a hold with --return shows Return true")
	holdNames(t, h, sprint.HoldReq{Names: []string{"m1"}, Release: true})
	holdNames(t, h, sprint.HoldReq{Names: []string{"m1"}, Reason: "let it finish"})
	hs, err = h.st.Holds(h.ctx)
	require.NoError(t, err)
	require.False(t, viewOf(t, hs, "m1").Return, "a hold with no --return shows Return false")
}

// An unreadable reader record is left out with no error, and a friend's row on
// the fleet table is not listed as a member (hold.go:117-118 and hold.go:156).
func TestHoldsLeavesOutAnUnreadableReaderAndAFriendRow(t *testing.T) {
	h := newHarness(t)
	h.setup(2)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "zoe", Width: 1}})
	require.NoError(t, err)
	// her fleet row is there, as where draws it; it is a friend, never a member
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{sprint.FriendRow("zoe")}))
	holdNames(t, h, sprint.HoldReq{Names: []string{"zoe"}, Reason: "out of credits"})
	kv, err := h.st.rootKV()
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(h.ctx, readerAwayKey("reader-a"), "not json"))
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	require.Len(t, hs, 1, "an unreadable reader record is no hold, and a friend's row is no member: %+v", hs)
	require.Equal(t, sprint.HoldFriend, hs[0].Kind)
	require.Equal(t, "zoe", hs[0].Name)
}
