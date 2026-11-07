package card

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- a fault spends neither attempt nor bounces nor bounces ----

func TestBreakHeldFault(t *testing.T) {
	t.Parallel()
	t.Run("three faults in a row, then a fail, then a fault", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		for i := 1; i <= 3; i++ {
			c := b.run("p1", "r1", "fault", "")
			b.ok(b.P("p1", Faulted{Consumer: c}))
			assert.Equal(t, []int{i, 0}, []int{b.c("p1").Attempt, b.c("p1").Bounces})
		}
		c := b.run("p1", "r1", "fail", "")
		b.ok(b.P("p1", Broken{Consumer: c}))
		c = b.run("p1", "r1", "fault", "")
		b.ok(b.P("p1", Faulted{Consumer: c}))
		assert.Equal(t, []int{5, 1}, []int{b.c("p1").Attempt, b.c("p1").Bounces})
		// a fault is not a fail: the producer move for the other result is refused
		c = b.run("p1", "r1", "fault", "")
		b.no(b.P("p1", Broken{Consumer: c}), "reported fault, Broken needs fail")
		b.ok(b.P("p1", Faulted{Consumer: c}))
	})
	t.Run("a fault that pushed a head continues from it, and the head is not ok again", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c := b.run("p1", "r1", "fault", hA)
		b.ok(b.P("p1", Faulted{Consumer: c}))
		assert.Equal(t, hA, b.c("p1").Head)
		c2 := b.start("p1", "r2")
		assert.Equal(t, hA, b.c(c2).PriorHead)
		b.take(c2)
		b.no(b.C(c2, Report{Result: "ok", Head: hA}), "prior head")
		b.no(b.C(c2, Report{Result: "fault", Head: hA}), "prior head")
		b.no(b.C(c2, Report{Result: "fail", Head: base}), "is the base")
		b.report(c2, "ok", hB, "")
		b.ok(b.P("p1", Finished{Consumer: c2}))
	})
	t.Run("through ReportPlan and into a held stream", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c := b.start("p1", "r1")
		b.take(c)
		b.hold()
		b.ok(b.reportPlan(c, Report{Result: "fault", Reason: "oom 137"}))
		b.is("p1", Waiting)
		assert.Equal(t, 0, b.c("p1").Bounces)
		b.unhold()
		b.ok(b.P("p1", Resolve{}))
		c = b.start("p1", "r1")
		b.take(c)
		b.ok(b.reportPlan(c, Report{Result: "fail", Reason: "red"}))
		assert.Equal(t, 1, b.c("p1").Bounces)
	})
	t.Run("a read's fault moves nothing and counts nothing", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		b.take("p1.r1.1")
		b.ok(b.reportPlan("p1.r1.1", Report{Result: "fault", Reason: "bench lost"}))
		b.is("p1", Review)
		assert.Equal(t, 0, b.c("p1").Bounces)
		b.no(b.P("p1", ReadYes{}), "said ok")
		b.no(b.P("p1", ReadFix{}), "said fix")
		b.ok(b.P("p1", AskRead{Row: "r2"})) // asked again on the same row
	})
	t.Run("a withdraw, a release and a preempt count nothing", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		for i := 0; i < 3; i++ {
			c := b.start("p1", "r1")
			b.take(c)
			b.ok(b.C(c, Withdraw{Reason: "preempted by blocker x", Head: []string{hA, hB, hD}[i], Preempt: true}))
		}
		assert.Equal(t, 0, b.c("p1").Bounces)
		assert.Equal(t, hD, b.c("p1").Head)
	})
}

// ---- red only on producers, cleared by Finished and Start ----

