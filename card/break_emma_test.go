package card

// Break-It campaign findings on card/ at b2b0b8c267da7a58afed80a4ff7de1ad20d4845d
// (v2-card-fix4). Every TestBreak* test fails on b2b0b8c2 and asserts a stated
// design rule or model invariant from docs/LAYER-2-CARD-MACHINE.md and
// tla/CardMachine.tla.

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBreakNestedMergeLeavesReadyProducerInHeldStream breaks
// CardMachine.ParkedUnderHold ("No producer is ready in a held stream after
// any step", docs/LAYER-2-CARD-MACHINE.md; L2-M15) and "Merge is sequential
// composition" (third cold read rulings).
//
// Root cause in card/replay.go:95: compose mutates cur in-place via foldWrites
// and stores the post-state in done.seen. When inner = Merge(unblock, hold) is
// merged as the second argument of Merge(replace, inner), unionMissing copies
// pReady from inner.seen where pReady.State is already Waiting (its post-state
// from hold). When inner.again(cur) re-runs hold via readyOn(cur, "s1"), it
// sees pReady.State == Waiting and omits parking pReady. Both card.Merge and
// store.Apply accept the plan, leaving pReady ready inside held stream s1.
func TestBreakNestedMergeLeavesReadyProducerInHeldStream(t *testing.T) {
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

// TestBreakNestedMergeRightAssociatedRefusesValidMoves breaks "Merge is
// sequential composition" (docs/LAYER-2-CARD-MACHINE.md, third cold read
// rulings) and commit b2b0b8c2 ("the merged plan carries that replay so nested
// merges compose").
//
// Root cause in card/replay.go:95: compose saves the post-state cur in
// done.seen instead of the pre-state. Consequently:
//  1. For three independent waiting producers p1, p2, p3 where each Resolve
//     plan reads only its own producer, Merge(a, b, c) and Merge(Merge(a, b), c)
//     both succeed, whereas right-associated Merge(a, Merge(b, c)) refuses
//     ("producer p2 is in ready, Resolve needs waiting") because Merge(b, c).seen
//     has p2 and p3 in their post-state.
//  2. Because ReportPlan (card/report.go:667) returns Merge(cons, prod), any
//     Merge(other, ReportPlan(...)) where other did not read p1.w1 refuses
//     ("consumer p1.w1 is done; nothing moves a done card").
func TestBreakNestedMergeRightAssociatedRefusesValidMoves(t *testing.T) {
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
		w.op("take", w.consumerPlan("p1.w1", Take{Row: "r1"}))

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

// TestBreakMergeAddAndSubsequentMoveBreaksAtCardAndStore breaks sequential
// composition of MoveAdd with a subsequent move in the same op
// (docs/LAYER-1-STORE.md: "put card sets stream, score, needs and brief, so an
// op may put a card and set its state in the same transaction (add and ready
// at once)"; docs/LAYER-2-CARD-MACHINE.md: "Merge is sequential composition",
// "no plan the machine emits is ever invalid at the store").
//
// Two root causes in card/replay.go:
//  1. cardFromPut (card/replay.go:539) constructs the folded Card without
//     setting State: Waiting (leaving State: ""). A subsequent Resolve on the
//     added card in Merge(addC1, resolveC1) sees State == "" and refuses with
//     "producer c1 is in , Resolve needs waiting".
//  2. keepExpects/expectField (card/replay.go:163-199) does not drop CardGen
//     fences on a card created earlier in the same op by PutCard. Thus
//     Merge(addC1, replaceP1ByC1) is accepted by card.Merge but emits both
//     CardAbsent{Card: "c1"} and CardGen{Card: "c1", Gen: 0} in Expect, which
//     store.Apply always refuses ("card c1 does not exist").
func TestBreakMergeAddAndSubsequentMoveBreaksAtCardAndStore(t *testing.T) {
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

// TestBreakPreemptGuardBypassedByNestedMergeAndSetPriority breaks the blocker
// preemption invariants ("Preemption withdraws a working consumer only (a
// ready one holds no width)", docs/LAYER-2-CARD-MACHINE.md; Cold Read 3 & 4
// preemption rulings).
//
// Root cause in card/replay.go:91-101, 204-230:
//  1. compose sets done.ev = nil on a merged plan, and preemptGuard only
//     inspects top-level p.ev.(Start) and q.ev.(Withdraw), so nesting Withdraw
//     inside Merge(wd, other) completely hides it from preemptGuard.
//  2. preemptGuard reads b.Priority from p.seen.cards (the pre-plan before
//     foldPlans), so raising a producer to "blocker" via SetPriority earlier
//     in the same Merge is invisible to preemptGuard.
func TestBreakPreemptGuardBypassedByNestedMergeAndSetPriority(t *testing.T) {
	t.Run("nested Merge hides Withdraw of Ready consumer from preemptGuard", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("b"), producer("p3"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "sprint/p1", Base: base}) // p1.w1 is Ready (not Working)
		w.move("b", SetPriority{Level: "blocker", Reason: "p0"})
		w.move("b", Resolve{})

		s1 := StreamFromStore(w.snap().Streams["s1"])
		wdReady := w.consumerPlan("p1.w1", Withdraw{Reason: "silent", Stream: s1})
		startB, r := w.plan("b", Start{Row: "r1", Branch: "sprint/b", Base: base})
		require.Nil(t, r)
		resP3, r := w.plan("p3", Resolve{})
		require.Nil(t, r)

		// Flat Merge(wdReady, startB) is rightly refused by preemptGuard.
		_, rFlat := Merge(wdReady, startB)
		require.NotNil(t, rFlat, "flat Merge refuses withdrawing a ready consumer with a blocker Start")

		// Nesting wdReady inside Merge(wdReady, resP3) bypasses preemptGuard.
		inner, r := Merge(wdReady, resP3)
		require.Nil(t, r)
		_, rNested := Merge(inner, startB)
		assert.NotNil(t, rNested, "nested Merge(Merge(wdReady, resP3), startB) must also refuse withdrawing a ready consumer")
	})

	t.Run("SetPriority to blocker in same Merge hides blocker Start from preemptGuard", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("b"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "sprint/p1", Base: base}) // p1.w1 is Ready (not Working)
		w.move("b", Resolve{})

		s1 := StreamFromStore(w.snap().Streams["s1"])
		setBlocker, r := w.plan("b", SetPriority{Level: "blocker", Reason: "p0"})
		require.Nil(t, r)
		wdReady := w.consumerPlan("p1.w1", Withdraw{Reason: "silent", Stream: s1})
		startB, r := w.plan("b", Start{Row: "r1", Branch: "sprint/b", Base: base})
		require.Nil(t, r)

		_, rMerged := Merge(setBlocker, wdReady, startB)
		assert.NotNil(t, rMerged, "Merge(SetPriority blocker, Withdraw ready consumer, Start b) must refuse withdrawing a ready consumer")
	})
}

// TestBreakMergeDropsRerunExpectFenceAndLandsCycle breaks CardMachine.OrderAcyclic
// ("the needs and after graphs stay acyclic", docs/LAYER-2-CARD-MACHINE.md;
// L2-M16: "Relink fences every card its acyclic walk reads").
//
// Root cause in card/replay.go:117: foldPlans re-runs each plan against the
// folded state (producing next), and uses next.Writes, but appends
// keepExpects(p.Expect, written) from the original p.Expect instead of
// keepExpects(next.Expect, written). When an earlier plan in Merge updates
// c1's Needs list to ["c2"] and the next plan relinks c3 from dead to c1,
// re-running the Relink walks c3 -> c1 -> c2 -> dead and adds
// CardGen{Card: "c2", Gen: 0} to next.Expect, which foldPlans discards. When a
// concurrent op relinks c2 to c3, store.Apply still accepts the merged plan
// and lands a cycle c3 -> c1 -> c2 -> c3.
func TestBreakMergeDropsRerunExpectFenceAndLandsCycle(t *testing.T) {
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
