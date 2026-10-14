package refmodel

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelSentinelRequiresScopedExternalAncestry(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, "lead")
	s.Primaries["p"] = Primary{Repo: "/fixture/repo", Stream: "s", Kind: KindPrimary, State: Landed, Head: 1, Score: 1}
	s.Primaries["stop"] = Primary{Stream: "s", Kind: KindSentinel, State: Waiting, Score: 2}
	without := s.Clone()
	without.tickResolve()
	require.Equal(t, Waiting, without.Primaries["stop"].State, "Landed alone is not ancestry")
	require.False(t, without.Open[Judgment{JReached, "stop"}])
	fact := ProducerTrunkFact{Scope: s.SentinelProtocolScope("stop"), Observed: "fixture-trunk", IncludedHeads: map[string]int{"p": 1}}
	with := s.Clone()
	with.protocol = &ProtocolInput{Producers: map[string]ProducerTrunkFact{"stop": fact}}
	with.tickResolve()
	require.Equal(t, Landed, with.Primaries["stop"].State)
	stale := s.Clone()
	stale.setPrimary("p", func(p *Primary) { p.Head = 2 })
	stale.protocol = with.protocol
	stale.tickResolve()
	require.Equal(t, Waiting, stale.Primaries["stop"].State)
}

func TestModelEmptyAndDroppedSentinelsReleaseWithoutProof(t *testing.T) {
	t.Parallel()
	for _, dropped := range []bool{false, true} {
		s := New(nil, nil, "lead")
		s.Primaries["stop"] = Primary{Stream: "s", Kind: KindSentinel, State: Waiting, Score: 2}
		if dropped {
			s.Primaries["p"] = Primary{Stream: "s", Kind: KindPrimary, State: Off, Score: 1}
			s.setPrimary("stop", func(p *Primary) { p.Needs = []string{"p"} })
		}
		s.tickResolve()
		require.Equal(t, Landed, s.Primaries["stop"].State)
	}
}

func TestModelConsumerRejectsNewBarrierAndChangedCard(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, "lead")
	s.Primaries["a"] = Primary{Stream: "s", Kind: KindSentinel, State: Landed, Score: 1}
	s.Primaries["c"] = Primary{Repo: "/fixture/repo", Stream: "s", Kind: KindPrimary, State: Waiting, Score: 3}
	fact := ConsumerTrunkFact{Scope: s.ConsumerProtocolScope("c"), Observed: "fixture-trunk", Merged: "fixture-merge", ContainsObserved: true}
	s.protocol = &ProtocolInput{Consumers: map[string]ConsumerTrunkFact{"c": fact}}
	require.True(t, s.consumerTrunkReady("c"))
	changed := s.Clone()
	changed.setPrimary("c", func(p *Primary) { p.State = Ready })
	require.False(t, changed.consumerTrunkReady("c"), "fresh Ready identity is required before deal")
	s.Primaries["b"] = Primary{Stream: "s", Kind: KindSentinel, State: Landed, Score: 2}
	require.False(t, s.consumerTrunkReady("c"), "unchanged card cannot reuse old barrier vector")
}

func TestModelEarlyReleaseDoesNotInventConsumerMerge(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, "lead")
	s.Primaries["p"] = Primary{Stream: "s", Kind: KindPrimary, State: Working, Score: 1}
	s.Primaries["stop"] = Primary{Stream: "s", Kind: KindSentinel, State: Waiting, Score: 2}
	s.Primaries["c"] = Primary{Stream: "s", Kind: KindPrimary, State: Waiting, Score: 3}
	n, err := Release(s, []string{"stop"}, "lead")
	require.NoError(t, err)
	require.Equal(t, Landed, n.Primaries["stop"].State)
	require.True(t, n.Primaries["stop"].ReleasedEarly)
	require.Equal(t, Waiting, n.Primaries["c"].State)
}

func TestModelUnnamedEarlierSentinelIsNotAProducer(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, "lead")
	s.Primaries["earlier"] = Primary{Stream: "s", Kind: KindSentinel, State: Waiting, Score: 1, Needs: []string{"elsewhere"}}
	s.Primaries["elsewhere"] = Primary{Stream: "other", Kind: KindPrimary, State: Working, Score: 1}
	s.Primaries["p"] = Primary{Repo: "/fixture/repo", Stream: "s", Kind: KindPrimary, State: Landed, Head: 1, Score: 2}
	s.Primaries["stop"] = Primary{Stream: "s", Kind: KindSentinel, State: Waiting, Score: 3}
	fact := ProducerTrunkFact{Scope: s.SentinelProtocolScope("stop"), Observed: "fixture-trunk", IncludedHeads: map[string]int{"p": 1}}
	require.NotContains(t, fact.Scope.Cards, "earlier")
	s.protocol = &ProtocolInput{Producers: map[string]ProducerTrunkFact{"stop": fact}}
	s.tickResolve()
	require.Equal(t, Landed, s.Primaries["stop"].State)
	require.Equal(t, Waiting, s.Primaries["earlier"].State)
}

func TestModelProtocolCloneOwnsOutsideFactArraysAndMaps(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, "lead")
	s.Primaries["stop"] = Primary{Stream: "s", Kind: KindSentinel, State: Waiting, Needs: []string{"p"}}
	s.Primaries["p"] = Primary{Stream: "s", Kind: KindPrimary, State: Landed, Head: 1}
	s.protocol = &ProtocolInput{Producers: map[string]ProducerTrunkFact{"stop": {Scope: s.SentinelProtocolScope("stop"), Observed: "fixture", IncludedHeads: map[string]int{"p": 1}}}}
	clone := s.Clone()
	fact := clone.protocol.Producers["stop"]
	fact.Scope.Cards["stop"].Needs[0] = "other"
	fact.IncludedHeads["p"] = 2
	fact.Scope.Targets["s"] = ProtocolTarget{"other", "main"}
	require.Equal(t, "p", s.protocol.Producers["stop"].Scope.Cards["stop"].Needs[0])
	require.Equal(t, 1, s.protocol.Producers["stop"].IncludedHeads["p"])
	require.Equal(t, ProtocolTarget{"sprint/s", "dev"}, s.protocol.Producers["stop"].Scope.Targets["s"])
}
