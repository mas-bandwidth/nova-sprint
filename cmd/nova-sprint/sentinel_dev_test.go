package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// A local bare origin is the fake forge. No network or clock wait is used.
func TestSentinelProtocolFetchesActualMainAndMergesConsumer(t *testing.T) {
	r := newLandRig(t)
	r.m.SetGitIdentity("nova-sprint <nova-sprint@example.invalid>")
	base := r.git(r.worker, "rev-parse", "HEAD")
	r.git(r.worker, "checkout", "-q", "-b", "consumer", base)
	consumerHead := r.commit("consumer.txt", "consumer\n", "consumer work")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/consumer")
	r.git(r.worker, "checkout", "-q", "main")
	producerHead := r.commit("producer.txt", "producer\n", "producer work")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	st, err := r.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	r.a.bindSentinelProtocol(st)
	s := &sprint.Snapshot{Epoch: 3, Work: sprint.NewTable(sprint.Work), Merge: sprint.NewTable(sprint.Merge)}
	s.Work.SetRows([]string{"producer", "consumer"})
	for _, stream := range []string{"producer", "consumer"} {
		s.Merge.Put(&sprint.Card{ID: sprint.CtlID(stream), Row: stream, Col: sprint.Ctl, Fields: map[string]string{sprint.FieldTrunk: "main", sprint.FieldStreamBranch: "sprint/" + stream}})
	}
	brief := fmt.Sprintf("fixture\nREPO: %s\n", r.remote)
	s.Work.Put(&sprint.Card{ID: "p", Row: "producer", Col: sprint.Landed, Score: 1, Fields: map[string]string{"head": producerHead, "brief": brief}})
	stop := &sprint.Card{ID: "stop", Row: "producer", Col: sprint.Waiting, Score: 2, Fields: map[string]string{"kind": "sentinel", "needs": "p"}}
	s.Work.Put(stop)
	observations := st.SentinelProof(context.Background(), s)
	_, proof, ok := sprint.SentinelDevReason(s, stop, observations)
	require.True(t, ok)
	require.Equal(t, producerHead, proof[0].Containing)
	stop.Col = sprint.Landed
	c := &sprint.Card{ID: "consumer", Row: "consumer", Col: sprint.Ready, Fields: map[string]string{"needs": "stop", "brief": brief}}
	s.Work.Put(c)
	s.ConsumerMerges = st.ConsumerTrunk(context.Background(), s)
	require.True(t, sprint.ConsumerTrunkReady(s, c))
	merged := r.git(r.remote, "rev-parse", "refs/heads/sprint/consumer")
	require.NotEqual(t, consumerHead, merged)
	r.git(r.remote, "merge-base", "--is-ancestor", producerHead, merged)
	r.git(r.remote, "merge-base", "--is-ancestor", consumerHead, merged)
	// A newly observed trunk advance replaces the previous receipt and is merged.
	next := r.commit("next.txt", "next\n", "next trunk")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	s.ConsumerMerges = st.ConsumerTrunk(context.Background(), s)
	require.True(t, sprint.ConsumerTrunkReady(s, c))
	require.Equal(t, next, s.ConsumerMerges[0].Observed)
	r.git(r.worker, "fetch", "-q", "origin")
	r.git(r.worker, "checkout", "-q", "-B", "consumer", "refs/remotes/origin/sprint/consumer")
	conflictedConsumer := r.commit("README", "consumer conflict\n", "consumer conflict")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/consumer")
	r.git(r.worker, "checkout", "-q", "main")
	r.commit("README", "trunk conflict\n", "trunk conflict")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	s.ConsumerMerges = st.ConsumerTrunk(context.Background(), s)
	require.False(t, sprint.ConsumerTrunkReady(s, c), "a conflicted actual merge must block the next deal")
	require.Equal(t, conflictedConsumer, r.git(r.remote, "rev-parse", "refs/heads/sprint/consumer"))

}

func sentinelProgressNotes(t *testing.T, ta *testApp, stream string) []sprint.Note {
	t.Helper()
	notes, _, err := ta.m.NotesSince(context.Background(), "", 100000)
	require.NoError(t, err)
	var found []sprint.Note
	for _, n := range notes {
		if n.Type == sprint.NSentinelReached && n.Stream == stream {
			require.Equal(t, sprint.Happened, n.Kind)
			require.Empty(t, n.Decisions)
			found = append(found, n)
		}
	}
	return found
}

func TestConsumerCallbackUsesOneObservedTrunkAcrossStreams(t *testing.T) {
	r := newLandRig(t)
	r.m.SetGitIdentity("nova-sprint <nova-sprint@example.invalid>")
	base := r.git(r.worker, "rev-parse", "HEAD")
	for _, stream := range []string{"one", "two"} {
		r.git(r.worker, "push", "-q", "origin", base+":refs/heads/sprint/"+stream)
	}
	initial := r.commit("producer.txt", "producer\n", "producer trunk")
	r.git(r.worker, "push", "-q", "origin", initial+":refs/heads/main")
	next := r.commit("next.txt", "next\n", "next trunk")
	r.git(r.worker, "push", "-q", "origin", next+":refs/heads/fixture-next")
	// The fake forge advances after the first consumer push. The callback
	// observes once; a later callback must observe and merge this advance.
	require.NoError(t, os.WriteFile(filepath.Join(r.remote, "hooks", "post-receive"), []byte("#!/bin/sh\ngit update-ref refs/heads/main "+next+"\n"), 0700))
	st, err := r.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	r.a.bindSentinelProtocol(st)
	s := &sprint.Snapshot{Work: sprint.NewTable(sprint.Work), Merge: sprint.NewTable(sprint.Merge)}
	s.Work.SetRows([]string{"producer", "one", "two"})
	s.Work.Put(&sprint.Card{ID: "stop", Row: "producer", Col: sprint.Landed, Fields: map[string]string{"kind": "sentinel", "release_kind": "automatic"}})
	for _, stream := range []string{"one", "two"} {
		s.Merge.Put(&sprint.Card{ID: sprint.CtlID(stream), Row: stream, Col: sprint.Ctl, Fields: map[string]string{sprint.FieldTrunk: "main", sprint.FieldStreamBranch: "sprint/" + stream}})
		s.Work.Put(&sprint.Card{ID: stream, Row: stream, Col: sprint.Ready, Fields: map[string]string{"needs": "stop", "brief": fmt.Sprintf("fixture\nREPO: %s\n", r.remote)}})
	}
	proofs := st.ConsumerTrunk(context.Background(), s)
	require.Len(t, proofs, 2)
	for _, proof := range proofs {
		require.True(t, proof.Success)
		require.Equal(t, initial, proof.Observed)
	}
	require.Equal(t, next, r.git(r.remote, "rev-parse", "refs/heads/main"))
	proofs = st.ConsumerTrunk(context.Background(), s)
	require.Len(t, proofs, 2)
	for _, proof := range proofs {
		require.True(t, proof.Success)
		require.Equal(t, next, proof.Observed)
	}
}
