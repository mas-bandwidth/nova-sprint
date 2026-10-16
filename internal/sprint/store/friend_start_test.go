package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// start is the friend's start receipt for the oldest n cards ready on her row, as friend sync
// reads her jobs begun (FriendStartStep): each goes to working, its deadline from now.
func (h *harness) start(friend string, n int) {
	h.t.Helper()
	ready := h.snap().Fleet.Cell(sprint.FriendRow(friend), sprint.Ready)
	sprint.SortCards(ready)
	r := sprint.FriendStartReq{Friend: friend, Gens: map[string]int{}}
	for _, c := range ready[:min(n, len(ready))] {
		r.IDs, r.Gens[c.ID] = append(r.IDs, c.ID), max(c.Int("gen"), 1)
	}
	if len(r.IDs) > 0 {
		h.must(FriendStartStep(r))
	}
}

// On the twin store a friend's card is dealt ready, friend sync's start receipt moves it to
// working with its deadline from then, and her own take is a start too: the next tick keeps
// it working (docs/SPEC-SPRINT.md section 1, a friend's card is
// working once she starts it).
func TestTwinStoreAFriendCardIsWorkingOnlyOnceSheStartsIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2, Class: "flash"}})
	require.NoError(t, err)
	h.up("amy")
	brief := "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend amy\n\nThe task."
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: brief}, {ID: "s1-2", Brief: brief}, {ID: "s1-3", Brief: brief}}}))
	h.startMachine()
	h.machine()
	amy := sprint.FriendRow("amy")
	snap := h.snap()
	assert.Equal(t, 3, snap.Fleet.Count(amy, sprint.Ready), "dealt ready, whatever her lanes")
	assert.Zero(t, snap.Fleet.Count(amy, sprint.Working))

	h.start("amy", 1)
	wc := h.snap().Fleet.Card("s1-1.w1")
	require.Equal(t, sprint.Working, wc.Col)
	assert.Equal(t, h.now.UTC().Format(time.RFC3339), wc.F("taken"), "its deadline runs from her start")
	assert.Equal(t, "1", wc.F(sprint.FieldStarted))

	// her own take is her start (take --as friend.amy): the next tick keeps it working, so a
	// card she took stays taken until its lease or its deadline ends it (the fault of
	// 2026-10-14 20:30:21 EDT: friend.emma's take moved working -> ready by the same tick)
	take := TakeStep(sprint.TakeReq{Sel: sprint.Sel{IDs: []string{"s1-2.w1"}}, As: amy, Gens: map[string]int{"s1-2.w1": 1}, Who: amy})
	h.must(take)
	require.Equal(t, sprint.Working, h.snap().Fleet.Card("s1-2.w1").Col)
	h.machine()
	snap = h.snap()
	assert.Equal(t, sprint.Working, snap.Fleet.Card("s1-2.w1").Col, "her take is her start")
	assert.Equal(t, "1", snap.Fleet.Card("s1-2.w1").F(sprint.FieldStarted))
	assert.Equal(t, sprint.Working, snap.Fleet.Card("s1-1.w1").Col)
	assert.Empty(t, sprint.Check(snap, nil))
}

// Take, then tick, still working: a card a friend takes by count (take --as friend.<name>,
// no lane, no ids) stays working across ticks while its deadline runs; the tick never puts a
// card she took back ready (tla/FriendLaneClient.tla TakenStaysTaken in nova-tools).
func TestAFriendsTakeThenATickIsStillWorking(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "emma", Width: 2, Class: "flash"}})
	require.NoError(t, err)
	h.up("emma")
	brief := "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend emma\n\nThe task."
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: brief}}}))
	h.startMachine()
	h.machine()
	emma := sprint.FriendRow("emma")
	require.Equal(t, sprint.Ready, h.snap().Fleet.Card("s1-1.w1").Col)

	h.must(TakeStep(sprint.TakeReq{Sel: sprint.Sel{Limit: 1}, As: emma, Who: emma}))
	require.Equal(t, sprint.Working, h.snap().Fleet.Card("s1-1.w1").Col, "took attempt 1")
	for range 3 {
		h.machine()
		h.tick(time.Minute)
	}
	wc := h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Working, wc.Col, "a taken card stays taken until its lease or deadline ends it")
	assert.Equal(t, emma, wc.Row)
	assert.Equal(t, "1", wc.F(sprint.FieldStarted))
}