func TestBreakHeldRed(t *testing.T) {
	t.Parallel()
	t.Run("red, waive, red again: the second waive needs a read of the review before it", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
		b.no(b.P("p1", ReadYes{}), "review 2 that said ok")
		b.ok(b.P("p1", WaiveRed{Reason: "flaky"}))
		b.is("p1", Merging)
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci again"}))
		assert.Equal(t, 3, b.c("p1").Review)
		b.no(b.P("p1", WaiveRed{Reason: "flaky"}), "review 2 at head")
		b.readOf("p1", "r3", "ok")
		b.no(b.P("p1", WaiveRed{Reason: "flaky"}), "review 2 at head") // red stays true, and still no read of review 2
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Landed{Head: hA}))
	})
	t.Run("red then block, unblock, restart: Start clears it", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
		assert.True(t, b.c("p1").Red)
		b.ok(b.P("p1", Block{Reason: "stuck"}))
		assert.True(t, b.c("p1").Red)
		b.ok(b.P("p1", Unblock{Reason: "go"}))
		b.ok(b.P("p1", Resolve{}))
		c := b.run("p1", "r1", "ok", hB)
		assert.False(t, b.c("p1").Red) // Start cleared it
		b.ok(b.P("p1", Finished{Consumer: c}))
		b.no(b.P("p1", WaiveRed{Reason: "x"}), "no red on review")
	})
	t.Run("red then a fix read: ReadFix keeps it, Start clears it", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
		b.readOf("p1", "r3", "fix")
		b.ok(b.P("p1", ReadFix{Finding: "the red is the code"}))
		assert.True(t, b.c("p1").Red)
		b.no(b.P("p1", WaiveRed{Reason: "x"}), "review") // ready: not in review
		b.start("p1", "r1")
		assert.False(t, b.c("p1").Red)
	})
	t.Run("red and waive in one op write the state twice", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
		b.ok(b.P("p1", WaiveRed{Reason: "flaky"}))
		b.noMerge("", b.P("p1", Red{Head: hA, Reason: "ci"}), b.P("p1", Landed{Head: hA})) // Landed after Red: not merging
		b.is("p1", Merging)
	})
	t.Run("a landed producer keeps its red and moves no more", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
		b.ok(b.P("p1", WaiveRed{Reason: "flaky"}))
		b.ok(b.P("p1", Landed{Head: hA}))
		for _, ev := range coldProducerEvents() {
			if b.step(b.P("p1", ev)) {
				t.Fatalf("%s moved a landed card", name(ev))
			}
		}
	})
}

// ---- an Edit never lands while a consumer is live ----

func TestBreakHeldEdit(t *testing.T) {
	t.Parallel()
	t.Run("every state with a live consumer", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"))
		b.ok(b.P("p1", Resolve{}))
		c := b.start("p1", "r1")
		b.no(b.P("p1", Edit{Prose: "new"}), "message r1 instead") // a ready consumer
		b.take(c)
		b.no(b.P("p1", Edit{Prose: "new"}), "message r1 instead") // a working consumer
		b.report(c, "ok", hA, "")
		b.ok(b.P("p1", Edit{Prose: "new"})) // reported, nobody holds it live
		b.ok(b.P("p1", Finished{Consumer: c}))
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		b.no(b.P("p1", Edit{Prose: "newer"}), "message r2 instead")
	})
	t.Run("a plan made in review before a read was cut", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		stale, r := b.w.plan("p1", Edit{Prose: "scope only"})
		require.Nil(t, r)
		b.ok(b.P("p1", AskRead{Row: "r3"}))
		assert.False(t, b.try("stale Edit", stale, nil))
		assert.Contains(t, b.last, "exists")
		assert.Empty(t, b.c("p1").Prose)
	})
	t.Run("a plan made in ready before the start", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		stale, r := b.w.plan("p1", Edit{Prose: "scope only"})
		require.Nil(t, r)
		b.start("p1", "r1")
		assert.False(t, b.try("stale Edit", stale, nil))
		assert.Contains(t, b.last, "gen")
	})
	t.Run("merged with a start, a read, a withdraw", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		b.noMerge("", b.P("p1", Start{Row: "r1", Branch: "b", Base: base}), b.P("p1", Edit{Prose: "x"}))
		b.okMerge(b.P("p1", Edit{Prose: "scope"}), b.P("p1", Start{Row: "r1", Branch: "b", Base: base}))
		assert.Equal(t, "scope", b.c("p1.w1").Prose, "the consumer is cut with the edited prose")
		b.is("p1", Working)
	})
	t.Run("the reviewed head and finished reads survive an edit", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		before := b.c("p1")
		b.ok(b.P("p1", Edit{Prose: "scope only", Paths: []string{"a.go"}, DoneWhen: []string{"tests"}, Reason: "narrowed"}))
		after := b.c("p1")
		assert.Equal(t, []any{before.Head, before.Author, before.Attempt, before.Score, before.Review}, []any{after.Head, after.Author, after.Attempt, after.Score, after.Review})
		b.ok(b.P("p1", ReadYes{}))
	})
}

