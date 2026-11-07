package card

// Nested and right-associated merges, Add followed by a move in the same op,
// the preempt guard, and the fences of re-run plans. Each test asserts a
// design rule or model invariant from AGENTS.md, Card machine and
// tla/CardMachine.tla.

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNestedMergeLeavesReadyProducerInHeldStream checks
// CardMachine.ParkedUnderHold ("No producer is ready in a held stream after
// any step", AGENTS.md, Card machine) and "Merge is sequential
// composition".
//
// inner = Merge(unblock, hold) merged as the second argument of
// Merge(replace, inner): re-running the hold against the folded state must
// still park pReady, which is ready there, so neither card.Merge nor
// store.Apply may accept a plan that leaves pReady ready inside held stream
// s1.
func TestNestedMergeLeavesReadyProducerInHeldStream(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("pReady"), producer("p1"), producer("p2"))
	w.move("pReady", Resolve{})
	w.move("p2", Resolve{})
	w.move("p2", Block{Reason: "bounced"})

	s1 := StreamFromStore(w.snap().Streams["s1"])
	p1 := w.card("p1")
	p2 := w.card("p2")
	pReady := w.card("pReady")

	// planReplace only reads p1 and p2 (not pReady).
	planReplace, r := Move(p1, nil, nil, s1, map[string]Card{"p2": p2}, Replace{By: "p2", Reason: "dup"})
	require.Nil(t, r)
	planUnblock, r := Move(p2, nil, nil, s1, nil, Unblock{Reason: "thaw"})
	require.Nil(t, r)
	planHold, r := MoveStream(s1, HoldStream{Seq: w.snap().Seq, Ready: []Card{pReady}, Reason: "freeze"})
	require.Nil(t, r)

	inner, r := Merge(planUnblock, planHold)
	require.Nil(t, r)
	merged, r := Merge(planReplace, inner)
	if r != nil {
		return
	}
	w.op("merge", merged)

	snap := w.snap()
	require.True(t, snap.Streams["s1"].Held, "stream s1 was held by HoldStream")
	assert.Equal(t, Waiting, snap.Cards["pReady"].State,
		"ParkedUnderHold: pReady must be parked (waiting) when stream s1 is held, not left in ready")
}

// TestNestedMergeRightAssociatedRefusesValidMoves checks "Merge is
// sequential composition" (AGENTS.md, Card machine): the merged plan
// carries the replay, so nested merges compose.
//
//  1. For three independent waiting producers p1, p2, p3 where each Resolve
//     plan reads only its own producer, Merge(a, b, c), Merge(Merge(a, b), c)
//     and right-associated Merge(a, Merge(b, c)) all succeed (the folded
//     state of Merge(b, c) must not refuse "producer p2 is in ready, Resolve
//     needs waiting").
//  2. ReportPlan (card/report.go) returns Merge(cons, prod), so
//     Merge(other, ReportPlan(...)) where other did not read p1.w1 must not
//     refuse "consumer p1.w1 is done; nothing moves a done card".
func TestNestedMergeRightAssociatedRefusesValidMoves(t *testing.T) {
	t.Parallel()
	t.Run("three independent Resolves right-associated", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("p2"), producer("p3"))
		s1 := StreamFromStore(w.snap().Streams["s1"])
		a, r := Move(w.card("p1"), nil, nil, s1, nil, Resolve{})
		require.Nil(t, r)
		b, r := Move(w.card("p2"), nil, nil, s1, nil, Resolve{})
		require.Nil(t, r)
		c, r := Move(w.card("p3"), nil, nil, s1, nil, Resolve{})
		require.Nil(t, r)

		_, rFlat := Merge(a, b, c)
		require.Nil(t, rFlat, "flat Merge(a, b, c) succeeds")
		m12, rLeft := Merge(a, b)
		require.Nil(t, rLeft)
		_, rLeft = Merge(m12, c)
		require.Nil(t, rLeft, "left-associated Merge(Merge(a, b), c) succeeds")

		m23, rRight := Merge(b, c)
		require.Nil(t, rRight)
		_, rRight = Merge(a, m23)
		assert.Nil(t, rRight, "right-associated Merge(a, Merge(b, c)) must match Merge(a, b, c) and Merge(Merge(a, b), c)")
	})

	t.Run("Merge of independent move with ReportPlan", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("p2"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "sprint/p1", Base: base})

		s1 := StreamFromStore(w.snap().Streams["s1"])
		other, r := Move(w.card("p2"), nil, nil, s1, nil, Resolve{})
		require.Nil(t, r)
		c := w.card("p1.w1")
		p1 := w.card("p1")
		rep, r := ReportPlan(c, p1, []Card{c}, w.rows(), s1,
			map[string]Card{"p1": p1, "p1.w1": c}, Report{Row: "r1", Result: "ok", Head: h1})
		require.Nil(t, r)

		_, rMerged := Merge(other, rep)
		assert.Nil(t, rMerged, "Merge(Resolve p2, ReportPlan p1.w1) must succeed as independent moves")
	})
}

