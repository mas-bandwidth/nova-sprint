package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Automatic release uses fresh ancestry, while consumers merge their trunk later.
func TestACutSentinelReleasesItselfOnTheTick(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.must(Lawful(Add(w.s, AddReq{Stream: "rel", IDs: []string{"cut"}, Sentinel: true, Needs: []string{"s1-1", "s1-2"}})))
	w.must(Lawful(Add(w.s, AddReq{Stream: "rel", IDs: []string{"b"}})))
	cut := w.s.Work.Card("cut")
	require.Equal(t, Waiting, cut.Col, "cut: %s %v", cut.Col, cut.Fields)
	require.Equal(t, Waiting, w.state("b"), "b waits behind the sentinel")
	p, _ := TickResolve(w.s, TickReq{})
	require.Empty(t, p.Units, "released before its needs landed: %+v", p)
	accepted(w, "s1-1", "s1-2")
	mergeOne(w, "s1")
	p = mergeOne(w, "s1")
	require.Len(t, notesIn(p, NSentinelReached), 1, "the last landing reached it: %+v", p.Units)
	require.Equal(t, Waiting, cut.Col, "the landing step never lands it")
	// Fake forge facts use explicit heads/repositories and exact scope; a mere
	// landed state above was not enough to release.
	for _, id := range []string{"s1-1", "s1-2"} {
		p := w.s.Work.Card(id)
		p.Fields["head"] = id + "-head"
		p.Fields["brief"] = "fixture\nREPO: /fixture/repo\n"
	}
	observation := SentinelObservation{Input: SentinelInputOf(w.s, cut)}
	for _, id := range []string{"s1-1", "s1-2"} {
		observation.Inclusions = append(observation.Inclusions, DevInclusion{Producer: id, Repo: "/fixture/repo", Stream: "s1", Trunk: Trunk(w.s, "s1"), Head: id + "-head", Containing: "observed-trunk", Included: true})
	}
	sp, _ := TickResolve(w.s, TickReq{SentinelObservations: []SentinelObservation{observation}})
	require.Len(t, sp.Units, 1, "release happens before the consumer merge: %+v", sp.Units)
	require.True(t, landsSentinel(w.s, sp), "the tick releases it: %+v", sp)
	landed := notesIn(sp, NSentinelLanded)
	require.Len(t, landed, 1, "the landing notification: %+v", sp)
	require.Equal(t, MachineActor, landed[0].Who, "it released itself: %+v", landed[0])
	push := sp.Notes
	require.Len(t, push, 1, "the seat push: %+v", sp.Notes)
	require.Equal(t, NCutReady, push[0].Type, "the seat push: %+v", push[0])
	require.Equal(t, w.s.Coordinator, push[0].To, "the push is addressed to the seat: %+v", push[0])
	require.Equal(t, MachineActor, push[0].Who, "the push: %+v", push[0])
	require.Contains(t, push[0].What, "automatic release happened", "the push names what happened: %+v", push[0])
	w.do(sp)
	require.Equal(t, Landed, cut.Col, "released: cut %s, b %s", cut.Col, w.state("b"))
	require.Equal(t, MachineActor, cut.F("released_by"), "released: cut %v", cut.Fields)
	require.Equal(t, "every producer is complete and its head is in the observed trunk", cut.F("release_reason"), "released: cut %v", cut.Fields)
	require.Equal(t, Waiting, w.state("b"), "what waited behind it is ready: cut %s, b %s", cut.Col, w.state("b"))
	require.Empty(t, w.openOn("cut"), "its reached judgment is closed: %v", w.openOn("cut"))
	require.Empty(t, w.openOn(StreamSubject("rel")), "the stream holds no judgment: %v", w.openOn(StreamSubject("rel")))
	// The tick that follows moves nothing again.
	q, _ := TickResolve(w.s, TickReq{})
	require.Empty(t, q.Units, "released twice: %+v", q)
	w.clean("released itself")
}

// A position gate still needs ancestry; an empty legacy held sentinel does not.
func TestPlainGateNeedsProofWhileEmptyLegacyHoldAutoReleases(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"gate"}, Sentinel: true}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"hold"}, Sentinel: true}))
	w.s.Work.Card("hold").Fields[FieldHeld] = stamp(w.s.Now)
	accepted(w, "s1-1")
	mergeOne(w, "s1")
	p, _ := TickResolve(w.s, TickReq{})
	require.True(t, landsSentinel(w.s, p))
	w.do(p)
	require.Equal(t, Waiting, w.state("gate"), "landed without fetched ancestry is insufficient")
	require.Equal(t, Landed, w.state("hold"), "empty legacy hold cannot gate automatic release")
}

// A need still in flight is not landed: the strict gate. The coordinator's release waives
// what is under way; the machine's release does not.
func TestACutSentinelWithANeedInFlightWaitsForTheTick(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.must(Lawful(Add(w.s, AddReq{Stream: "rel", IDs: []string{"cut"}, Sentinel: true, Needs: []string{"s1-1", "s1-2"}})))
	accepted(w, "s1-1")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2"}}}))
	p, _ := TickResolve(w.s, TickReq{})
	require.Empty(t, p.Units, "released with s1-1 merging and s1-2 working: %+v", p.Units)
	require.Equal(t, Waiting, w.state("cut"), "cut is %s", w.state("cut"))
}