// ---- needs and after stay acyclic ----

func TestBreakHeldCycles(t *testing.T) {
	t.Parallel()
	t.Run("the loop is named", func(t *testing.T) {
		b := newBk(t, producer("n1"), producer("p1", "n1"), producer("p2", "p1"))
		b.ok(b.P("n1", Cancel{Reason: "dropped"}))
		b.no(b.P("p1", Relink{Need: "n1", To: "p2"}), "p1 needs p2 needs p1")
		b.ok(b.P("p1", Waive{Need: "n1"}))
	})
	t.Run("through an after edge", func(t *testing.T) {
		b := newBk(t, producer("n1"), producer("p1", "n1"))
		b.ok(b.addStep("p3", nil, []string{"p1"}))
		b.ok(b.P("n1", Cancel{Reason: "dropped"}))
		b.no(b.P("p1", Relink{Need: "n1", To: "p3"}), "p1 needs p3 needs p1")
	})
	t.Run("two relinks in one op", func(t *testing.T) {
		b := newBk(t, producer("a"), producer("b"), producer("p1", "a"), producer("p2", "b"))
		b.ok(b.P("a", Cancel{Reason: "x"}))
		b.ok(b.P("b", Cancel{Reason: "x"}))
		b.noMerge("needs", b.P("p1", Relink{Need: "a", To: "p2"}), b.P("p2", Relink{Need: "b", To: "p1"}))
		b.ok(b.P("p1", Relink{Need: "a", To: "p2"}))
		b.no(b.P("p2", Relink{Need: "b", To: "p1"}), "p2 needs p1 needs p2")
	})
	t.Run("through a sentinel", func(t *testing.T) {
		b := newBk(t, producer("n"), producer("p1", "n"))
		b.ok(b.sentinel("g1", []string{"p1"}))
		b.ok(b.P("n", Cancel{Reason: "x"}))
		b.no(b.P("p1", Relink{Need: "n", To: "g1"}), "p1 needs g1 needs p1")
	})
	t.Run("a walk that leaves the cards passed", func(t *testing.T) {
		w := newWorld(t, producer("n"), producer("m"), producer("x", "m"), producer("p1", "n"))
		w.move("n", Cancel{Reason: "x"})
		all, cons := w.all()
		partial := map[string]Card{"n": all["n"], "x": all["x"]} // m, reachable from x, not passed
		_, r := Move(all["p1"], cons["p1"], w.rows(), StreamFromStore(w.snap().Streams["s1"]), partial, Relink{Need: "n", To: "x"})
		has(t, r, "card m, needed by x, was not passed")
	})
	t.Run("a replace does not close a loop either", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2", "p1"))
		b.ok(b.P("p1", Replace{By: "p2", Reason: "p2 does it"}))
		b.no(b.P("p2", Relink{Need: "p1", To: "p2"}), "p2 needs p2") // To is p2 itself
		b.ok(b.P("p2", Waive{Need: "p1", Reason: "replaced by me"}))
	})
}

// ---- a Withdraw of a working producer's live consumer releases it in the same plan ----