// TestMergeAddAndSubsequentMoveBreaksAtCardAndStore checks sequential
// composition of MoveAdd with a subsequent move in the same op
// (AGENTS.md, Store: "put card sets stream, score, needs and brief, so an
// op may put a card and set its state in the same transaction (add and ready
// at once)"; AGENTS.md, Card machine: "Merge is sequential composition",
// "no plan the machine emits is ever invalid at the store").
//
//  1. The folded Card built from a PutCard has State: Waiting, so a
//     subsequent Resolve on the added card in Merge(addC1, resolveC1) is
//     accepted.
//  2. A CardGen fence on a card created earlier in the same op by PutCard is
//     dropped, so Merge(addC1, replaceP1ByC1) does not emit both
//     CardAbsent{Card: "c1"} and CardGen{Card: "c1", Gen: 0} in Expect, which
//     store.Apply would always refuse ("card c1 does not exist").
func TestMergeAddAndSubsequentMoveBreaksAtCardAndStore(t *testing.T) {
	t.Parallel()
	t.Run("MoveAdd followed by Resolve refuses with blank state", func(t *testing.T) {
		w := newWorld(t)
		s1 := StreamFromStore(w.snap().Streams["s1"])

		addPlan, r := MoveAdd(Add{ID: "c1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do c1"}, nil)
		require.Nil(t, r)
		addedCard := Card{ID: "c1", Kind: "producer", Stream: "s1", State: Waiting, Tier: "pro", Priority: "normal", Brief: "do c1"}
		resolvePlan, r := Move(addedCard, nil, nil, s1, nil, Resolve{})
		require.Nil(t, r)

		merged, r := Merge(addPlan, resolvePlan)
		require.Nil(t, r, "Merge(MoveAdd c1, Resolve c1) must succeed (add and ready at once), not fail with blank state")
		w.op("add+resolve", merged)
		assert.Equal(t, Ready, w.snap().Cards["c1"].State)
	})

	t.Run("MoveAdd followed by Replace emits contradictory CardAbsent and CardGen", func(t *testing.T) {
		w := newWorld(t, producer("p1"))
		s1 := StreamFromStore(w.snap().Streams["s1"])
		p1 := w.card("p1")

		addC1, r := MoveAdd(Add{ID: "c1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do c1"}, nil)
		require.Nil(t, r)
		c1New := Card{ID: "c1", Kind: "producer", Stream: "s1", State: Waiting, Tier: "pro"}
		repP1, r := Move(p1, nil, nil, s1, map[string]Card{"c1": c1New}, Replace{By: "c1", Reason: "superseded"})
		require.Nil(t, r)

		merged, r := Merge(addC1, repP1)
		require.Nil(t, r)
		rec, err := w.apply("add+replace", merged)
		require.NoError(t, err)
		assert.True(t, rec.Accepted, "store.Apply must accept a plan accepted by Merge(MoveAdd c1, Replace p1 by c1), got refusal: %s", rec.Reason)
	})
}

// TestPreemptGuardBypassedByNestedMergeAndSetPriority checks the preempt
// guard. The guard reads Withdraw.Preempt only, on the flattened moves, and
// it reads a SetPriority earlier in the same op. A plain withdraw of a ready
// consumer is not a preempt.
func TestPreemptGuardBypassedByNestedMergeAndSetPriority(t *testing.T) {
	t.Parallel()
	t.Run("a plain withdraw is not a preempt", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("b"), producer("p3"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "sprint/p1", Base: base})
		w.move("b", SetPriority{Level: "blocker", Reason: "p0"})
		w.move("b", Resolve{})

		wdReady := w.consumerPlan("p1.w1", Withdraw{Reason: "silent"})
		startB, r := w.plan("b", Start{Row: "r1", Branch: "sprint/b", Base: base})
		require.Nil(t, r)
		resP3, r := w.plan("p3", Resolve{})
		require.Nil(t, r)

		_, rFlat := Merge(wdReady, startB)
		require.Nil(t, rFlat, "a silent withdraw carries no Preempt flag")
		inner, r := Merge(wdReady, resP3)
		require.Nil(t, r)
		_, rNested := Merge(inner, startB)
		require.Nil(t, rNested, "nesting a plain withdraw does not make it a preempt")
	})

	t.Run("nested Merge still checks a preempt withdraw", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("b"), producer("p3"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "sprint/p1", Base: base})
		w.move("b", SetPriority{Level: "blocker", Reason: "p0"})
		w.move("b", Resolve{})

		wd := w.consumerPlan("p1.w1", Withdraw{Reason: "preempted", Preempt: true})
		startB, r := w.plan("b", Start{Row: "r2", Branch: "sprint/b", Base: base})
		require.Nil(t, r)
		resP3, r := w.plan("p3", Resolve{})
		require.Nil(t, r)

		_, rFlat := Merge(wd, startB)
		require.NotNil(t, rFlat, "the consumer is on r1 and the blocker starts on r2")
		inner, r := Merge(wd, resP3)
		require.Nil(t, r)
		_, rNested := Merge(inner, startB)
		assert.NotNil(t, rNested, "flattening must still see the preempt withdraw")
	})

	t.Run("SetPriority to blocker in the same Merge is the priority the guard reads", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("b"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "sprint/p1", Base: base})
		w.move("b", Resolve{})

		setBlocker, r := w.plan("b", SetPriority{Level: "blocker", Reason: "p0"})
		require.Nil(t, r)
		wd := w.consumerPlan("p1.w1", Withdraw{Reason: "preempted", Preempt: true})
		startB, r := w.plan("b", Start{Row: "r2", Branch: "sprint/b", Base: base})
		require.Nil(t, r)

		_, rMerged := Merge(setBlocker, wd, startB)
		assert.NotNil(t, rMerged, "SetPriority earlier in the op makes b the blocker")
	})
}

