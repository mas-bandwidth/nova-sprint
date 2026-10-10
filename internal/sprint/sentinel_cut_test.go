package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The cut sentinel releases itself (docs/roadmap.sexp, auto-sentinels-released-by-tick;
// the owner, 2026-10-10: "what else is manual that should be automatic from the machine"):
// a sentinel that names its needs, marked reached by the step that landed its last need,
// is released by the next tick's resolve and the seat is pushed that the release is ready
// to cut; the publish itself stays a gated step with a cold read. A plain position gate (a
// wave's stop, no needs named) and a held sentinel keep the coordinator's release, and a
// need still in flight is not landed: the machine's release waives nothing.
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
	// The next tick's resolve releases it.
	sp, _ := TickResolve(w.s, TickReq{})
	require.Len(t, sp.Units, 2, "the release and the wave behind it: %+v", sp.Units)
	require.True(t, landsSentinel(w.s, sp), "the tick releases it: %+v", sp)
	landed := notesIn(sp, NSentinelLanded)
	require.Len(t, landed, 1, "the landing notification: %+v", sp)
	require.Equal(t, MachineActor, landed[0].Who, "it released itself: %+v", landed[0])
	push := sp.Notes
	require.Len(t, push, 1, "the seat push: %+v", sp.Notes)
	require.Equal(t, NCutReady, push[0].Type, "the seat push: %+v", push[0])
	require.Equal(t, w.s.Coordinator, push[0].To, "the push is addressed to the seat: %+v", push[0])
	require.Equal(t, MachineActor, push[0].Who, "the push: %+v", push[0])
	require.Contains(t, push[0].What, "ready to cut", "the push says what the seat may cut: %+v", push[0])
	w.do(sp)
	require.Equal(t, Landed, cut.Col, "released: cut %s, b %s", cut.Col, w.state("b"))
	require.Equal(t, MachineActor, cut.F("released_by"), "released: cut %v", cut.Fields)
	require.Equal(t, "every card it needs has landed", cut.F("release_reason"), "released: cut %v", cut.Fields)
	require.Equal(t, Ready, w.state("b"), "what waited behind it is ready: cut %s, b %s", cut.Col, w.state("b"))
	require.Empty(t, w.openOn("cut"), "its reached judgment is closed: %v", w.openOn("cut"))
	require.Empty(t, w.openOn(StreamSubject("rel")), "the stream holds no judgment: %v", w.openOn(StreamSubject("rel")))
	// The tick that follows moves nothing again.
	q, _ := TickResolve(w.s, TickReq{})
	require.Empty(t, q.Units, "released twice: %+v", q)
	w.clean("released itself")
}

// A plain gate (no needs named) is a wave the coordinator passes on their own look, and a
// held sentinel keeps its hold: the tick releases neither.
func TestAPlainGateAndAHeldSentinelAreNotReleasedByTheTick(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Lawful(Add(w.s, AddReq{Stream: "s1", IDs: []string{"gate"}, Sentinel: true})))
	w.must(Lawful(Add(w.s, AddReq{Stream: "s2", IDs: []string{"hold"}, Sentinel: true, Held: true})))
	accepted(w, "s1-1")
	mergeOne(w, "s1")
	require.NotEmpty(t, w.s.Work.Card("gate").F("reached"), "the gate is reached")
	p, _ := TickResolve(w.s, TickReq{})
	require.Empty(t, p.Units, "the tick released a gate or a hold: %+v", p.Units)
	require.Equal(t, Waiting, w.state("gate"), "the gate is %s", w.state("gate"))
	require.Equal(t, Waiting, w.state("hold"), "the hold is %s", w.state("hold"))
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