func TestBreakHeldWithdraw(t *testing.T) {
	t.Parallel()
	t.Run("ready and working consumers", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"))
		b.ok(b.P("p1", Resolve{}))
		b.ok(b.P("p2", Resolve{}))
		c1 := b.start("p1", "r1")
		c2 := b.start("p2", "r2")
		b.take(c2)
		b.ok(b.C(c1, Withdraw{Reason: "x"}))
		b.ok(b.C(c2, Withdraw{Reason: "y", Head: hA}))
		b.is("p1", Ready)
		b.is("p2", Ready)
		assert.Equal(t, hA, b.c("p2").Head)
		assert.Equal(t, "y", b.c("p2").Reason)
	})
	t.Run("a withdraw planned from a producer that is not working", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c := b.start("p1", "r1")
		b.take(c)
		stale := b.c("p1")
		stale.State = Ready
		_, r := MoveConsumer(b.c(c), stale, b.w.rows()["r1"], Withdraw{Reason: "x", Stream: Stream{Name: "s1"}})
		has(t, r, "withdrawing live work consumer p1.w1 would leave it working; pass the producer in working, so the withdraw releases it")
		_, r = MoveConsumer(b.c(c), b.c("p1"), b.w.rows()["r1"], Withdraw{Reason: "x"})
		has(t, r, `it needs p1's stream s1, not ""`)
		_, r = MoveConsumer(b.c(c), b.c("p1"), b.w.rows()["r1"], Withdraw{Reason: "x", Stream: Stream{Name: "s9"}})
		has(t, r, "it needs p1's stream s1")
	})
	t.Run("a hold that arrives between the plan and the apply", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c := b.start("p1", "r1")
		b.take(c)
		plan := b.w.consumerPlan(c, Withdraw{Reason: "silent"}) // stream read free
		b.hold()
		assert.False(t, b.try("stale withdraw", plan, nil))
		assert.Contains(t, b.last, "stream s1 is held, expected not held")
		b.is(c, Working)
		b.is("p1", Working)
	})
	t.Run("a read withdrawn leaves its producer in review", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		b.take("p1.r1.1")
		b.ok(b.C("p1.r1.1", Withdraw{Reason: "silent"}))
		b.is("p1", Review)
		b.no(b.C("p1.r1.1", Withdraw{Reason: "again"}), "nothing moves a fail card")
	})
	t.Run("a withdrawn consumer's producer cannot also be released or finished", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c := b.start("p1", "r1")
		b.take(c)
		b.ok(b.C(c, Withdraw{Reason: "silent"}))
		b.no(b.P("p1", Released{}), "ready, Released needs working")
		b.no(b.P("p1", Finished{Consumer: c}), "ready, Finished needs working")
	})
}

// ---- a sentinel reaches exactly when every need landed ----

func TestBreakHeldSentinel(t *testing.T) {
	t.Parallel()
	t.Run("one need at a time", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"), producer("p3"))
		b.ok(b.sentinel("g", []string{"p1", "p2", "p3"}))
		b.no(b.P("g", Reached{}), "need p1 of sentinel g is waiting, not landed")
		b.land("p1", hA)
		b.no(b.P("g", Reached{}), "need p2 of sentinel g")
		b.land("p2", hB)
		b.no(b.P("g", Reached{}), "need p3 of sentinel g")
		b.ok(b.P("p3", Resolve{}))
		b.no(b.P("g", Reached{}), "need p3 of sentinel g is ready, not landed")
		b.land2("p3", hD)
		b.ok(b.P("g", Reached{}))
		b.no(b.P("g", Reached{}), "nothing moves a landed card")
	})
	t.Run("a failed need", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"))
		b.ok(b.sentinel("g", []string{"p1", "p2"}))
		b.land("p1", hA)
		b.ok(b.P("p2", Cancel{Reason: "dropped"}))
		b.no(b.P("g", Reached{}), "need p2 of sentinel g is fail, not landed")
		b.ok(b.P("g", Waive{Need: "p2", Reason: "not needed"}))
		b.ok(b.P("g", Reached{}))
	})
	t.Run("the cards passed lie about a need", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"))
		b.ok(b.sentinel("g", []string{"p1", "p2"}))
		b.land("p1", hA)
		all, cons := b.w.all()
		liar := all["p2"]
		liar.State = LandedState
		lie := map[string]Card{"p1": all["p1"], "p2": liar}
		plan, r := Move(all["g"], cons["g"], b.w.rows(), StreamFromStore(b.snap().Streams["s1"]), lie, Reached{})
		require.Nil(t, r)
		assert.False(t, b.try("lying Reached", plan, nil))
		assert.Contains(t, b.last, "need p2 of card g is waiting, not landed")
		b.is("g", Waiting)
	})
	t.Run("a sentinel behind a sentinel, and none at all", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.sentinel("g0", nil))
		b.ok(b.sentinel("g1", []string{"g0", "p1"}))
		b.ok(b.P("g0", Reached{}))
		b.no(b.P("g1", Reached{}), "need p1 of sentinel g1")
		b.land("p1", hA)
		b.ok(b.P("g1", Reached{}))
	})
	t.Run("what a sentinel never takes", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.sentinel("g", []string{"p1"}))
		for _, ev := range []Event{Resolve{}, Start{Row: "r1", Branch: "b", Base: base}, AskRead{Row: "r1"}, Edit{Prose: "x"}, Rescore{Score: 3}, Block{Reason: "x"}} {
			if b.step(b.P("g", ev)) {
				t.Fatalf("%s accepted for a sentinel", name(ev))
			}
		}
		b.ok(b.P("g", SetPriority{Level: "blocker", Reason: "release gate"}))
	})
}