// TestMergeDropsRerunExpectFenceAndLandsCycle checks CardMachine.OrderAcyclic
// ("the needs and after graphs stay acyclic", AGENTS.md, Card machine:
// "Relink fences every card its acyclic walk reads").
//
// foldPlans re-runs each plan against the folded state and must keep the
// fences of the re-run plan (next.Expect), not the original's. When an
// earlier plan in Merge updates c1's Needs list to ["c2"] and the next plan
// relinks c3 from dead to c1, re-running the Relink walks c3 -> c1 -> c2 ->
// dead and fences c2 with CardGen{Card: "c2", Gen: 0}. A concurrent op that
// relinks c2 to c3 must then refuse the merged plan; it must not land a
// cycle c3 -> c1 -> c2 -> c3.
func TestMergeDropsRerunExpectFenceAndLandsCycle(t *testing.T) {
	t.Parallel()
	t.Run("foldPlans drops next.Expect CardGen fence and lands cycle in store", func(t *testing.T) {
		w := newWorld(t, producer("dead"), producer("c1"), producer("c2", "dead"), producer("c3", "dead"))
		w.move("dead", Cancel{Reason: "gone"})

		// Concurrent op A planned from the snapshot relinks c2 -> c3.
		relinkC2ToC3, r := w.plan("c2", Relink{Need: "dead", To: "c3", Reason: "swap"})
		require.Nil(t, r)

		// editC1 seeds c1 in cur; setNeedsC1 sets c1.Needs = ["c2"]; relinkC3ToC1 relinks c3 -> c1.
		editC1, r := w.plan("c1", Edit{Prose: "updated", Reason: "scope"})
		require.Nil(t, r)
		setNeedsC1 := Plan{
			Writes: []store.Write{store.SetCardNeeds{Card: "c1", Needs: []string{"c2"}}},
		}
		relinkC3ToC1, r := w.plan("c3", Relink{Need: "dead", To: "c1", Reason: "swap"})
		require.Nil(t, r)

		merged, r := Merge(editC1, setNeedsC1, relinkC3ToC1)
		require.Nil(t, r)

		// Apply concurrent op A first (c2 now needs c3, c2.Gen becomes 1).
		w.op("relink-c2", relinkC2ToC3)

		// Apply merged op B: must be refused by store.Apply because c2.Gen advanced!
		rec, err := w.apply("needs-c1-and-relink-c3", merged)
		require.NoError(t, err)
		assert.False(t, rec.Accepted,
			"OrderAcyclic: Merge must keep next.Expect's CardGen fence on c2 so concurrent Relink c2->c3 cannot close cycle c3 -> c1 -> c2 -> c3 (got c1.Needs=%v, c2.Needs=%v, c3.Needs=%v)",
			w.snap().Cards["c1"].Needs, w.snap().Cards["c2"].Needs, w.snap().Cards["c3"].Needs)
	})

	t.Run("hand-built first plan creates zero-value stub card in cur blocking unionMissing", func(t *testing.T) {
		w := newWorld(t, producer("dead"), producer("c1"), producer("c3", "dead"))
		w.move("dead", Cancel{Reason: "gone"})

		// card/replay.go:29 promises: "A hand-built plan, with no move to run again, is kept as written."
		handBuilt := Plan{
			Expect: []store.Expect{store.CardGen{Card: "c1", Gen: 0}},
			Writes: []store.Write{store.SetCardScore{Card: "c1", Score: 10}},
		}
		relinkC3ToC1, r := w.plan("c3", Relink{Need: "dead", To: "c1", Reason: "swap"})
		require.Nil(t, r)

		_, rMerged := Merge(handBuilt, relinkC3ToC1)
		assert.Nil(t, rMerged, "hand-built first plan touching c1 must not overwrite c1 with a zero-Kind stub in cur")
	})
}
