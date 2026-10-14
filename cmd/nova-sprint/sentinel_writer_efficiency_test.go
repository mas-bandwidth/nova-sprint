package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// The wrapper records actual commands to a local bare fixture, not a forge
// success stub. These sequential tests keep PATH private via t.Setenv.
func recordSentinelGit(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	require.NoError(t, err)
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + quote(log) + "\nexec " + quote(git) + " \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestSentinelIncompleteProducerSetDoesNotFetchOrLock(t *testing.T) {
	r := newLandRig(t)
	head := r.git(r.worker, "rev-parse", "HEAD")
	st, err := r.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	r.a.bindSentinelProtocol(st)
	s := &sprint.Snapshot{Work: sprint.NewTable(sprint.Work), Merge: sprint.NewTable(sprint.Merge)}
	s.Work.SetRows([]string{"s1"})
	brief := fmt.Sprintf("fixture\nREPO: %s\n", r.remote)
	s.Work.Put(&sprint.Card{ID: "landed", Row: "s1", Col: sprint.Landed, Score: 1, Fields: map[string]string{"head": head, "brief": brief}})
	s.Work.Put(&sprint.Card{ID: "pending", Row: "s1", Col: sprint.Working, Score: 2})
	s.Work.Put(&sprint.Card{ID: "stop", Row: "s1", Col: sprint.Waiting, Score: 3, Fields: map[string]string{"kind": "sentinel", "needs": "landed,pending"}})
	log := recordSentinelGit(t)
	st.SentinelProof(context.Background(), s)
	_, err = os.Stat(log)
	require.True(t, os.IsNotExist(err), "an incomplete producer set must do no Git work")
}

func TestConsumerAlreadyMergedFreshTrunkDoesNotTakeTheLandLockOrPush(t *testing.T) {
	r := newLandRig(t)
	r.m.SetGitIdentity("nova-sprint <nova-sprint@example.invalid>")
	head := r.git(r.worker, "rev-parse", "HEAD")
	r.git(r.worker, "push", "-q", "origin", head+":refs/heads/sprint/consumer")
	st, err := r.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	r.a.bindSentinelProtocol(st)
	s := &sprint.Snapshot{Work: sprint.NewTable(sprint.Work), Merge: sprint.NewTable(sprint.Merge)}
	s.Work.SetRows([]string{"producer", "consumer"})
	s.Merge.Put(&sprint.Card{ID: sprint.CtlID("consumer"), Row: "consumer", Col: sprint.Ctl, Fields: map[string]string{sprint.FieldTrunk: "main", sprint.FieldStreamBranch: "sprint/consumer"}})
	s.Work.Put(&sprint.Card{ID: "stop", Row: "producer", Col: sprint.Landed, Fields: map[string]string{"kind": "sentinel"}})
	c := &sprint.Card{ID: "consumer", Row: "consumer", Col: sprint.Ready, Fields: map[string]string{"needs": "stop", "brief": fmt.Sprintf("fixture\nREPO: %s\n", r.remote)}}
	s.Work.Put(c)
	other := &lander{a: r.a, st: st}
	defer other.release()
	dir, why := other.clone(context.Background(), r.remote)
	require.Empty(t, why)
	require.Empty(t, other.hold(context.Background(), dir), "another real land holds the checkout lock")
	log := recordSentinelGit(t)
	for i := 0; i < 2; i++ {
		s.ConsumerMerges = st.ConsumerTrunk(context.Background(), s)
		require.True(t, sprint.ConsumerTrunkReady(s, c), "fresh containing branch qualifies even while another land holds its checkout")
		require.Equal(t, head, s.ConsumerMerges[0].Observed)
		require.Equal(t, head, s.ConsumerMerges[0].Merged)
	}
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	fetches := 0
	for _, line := range strings.Split(string(data), "\n") {
		for _, word := range strings.Fields(line) {
			require.NotContains(t, []string{"merge", "push", "checkout", "worktree"}, word, "a containing fresh branch needs no mutation: %s", line)
			if word == "fetch" {
				fetches++
			}
		}
	}
	require.Equal(t, 4, fetches, "both remote branch and trunk are freshly fetched on every callback")
	c.Col = sprint.Waiting
	c.Fields["needs"] = "stop,pending"
	s.Work.Put(c)
	s.Work.Put(&sprint.Card{ID: "pending", Row: "consumer", Col: sprint.Working, Score: 1})
	require.Empty(t, st.ConsumerTrunk(context.Background(), s), "an unmet dependency does not acquire merge evidence")
	after, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, string(data), string(after), "a waiting dependency performs no Git work")
}
