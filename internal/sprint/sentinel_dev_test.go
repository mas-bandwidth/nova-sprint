package sprint

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func sentinelProofFixture(t *testing.T) (*Snapshot, *Card, SentinelObservation) {
	t.Helper()
	s := newWorld(t).s
	s.Epoch = 7
	s.Work.SetRows([]string{"producer", "consumer"})
	p := &Card{ID: "p", Row: "producer", Col: Landed, Score: 1, Rev: 2, Fields: map[string]string{"head": "abc", "brief": "p\nREPO: /fixture/repo\n"}}
	c := &Card{ID: "stop", Row: "producer", Col: Waiting, Score: 2, Rev: 3, Fields: map[string]string{"kind": "sentinel", "needs": "p", "held": "yes"}}
	s.Work.Put(p)
	s.Work.Put(c)
	return s, c, SentinelObservation{Input: SentinelInputOf(s, c), Inclusions: []DevInclusion{{Producer: "p", Repo: "/fixture/repo", Stream: "producer", Trunk: Trunk(s, "producer"), Head: "abc", Containing: "def", Included: true}}}
}

func TestSentinelAutoReleaseRequiresFreshActualTrunkProof(t *testing.T) {
	s, c, proof := sentinelProofFixture(t)
	_, _, ok := SentinelDevReason(s, c, nil)
	require.False(t, ok)
	reason, merges, ok := SentinelDevReason(s, c, []SentinelObservation{proof})
	require.True(t, ok)
	require.NotEmpty(t, reason)
	require.Equal(t, "def", merges[0].Containing)
	for _, mutate := range []func(){func() { s.Epoch++ }, func() { c.Rev++ }, func() { s.Work.Card("p").Fields["head"] = "new" }, func() { c.Fields["needs"] = "missing" }} {
		s, c, proof = sentinelProofFixture(t)
		mutate()
		_, _, ok = SentinelDevReason(s, c, []SentinelObservation{proof})
		require.False(t, ok)
	}
}

func TestSentinelEmptyAndDroppedNeedNoInventedMerge(t *testing.T) {
	s, c, _ := sentinelProofFixture(t)
	s.Work.Put(&Card{ID: "p", Fields: map[string]string{"outcome": "dropped"}})
	reason, merges, ok := SentinelDevReason(s, c, nil)
	require.True(t, ok)
	require.Equal(t, "all producers dropped", reason)
	require.Empty(t, merges)
	c.Fields["needs"] = ""
	c.Score = 0
	reason, merges, ok = SentinelDevReason(s, c, nil)
	require.True(t, ok)
	require.Equal(t, "no producers", reason)
	require.Empty(t, merges)
}

func TestSentinelAutomaticReleaseDoesNotWaitForReachedOrHold(t *testing.T) {
	s, c, proof := sentinelProofFixture(t)
	units, _, _, landing := SentinelReleases(s, MachineActor, proof)
	require.True(t, landing[c.ID])
	require.Len(t, units, 1)
	require.Contains(t, units[0].Notes[0].What, "at def")
}

func TestConsumerFailedOrStaleMergeCannotAuthorizeNextDeal(t *testing.T) {
	s, stop, _ := sentinelProofFixture(t)
	stop.Col = Landed
	c := &Card{ID: "consumer", Row: "consumer", Col: Ready, Rev: 1, Fields: map[string]string{"needs": "stop", "brief": "consumer\nREPO: /fixture/repo\n"}}
	s.Work.Put(c)
	require.False(t, ConsumerTrunkReady(s, c))
	proof := ConsumerMerge{Epoch: s.Epoch, Card: SentinelCardOf(c), Repo: "/fixture/repo", Branch: StreamBranch(s, c.Row), Trunk: Trunk(s, c.Row), Observed: "def", Merged: "merged", Success: true, ContainsObserved: true}
	s.ConsumerMerges = []ConsumerMerge{proof}
	require.True(t, ConsumerTrunkReady(s, c))
	s.ConsumerMerges[0].Success = false
	require.False(t, ConsumerTrunkReady(s, c))
	s.ConsumerMerges[0] = proof
	c.Rev++
	require.False(t, ConsumerTrunkReady(s, c))
}

func TestNamedSentinelExpandsActualProducerProof(t *testing.T) {
	s, upstream, _ := sentinelProofFixture(t)
	upstream.Col = Landed
	upstream.Fields["release_kind"] = "automatic"
	downstream := &Card{ID: "next-stop", Row: "consumer", Col: Waiting, Score: 1, Fields: map[string]string{"kind": "sentinel", "needs": "stop"}}
	s.Work.Put(downstream)
	input := SentinelInputOf(s, downstream)
	proof := SentinelObservation{Input: input, Inclusions: []DevInclusion{{Producer: "p", Repo: "/fixture/repo", Stream: "producer", Trunk: Trunk(s, "producer"), Head: "abc", Containing: "def", Included: true}}}
	_, _, ok := SentinelDevReason(s, downstream, []SentinelObservation{proof})
	require.True(t, ok)
	proof.Inclusions = nil
	_, _, ok = SentinelDevReason(s, downstream, []SentinelObservation{proof})
	require.False(t, ok)
}

func TestSentinelClosureLoadsRetainedDroppedProducers(t *testing.T) {
	s, upstream, _ := sentinelProofFixture(t)
	upstream.Col = Landed
	upstream.Fields["release_kind"] = "automatic"
	upstream.Fields["needs"] = "kept-drop"
	s.Work.Put(&Card{ID: "kept-drop", Fields: map[string]string{"outcome": "dropped"}})
	downstream := &Card{ID: "next-stop", Row: "consumer", Col: Waiting, Fields: map[string]string{"kind": "sentinel", "needs": "stop"}}
	s.Work.Put(downstream)
	require.Contains(t, ResolveExtras(s), "kept-drop")
	input := SentinelInputOf(s, downstream)
	require.Contains(t, input.Producers, SentinelCardOf(s.Work.Card("kept-drop")))
}

func TestSentinelReachedIsProgressRatherThanCoordinatorGate(t *testing.T) {
	s, c, _ := sentinelProofFixture(t)
	note := reachedNote(s, c, nil, 0, MachineActor)
	require.Equal(t, Happened, note.Kind)
	require.Empty(t, note.Decisions)
}

func TestSentinelPlacedProducerWithRetainedDroppedOutcomeIsNotComplete(t *testing.T) {
	s, c, _ := sentinelProofFixture(t)
	p := s.Work.Card("p")
	p.Col = Ready
	p.Fields["outcome"] = "dropped"
	_, _, ok := SentinelDevReason(s, c, nil)
	require.False(t, ok, "a recreated placed producer is new work, not a dropped waiver")
}

func TestDroppedNamedSentinelDoesNotWaiveAnIndependentProducer(t *testing.T) {
	s, _, _ := sentinelProofFixture(t)
	s.Work.Card("p").Col = Ready
	s.Work.Put(&Card{ID: "stop", Fields: map[string]string{"kind": "sentinel", "outcome": "dropped", "needs": "p"}})
	downstream := &Card{ID: "next-stop", Row: "consumer", Col: Waiting, Fields: map[string]string{"kind": "sentinel", "needs": "stop"}}
	s.Work.Put(downstream)
	reason, proof, ok := SentinelDevReason(s, downstream, nil)
	require.True(t, ok)
	require.Equal(t, "all producers dropped", reason)
	require.Empty(t, proof)
	downstream.Fields["needs"] = "stop,p"
	_, _, ok = SentinelDevReason(s, downstream, nil)
	require.False(t, ok, "a dropped control producer does not waive independently named active work")
}
