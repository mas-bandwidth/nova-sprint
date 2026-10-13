package store

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/require"
)

func TestSentinelTickLoadsRetainedDroppedTransitiveProducer(t *testing.T) {
	h := newHarness(t)
	h.must(AddStep(sprint.AddReq{Stream: "producer", IDs: []string{"p"}}))
	h.must(AddStep(sprint.AddReq{Stream: "producer", IDs: []string{"upstream"}, Sentinel: true, Needs: []string{"p"}}))
	s := h.snap()
	p := s.Work.Card("p")
	// Seed a retained dropped record at the actual backend boundary. Dropping
	// removes its place but preserves completion evidence for named dependencies.
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: "t-work", Epoch: fmt.Sprint(s.Epoch), ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "retained-drop-fixture", Members: []ntable.BatchMemberEntry{{ID: sprint.StoredID(p.ID, s.Epoch), Expect: &ntable.MemberExpect{Revision: fmt.Sprint(p.Rev)}, Remove: true, Set: map[string]string{"outcome": "dropped"}}}})
	require.NoError(t, err)
	h.must(ReleaseStep(sprint.ReleaseReq{IDs: []string{"upstream"}, Reason: "fixture dropped producer", Coordinator: "tester", Who: "tester"}))
	// The fixture represents the automatic upstream audit, whose closure must
	// still load the dropped ordinary producer for the downstream proof input.
	h.setCard(sprint.Work, "upstream", map[string]string{"release_kind": "automatic"})
	h.must(AddStep(sprint.AddReq{Stream: "consumer", IDs: []string{"downstream"}, Sentinel: true, Needs: []string{"upstream"}}))
	loaded, err := h.st.Load(h.ctx, All, tickExtras)
	require.NoError(t, err)
	require.NotNil(t, loaded.Work.Card("p"))
	require.Nil(t, loaded.Work.Placed("p"))
	require.Equal(t, "dropped", loaded.Work.Card("p").F("outcome"))
	reason, proof, ok := sprint.SentinelDevReason(loaded, loaded.Work.Card("downstream"), nil)
	require.True(t, ok)
	require.Equal(t, "all producers dropped", reason)
	require.Empty(t, proof)
}

// installSentinelFixtureForge declares only this test's producer/consumer
// observations. The command adapter is separately tested against a real local
// bare origin; this store fixture never touches Git or a real forge.
func installSentinelFixtureForge(h *harness, stopID string, producers, consumers []string) func() {
	producersMerged := false
	observed := func() string {
		if producersMerged {
			return "fixture-observed-trunk"
		}
		return "fixture-trunk-before"
	}
	h.t.Helper()
	for _, id := range producers {
		c := h.snap().Work.Card(id)
		h.setCard(sprint.Work, id, map[string]string{"brief": c.F("brief") + "\nREPO: /fixture/repo\n"})
	}
	for _, id := range consumers {
		c := h.snap().Work.Card(id)
		brief := c.F("brief")
		if brief == "" {
			brief = "fixture"
		}
		h.setCard(sprint.Work, id, map[string]string{"brief": brief + "\nREPO: /fixture/repo\n"})
	}
	h.st.SentinelProof = func(_ context.Context, s *sprint.Snapshot) []sprint.SentinelObservation {
		stop := s.Work.Card(stopID)
		if stop == nil || stop.Col != sprint.Waiting {
			return nil
		}
		input := sprint.SentinelInputOf(s, stop)
		observation := sprint.SentinelObservation{Input: input}
		for _, id := range producers {
			p := s.Work.Card(id)
			if p == nil {
				continue
			}
			observation.Inclusions = append(observation.Inclusions, sprint.DevInclusion{Producer: id, Repo: swarm.ReadCardBase([]byte(p.F("brief"))).Repo, Stream: p.Row, Trunk: sprint.Trunk(s, p.Row), Head: p.F("head"), Containing: observed(), Included: producersMerged})
		}
		return []sprint.SentinelObservation{observation}
	}
	h.st.ConsumerTrunk = func(_ context.Context, s *sprint.Snapshot) []sprint.ConsumerMerge {
		var proofs []sprint.ConsumerMerge
		for _, id := range consumers {
			c := s.Work.Card(id)
			if c == nil || !sprint.ConsumerNeedsTrunk(s, c) {
				continue
			}
			proofs = append(proofs, sprint.ConsumerMerge{Barriers: sprint.ConsumerBarriersOf(s, c), Epoch: s.Epoch, Card: sprint.SentinelCardOf(c), Repo: swarm.ReadCardBase([]byte(c.F("brief"))).Repo, Branch: sprint.StreamBranch(s, c.Row), Trunk: sprint.Trunk(s, c.Row), Observed: observed(), Merged: "fixture-merged-consumer", Success: true, ContainsObserved: true})
		}
		return proofs
	}
	return func() { producersMerged = true }
}