// ---- a producer never lands twice; a card in fail or landed never moves ----

func TestBreakHeldLandsOnce(t *testing.T) {
	t.Parallel()
	t.Run("landed, landed, red", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.land("p1", hA)
		b.no(b.P("p1", Landed{Head: hA}), "nothing moves a landed card")
		b.no(b.P("p1", Red{Head: hA, Reason: "late red"}), "nothing moves a landed card")
		b.no(b.P("p1", Cancel{Reason: "late"}), "nothing moves a landed card")
	})
	t.Run("in one op", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.noMerge("nothing moves a landed card", b.P("p1", Landed{Head: hA}), b.P("p1", Landed{Head: hA}))
		b.noMerge("", b.P("p1", Landed{Head: hA}), b.P("p1", Red{Head: hA, Reason: "x"}))
		b.noMerge("", b.P("p1", Red{Head: hA, Reason: "x"}), b.P("p1", Landed{Head: hA}))
		b.noMerge("", b.P("p1", Landed{Head: hA}), b.P("p1", Cancel{Reason: "x"}))
		b.is("p1", Merging)
	})
	t.Run("two plans from one snapshot", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		one, r := b.w.plan("p1", Landed{Head: hA})
		require.Nil(t, r)
		two, r := b.w.plan("p1", Landed{Head: hA})
		require.Nil(t, r)
		assert.True(t, b.try("first landed", one, nil))
		assert.False(t, b.try("second landed", two, nil))
		assert.Contains(t, b.last, "gen")
	})
	t.Run("wrong heads", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.no(b.P("p1", Landed{Head: hB}), "producer p1 is at head")
		b.no(b.P("p1", Red{Head: hB}), "producer p1 is at head")
		b.no(b.P("p1", Landed{Head: ""}), "producer p1 is at head")
	})
	t.Run("a failed producer: replace by a card failed in the same op", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p9"), producer("p5"))
		b.noMerge("fail", b.P("p9", Cancel{Reason: "x"}), b.P("p5", Replace{By: "p9", Reason: "y"}))
		b.noMerge("fail", b.P("p1", Cancel{Reason: "x"}), b.P("p1", Rescore{Score: 4}))
		b.ok(b.P("p1", Cancel{Reason: "x"}))
		for _, ev := range coldProducerEvents() {
			if b.step(b.P("p1", ev)) {
				t.Fatalf("%s moved a failed card", name(ev))
			}
		}
	})
}

// ---- review counts every review, ReadYes never skips one ----

func TestBreakHeldReviewCount(t *testing.T) {
	t.Parallel()
	t.Run("a new attempt's review needs its own read", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.readOf("p1", "r3", "fix")
		b.ok(b.P("p1", ReadFix{Finding: "case missing"}))
		c := b.run("p1", "r1", "ok", hB)
		b.ok(b.P("p1", Finished{Consumer: c}))
		assert.Equal(t, 2, b.c("p1").Review)
		b.no(b.P("p1", ReadYes{}), "no read of review 2 that said ok")
		b.no(b.P("p1", WaiveRed{Reason: "x"}), "no red on review 2")
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
	})
	t.Run("same head pushed again after a fix", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.readOf("p1", "r3", "fix")
		b.ok(b.P("p1", ReadFix{Finding: "x"}))
		c := b.run("p1", "r1", "fail", hB)
		b.ok(b.P("p1", Broken{Consumer: c}))
		// head hA again: not the prior head (hB), so it is accepted as a push
		c = b.run("p1", "r1", "ok", hA)
		b.ok(b.P("p1", Finished{Consumer: c}))
		b.no(b.P("p1", ReadYes{}), "review 2 that said ok")
	})
	t.Run("a read cut at one review never counts at the next", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.ok(b.P("p1", ReadYes{}))
		b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
		b.no(b.P("p1", ReadYes{}), "review 2 that said ok")
		b.ok(b.P("p1", AskRead{Row: "r2"})) // r2 again: the old read is done, not live
		assert.Equal(t, 2, b.c("p1.r2.1").Review)
	})
	t.Run("the review column cannot overflow", func(t *testing.T) {
		p := pr(Working)
		p.Review = 2147483647
		_, r := Move(p, []Card{wk(Done, "ok", h2)}, rows(), Stream{Name: "s1"}, needs(LandedState), Finished{Consumer: "p1.w1"})
		has(t, r, "producer p1 is at review 2147483647, the most there is")
		// and the card is not stuck: the reported consumer is not live, so Cancel is open
		_, r = Move(p, []Card{wk(Done, "ok", h2)}, rows(), Stream{Name: "s1"}, needs(LandedState), Cancel{Reason: "x"})
		assert.Nil(t, r)
	})
}

