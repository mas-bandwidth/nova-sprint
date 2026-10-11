package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdAll holds the names with the given reason as the harness's actor and must
// apply in whole (Store.Hold, docs/SPEC-SPRINT.md section 11).
func holdAll(t *testing.T, h *harness, r sprint.HoldReq) {
	t.Helper()
	r.Who = h.st.Actor
	res, err := h.st.Hold(h.ctx, r)
	require.NoError(t, err, "hold %v", r.Names)
	require.Empty(t, res.Refused, "hold %v refused: %v", r.Names, res.Refused)
}

// views lists Holds as "kind name" in order, with the reason.
func views(t *testing.T, h *harness) []string {
	t.Helper()
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	var out []string
	for _, hv := range hs {
		out = append(out, hv.Kind+" "+hv.Name+": "+hv.Reason)
	}
	return out
}

// view is the one hold in force on name.
func view(t *testing.T, h *harness, name string) (sprint.HoldView, bool) {
	t.Helper()
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	for _, hv := range hs {
		if hv.Name == name {
			return hv, true
		}
	}
	return sprint.HoldView{}, false
}

// TestHoldsOnAStoreWithNoSprintIsNilAndNoError: a bare Mem with no sprint has
// nothing held and no error (Holds' doc: a store that keeps no records shows
// the tables' holds only).
func TestHoldsOnAStoreWithNoSprintIsNilAndNoError(t *testing.T) {
	t.Parallel()
	st := &Store{B: NewMem(), Names: sprint.Names{Prefix: "t-"}, Actor: "tester"}
	hs, err := st.Holds(t.Context())
	require.NoError(t, err)
	assert.Nil(t, hs)
}

// TestHoldsListsMemberReaderFriendAndStreamInOrder: a held member, reader,
// friend and stream come back in that kind order, each with its reason; two
// members are in fleet-row order and two streams sorted by name; unhold leaves
// none (Holds' doc, hold.go:117).
func TestHoldsListsMemberReaderFriendAndStreamInOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}, {Name: "bob", Width: 2}})
	require.NoError(t, err)
	h.up("amy")
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s2", Count: 1}))

	holdAll(t, h, sprint.HoldReq{Names: []string{"m1", "m2", "reader-a", "amy", "s1", "s2"}, Reason: "the trim"})
	assert.Equal(t, []string{
		"member m1: the trim",
		"member m2: the trim",
		"reader reader-a: the trim",
		"friend amy: the trim",
		"stream s1: the trim",
		"stream s2: the trim",
	}, views(t, h), "kind order: members, readers, friends, streams")

	ra, ok := view(t, h, "reader-a")
	require.True(t, ok)
	assert.Equal(t, h.st.Actor, ra.By, "the reader's hold names who")
	am, ok := view(t, h, "amy")
	require.True(t, ok)
	assert.Equal(t, h.st.Actor, am.By, "the friend's hold names who")

	holdAll(t, h, sprint.HoldReq{Names: []string{"m1", "m2", "reader-a", "amy", "s1", "s2"}, Release: true, Reason: "trimmed"})
	assert.Empty(t, views(t, h), "unhold releases all")
}

// TestHoldsMemberReturnFollowsTheHoldFlag: a member's HoldView.Return is true
// when the hold handed the work back (HoldReq.Return) and false when the work
// finishes (HoldView.Return's doc).
func TestHoldsMemberReturnFollowsTheHoldFlag(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	holdAll(t, h, sprint.HoldReq{Names: []string{"m1"}, Reason: "finish it"})
	holdAll(t, h, sprint.HoldReq{Names: []string{"m2"}, Reason: "hand it back", Return: true})
	finishes, ok := view(t, h, "m1")
	require.True(t, ok)
	assert.False(t, finishes.Return, "a hold with no --return lets the work finish")
	hands, ok := view(t, h, "m2")
	require.True(t, ok)
	assert.True(t, hands.Return, "a hold with --return hands the work back")
}

// TestHoldsLeavesOutAnUnreadableReaderRecord: a reader whose record cannot be
// read is no hold and Holds does not error (Holds' doc; hold.go:182).
func TestHoldsLeavesOutAnUnreadableReaderRecord(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	require.NoError(t, h.m.SetKey(h.ctx, readerAwayKey("reader-b"), "not json"))
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	for _, hv := range hs {
		assert.NotEqual(t, "reader-b", hv.Name, "an unreadable reader record is no hold")
	}
}

// TestHoldsDoesNotListAFriendsFleetRowAsAMember: a friend's row on the fleet
// table is a friend's hold, never a member's (Holds' doc; hold.go:156).
func TestHoldsDoesNotListAFriendsFleetRowAsAMember(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)
	h.up("amy")
	holdAll(t, h, sprint.HoldReq{Names: []string{"amy"}, Reason: "out of credits"})
	hv, ok := view(t, h, "amy")
	require.True(t, ok)
	assert.Equal(t, sprint.HoldFriend, hv.Kind)
	hs, err := h.st.Holds(h.ctx)
	require.NoError(t, err)
	for _, got := range hs {
		if got.Kind == sprint.HoldMember {
			assert.False(t, sprint.IsFriendRow(got.Name), "a friend's row is not a member: %v", got)
		}
	}
}