func resolveFixtureConsumers(h *harness, ids []string) Result {
	h.t.Helper()
	for _, id := range ids {
		require.Equal(h.t, sprint.Waiting, h.state(id), "release must leave consumers waiting until the merge")
	}
	step := ResolveStep(sprint.ResolveReq{Sel: sprint.Sel{Only: ids}, Who: h.st.Actor})
	step.Load = All
	plan := step.Plan
	step.Plan = func(s *sprint.Snapshot) sprint.Plan { s.ConsumerMerges = h.st.ConsumerTrunk(h.ctx, s); return plan(s) }
	return h.must(step)
}

// This is an explicit stored-shape fixture: guarded metadata writes can
// carry an old audit field on a placed record. Normal Add does not reuse a
// kept dropped identity; no such command behavior is claimed here.
func TestSentinelStoredPlacedProducerWithDroppedAuditIsNotWaived(t *testing.T) {
	h := newHarness(t)
	h.must(AddStep(sprint.AddReq{Stream: "producer", IDs: []string{"p"}}))
	h.must(AddStep(sprint.AddReq{Stream: "producer", IDs: []string{"stop"}, Sentinel: true, Needs: []string{"p"}}))
	h.must(AddStep(sprint.AddReq{Stream: "producer", IDs: []string{"consumer-after"}}))
	h.setCard(sprint.Work, "p", map[string]string{"outcome": "dropped"})
	loaded, err := h.st.Load(h.ctx, All, tickExtras)
	require.NoError(t, err)
	require.NotNil(t, loaded.Work.Placed("p"))
	require.Equal(t, sprint.Ready, loaded.StateOf("p"))
	require.Equal(t, "dropped", loaded.Work.Card("p").F("outcome"))
	result := h.run(TickPartStep("resolve", sprint.TickResolve, sprint.TickReq{}, nil, nil, nil))
	require.Equal(t, sprint.Waiting, h.state("stop"), "placed work is not a dropped completion waiver")
	require.Empty(t, result.Moved)
	require.Equal(t, sprint.Waiting, h.state("consumer-after"))
	require.Empty(t, h.snap().Work.Card("consumer-after").F("work"), "consumer must remain undealt")
	_, _, ok := sprint.SentinelDevReason(loaded, loaded.Work.Card("stop"), nil)
	require.False(t, ok)
}

// Finish records the declared head before any review, preserving real read
// evidence. It is scoped to these sentinel drivers, not the generic harness.
func workWithSentinelFixtureHead(h *harness, member string, allowed []string) {
	h.t.Helper()
	for {
		h.run(TakeStep(sprint.TakeReq{As: member, Sel: sprint.Sel{Limit: 100}, Who: member}))
		s := h.snap()
		work := s.Fleet.Cell(member, sprint.Working)
		if len(work) == 0 {
			return
		}
		for _, c := range work {
			primary := c.F("primary")
			matched := false
			for _, id := range allowed {
				matched = matched || id == primary
			}
			require.True(h.t, matched, "unexpected fixture worker primary %s", primary)
			h.must(FinishStep(sprint.FinishReq{As: member, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}, Who: member, Head: "fixture-head-" + primary}))
		}
	}
}