// ---- a blocker preempts only a working consumer on its own row ----

func TestBreakHeldPreempt(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *bk {
		b := newBk(t, producer("p2"), producer("p3"), producer("pb"), producer("pr"))
		b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
		for _, id := range []string{"p2", "p3", "pb"} {
			b.ok(b.P(id, Resolve{}))
		}
		b.take(b.start("p2", "r2"))
		b.take(b.start("p3", "r3"))
		return b
	}
	start := func(b *bk, row string) cold4Step { return b.P("pb", Start{Row: row, Branch: "bb", Base: base}) }
	t.Run("own row, working", func(t *testing.T) {
		b := setup(t)
		b.okMerge(b.C("p2.w1", Withdraw{Reason: "preempted by blocker pb", Head: hA, Preempt: true}), start(b, "r2"))
		b.is("p2", Ready)
		assert.Equal(t, []any{hA, 0}, []any{b.c("p2").Head, b.c("p2").Bounces})
		b.take("pb.w1")
		again := b.start("p2", "r2")
		assert.Equal(t, hA, b.c(again).PriorHead) // continues from the preempted push
	})
	t.Run("another row", func(t *testing.T) {
		b := setup(t)
		b.noMerge("blocker pb is dealt to r2", b.C("p3.w1", Withdraw{Reason: "preempted by blocker pb", Preempt: true}), start(b, "r2"))
		b.noMerge("blocker pb is dealt to r2", start(b, "r2"), b.C("p3.w1", Withdraw{Reason: "preempted by blocker pb", Preempt: true}))
		b.is("p3", Working)
	})
	t.Run("a ready consumer is never preempted", func(t *testing.T) {
		b := setup(t)
		b.ok(b.P("pr", Resolve{}))
		cr := b.start("pr", "r2") // ready beside the working one
		b.no(b.C(cr, Withdraw{Reason: "preempted by blocker pb", Preempt: true}), "a preempt withdraws a working consumer")
		b.noMerge("a preempt withdraws a working consumer", b.C(cr, Withdraw{Reason: "preempted by blocker pb", Preempt: true}), start(b, "r2"))
	})
	t.Run("a read on the row is preempted and its producer is asked again", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("pb"))
		b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
		b.review("p1", "r1", hA)
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		b.take("p1.r1.1")
		b.ok(b.P("pb", Resolve{}))
		b.okMerge(b.C("p1.r1.1", Withdraw{Reason: "preempted by blocker pb", Preempt: true}), start(b, "r2"))
		b.is("p1", Review)
		assert.Equal(t, 0, b.c("p1").Bounces)
		b.ok(b.P("p1", AskRead{Row: "r2"}))
	})
}

// ---- a refused op leaves nothing half-applied; Merge equals one by one ----

func TestBreakHeldRefusedLeavesNothing(t *testing.T) {
	t.Parallel()
	t.Run("the first plan of a merge would write before the second fails", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"), producer("p3"))
		b.ok(b.P("p1", Resolve{}))
		b.ok(b.P("p2", Resolve{}))
		c1, c2 := b.start("p1", "r2"), b.start("p2", "r2")
		b.take(c1)
		// Rescore lands first, then Take overflows the width: the store refuses, the score must not move
		b.noMerge("its roster width is 1", b.P("p3", Rescore{Score: 9}), b.C(c2, Take{}))
		assert.Equal(t, 0, b.c("p3").Score)
		b.noMerge("its roster width is 1", cold4R("r3", Hold{Reason: "x"}), b.P("p3", Rescore{Score: 9}), b.C(c2, Take{}))
		assert.False(t, b.snap().Rows["r3"].Held)
	})
	t.Run("a stale hold", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"))
		b.ok(b.P("p1", Resolve{}))
		stale := cold4Hold()
		plan, r := stale.plan(b.w)
		require.Nil(t, r)
		b.ok(b.P("p2", Resolve{})) // a card became ready after the snapshot
		assert.False(t, b.try("stale hold", plan, nil))
		assert.Contains(t, b.last, "expected seq")
		assert.False(t, b.snap().Streams["s1"].Held)
		b.is("p1", Ready)
	})
}

