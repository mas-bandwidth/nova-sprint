package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// startFriend is the friend's start receipt for the oldest n cards ready on her row, as
// friend sync reads her jobs begun (store.FriendStartStep): each goes to working, its
// deadline from now (docs/SPEC-SPRINT.md section 1, a friend's card is working once she
// starts it).
func (ta *testApp) startFriend(friend string, n int) {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	ready, err := st.ReadCells(context.Background(), sprint.Fleet, sprint.FriendRow(friend), sprint.Ready)
	require.NoError(ta.t, err)
	sprint.SortCards(ready)
	r := sprint.FriendStartReq{Friend: friend, Gens: map[string]int{}}
	for _, c := range ready[:min(n, len(ready))] {
		r.IDs, r.Gens[c.ID] = append(r.IDs, c.ID), max(c.Int("gen"), 1)
	}
	if len(r.IDs) == 0 {
		return
	}
	res, err := st.Run(context.Background(), store.FriendStartStep(r))
	require.NoError(ta.t, err)
	require.Empty(ta.t, res.Refused)
}

// beginJob is the friend beginning job <job> in her working directory under root, as her
// daemon stages it and she then writes in it: its JOB.md an hour old, and a file of hers now.
func beginJob(t *testing.T, root, friend, job string) {
	t.Helper()
	dir := filepath.Join(root, friend+"-working", "jobs", job)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	jobFile := filepath.Join(dir, "JOB.md")
	require.NoError(t, os.WriteFile(jobFile, []byte("# JOB\n"), 0o644))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(jobFile, old, old))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("begun\n"), 0o644))
}
