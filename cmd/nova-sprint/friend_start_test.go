package main

import (
	"context"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// startFriend is the friend's own take of the oldest n cards ready on her row (take --as
// friend.<name> by id): each goes to working, its deadline from now (docs/SPEC-SPRINT.md section 1, a friend's card is working once she
// starts it).
func (ta *testApp) startFriend(friend string, n int) {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	ready, err := st.ReadCells(context.Background(), sprint.Fleet, sprint.FriendRow(friend), sprint.Ready)
	require.NoError(ta.t, err)
	sprint.SortCards(ready)
	row := sprint.FriendRow(friend)
	r := sprint.TakeReq{As: row, Gens: map[string]int{}, Who: row, LaneByID: true}
	for _, c := range ready[:min(n, len(ready))] {
		r.IDs, r.Gens[c.ID] = append(r.IDs, c.ID), max(c.Int("gen"), 1)
	}
	if len(r.IDs) == 0 {
		return
	}
	res, err := st.Run(context.Background(), store.TakeStep(r))
	require.NoError(ta.t, err)
	require.Empty(ta.t, res.Refused)
}