func TestBreakHeldMergeEqualsOneByOne(t *testing.T) {
	t.Parallel()
	dropSetup := func(t *testing.T) *bk {
		b := newBk(t, producer("n1"), producer("m1"), producer("p2", "n1"), producer("p3", "n1"), producer("p4", "n1"), producer("p5", "n1"), producer("s0"))
		return b
	}
	t.Run("a drop with a fail, a waive and a relink", func(t *testing.T) {
		got := sameAsSequence(t, dropSetup, func(b *bk) []cold4Step {
			return []cold4Step{b.P("n1", Cancel{Reason: "dropped"}), b.P("p2", NeedFailed{Need: "n1", InThisOp: true}), b.P("p3", Waive{Need: "n1", Reason: "no need", InThisOp: true}),
				b.P("p4", Relink{Need: "n1", To: "m1", Reason: "m1 does it", InThisOp: true})}
		})
		assert.Equal(t, "", got)
	})
	t.Run("a drop with the dependants first", func(t *testing.T) {
		got := sameAsSequence(t, dropSetup, func(b *bk) []cold4Step {
			return []cold4Step{b.P("p2", NeedFailed{Need: "n1", InThisOp: true}), b.P("n1", Cancel{Reason: "dropped"})}
		})
		t.Logf("dependant before the drop: %q", got) // sequentially the NeedFailed is refused: the need is not yet fail
	})
	t.Run("two takes on two rows, and a width change", func(t *testing.T) {
		setup := func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"))
			b.ok(b.P("p1", Resolve{}))
			b.ok(b.P("p2", Resolve{}))
			b.start("p1", "r2")
			b.start("p2", "r3")
			return b
		}
		assert.Equal(t, "", sameAsSequence(t, setup, func(b *bk) []cold4Step {
			return []cold4Step{b.C("p1.w1", Take{}), b.C("p2.w1", Take{}), cold4R("r1", Hold{Reason: "h"})}
		}))
	})
	t.Run("an edit then a start, a rescore then a priority", func(t *testing.T) {
		setup := func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"))
			b.ok(b.P("p1", Resolve{}))
			return b
		}
		assert.Equal(t, "", sameAsSequence(t, setup, func(b *bk) []cold4Step {
			return []cold4Step{b.P("p1", Edit{Prose: "narrow"}), b.P("p1", Start{Row: "r1", Branch: "b", Base: base}), b.P("p2", Rescore{Score: 3}), b.P("p2", SetPriority{Level: "high", Reason: "ask"})}
		}))
	})
	t.Run("a work report with its producer move", func(t *testing.T) {
		for _, result := range []string{"ok", "fail", "fault"} {
			setup := func() *bk {
				b := newBk(t, producer("p1"))
				b.ok(b.P("p1", Resolve{}))
				b.take(b.start("p1", "r1"))
				return b
			}
			head := hA
			one, two := setup(), setup()
			one.ok(one.reportPlan("p1.w1", Report{Result: result, Head: head, Reason: "why"}))
			two.ok(two.C("p1.w1", Report{Result: result, Head: head, Reason: "why"}))
			follow := map[string]Event{"ok": Finished{Consumer: "p1.w1"}, "fail": Broken{Consumer: "p1.w1"}, "fault": Faulted{Consumer: "p1.w1"}}[result]
			two.ok(two.P("p1", follow))
			assert.Equal(t, "", cold4Diff(one.snap(), two.snap()), result)
		}
	})
	t.Run("two reads in one op against two asks", func(t *testing.T) {
		setup := func(t *testing.T) *bk {
			b := newBk(t, producer("p1"))
			b.review("p1", "r1", hA)
			return b
		}
		assert.Equal(t, "", sameAsSequence(t, setup, func(b *bk) []cold4Step {
			return []cold4Step{b.P("p1", AskReads{Rows: []string{"r2", "r3"}})}
		}))
	})
}
