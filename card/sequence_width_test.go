package card

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- no row over width (working consumers only) ----

func TestSequenceWidth(t *testing.T) {
	t.Parallel()
	t.Run("deal order inside one op", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"))
		b.ok(b.P("p1", Resolve{}))
		b.ok(b.P("p2", Resolve{}))
		c1 := b.start("p1", "r2") // in hand at the deal: the width-1 row is full
		start2 := b.P("p2", Start{Row: "r2", Branch: "b-p2", Base: base})
		b.no(start2, "its roster width is 1")
		// the deal before the report that frees the slot: refused, nothing lands
		b.noMerge("its roster width is 1", start2, b.C(c1, Report{Result: "ok", Head: hA}))
		b.is(c1, Working)
		b.is("p2", Ready)
		// the other order is the legal one
		b.okMerge(b.C(c1, Report{Result: "ok", Head: hA}), start2)
		b.is(c1, Done)
		b.is("p2.w1", Working)
	})
	t.Run("held and down rows", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c := b.start("p1", "r3")
		b.ok(seqR("r3", Hold{Reason: "x"}))
		b.ok(seqR("r3", Gone{})) // going down withdraws nothing
		b.is(c, Working)
		b.ok(seqR("r3", Seen{}))
		b.is(c, Working) // a held row keeps what it was dealt
	})
	t.Run("preempted slot goes to the blocker, the preempted waits at width", func(t *testing.T) {
		b := newBk(t, producer("p2"), producer("pb"))
		b.ok(b.P("p2", Resolve{}))
		c2 := b.start("p2", "r2")
		b.take(c2)
		b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
		b.ok(b.P("pb", Resolve{}))
		b.okMerge(b.C(c2, Withdraw{Reason: "preempted by blocker pb"}), b.P("pb", Start{Row: "r2", Branch: "bb", Base: base}))
		b.is("p2", Ready)
		b.is("pb.w1", Working)
		b.no(b.P("p2", Start{Row: "r2", Branch: "b-p2", Base: base}), "its roster width is 1") // no deal ahead of width
		assert.Equal(t, 0, b.c("p2").Bounces)
	})
}

// ---- no live read outside its producer's review ----

func TestSequenceLiveRead(t *testing.T) {
	t.Parallel()
	t.Run("every way out of review is shut under a live read", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p9"))
		b.review("p1", "r1", hA)
		rid := ""
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		rid = "p1.r1.1"
		for _, ev := range []Event{Cancel{Reason: "x"}, Block{Reason: "x"}, ReadYes{}, ReadFix{Finding: "x"}, WaiveRed{Reason: "x"}, Replace{By: "p9", Reason: "x"}, Landed{Head: hA}, Red{Head: hA, Reason: "x"}} {
			if b.step(b.P("p1", ev)) {
				t.Fatalf("%s accepted under a live read", name(ev))
			}
		}
		b.no(b.P("p1", Edit{Prose: "changed"}), "message r2 instead")
		b.is(rid, Working)
		b.is("p1", Review)
	})
	t.Run("a read cut in an op is live to the next plan of the op", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p9"))
		b.review("p1", "r1", hA)
		for _, ev := range []Event{Cancel{Reason: "x"}, Block{Reason: "x"}, Edit{Prose: "changed"}, Replace{By: "p9", Reason: "x"}} {
			b.noMerge("", b.P("p1", AskRead{Row: "r2"}), b.P("p1", ev))
		}
		b.is("p1", Review)
		assert.Empty(t, liveCards(b.snap(), "p1"))
	})
	t.Run("a read cut after the plan was made refuses the plan", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		rid := b.readOf("p1", "r2", "ok")
		_ = rid
		stale := []Event{ReadYes{}, Cancel{Reason: "x"}, Edit{Prose: "changed"}, Block{Reason: "x"}}
		var plans []Plan
		for _, ev := range stale {
			p, r := b.w.plan("p1", ev)
			require.Nil(t, r, name(ev))
			plans = append(plans, p)
		}
		b.ok(b.P("p1", AskRead{Row: "r3"})) // the live read arrives first
		for i, p := range plans {
			assert.False(t, b.try("stale "+name(stale[i]), p, nil), name(stale[i]))
			assert.Contains(t, b.last, "p1.r1.2 exists", name(stale[i]))
		}
		b.is("p1", Review)
	})
	t.Run("withdrawn and faulted reads leave review open, asked again", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.ok(b.P("p1", AskReads{Rows: []string{"r2", "r3"}}))
		b.ok(b.C("p1.r1.1", Withdraw{Reason: "silent"}))
		b.take("p1.r1.2")
		b.report("p1.r1.2", "fault", "", "bench fault")
		b.no(b.P("p1", ReadYes{}), "no read of review 1 that said ok")
		b.no(b.P("p1", ReadFix{}), "no read of review 1 that said fix")
		b.ok(b.P("p1", AskRead{Row: "r2"})) // same row again: the old read is fail, not live
		b.take("p1.r1.3")
		b.report("p1.r1.3", "fix", hA, "")
		b.ok(b.P("p1", ReadFix{Finding: "f"}))
		assert.Equal(t, 1, b.c("p1").Bounces)
	})
	t.Run("report and verdict in one op", func(t *testing.T) {
		// layer 3 plans the verdict against the report as it will leave the read, as ReportPlan does for work
		verdict := func(b *bk, read string, result string, ev Event) seqStep {
			return seqStep{name(ev) + " p1 after " + result, func(w *world) (Plan, *Refusal) {
				all, cons := w.all()
				var cs []Card
				for _, c := range cons["p1"] {
					if c.ID == read {
						c.State, c.Result, c.Head = Done, result, all["p1"].Head
					}
					cs = append(cs, c)
				}
				return Move(all["p1"], cs, w.rows(), StreamFromStore(w.snap().Streams["s1"]), all, ev)
			}}
		}
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		b.take("p1.r1.1")
		b.okMerge(b.C("p1.r1.1", Report{Result: "ok", Head: hA}), verdict(b, "p1.r1.1", "ok", ReadYes{}))
		b.is("p1", Merging)
		// a fix read cannot be a yes; an ok read cannot be a fix
		b = newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.ok(b.P("p1", AskRead{Row: "r2"}))
		b.take("p1.r1.1")
		b.noMerge("that said ok", b.C("p1.r1.1", Report{Result: "fix", Head: hA}), verdict(b, "p1.r1.1", "fix", ReadYes{}))
		b.noMerge("that said fix", b.C("p1.r1.1", Report{Result: "ok", Head: hA}), verdict(b, "p1.r1.1", "ok", ReadFix{Finding: "f"}))
		b.is("p1", Review)
		b.is("p1.r1.1", Working)
		b2 := newBk(t, producer("p1"))
		b2.review("p1", "r1", hA)
		b2.ok(b2.P("p1", AskRead{Row: "r2"}))
		b2.okMerge(b2.C("p1.r1.1", Withdraw{Reason: "gone"}), seqStep{"Cancel p1 after withdraw", func(w *world) (Plan, *Refusal) {
			all, cons := w.all()
			var cs []Card
			for _, c := range cons["p1"] {
				c.State = Fail
				cs = append(cs, c)
			}
			return Move(all["p1"], cs, w.rows(), StreamFromStore(w.snap().Streams["s1"]), all, Cancel{Reason: "no"})
		}})
		b2.is("p1", Fail)
	})
	t.Run("no read after ReadYes in the same op", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.review("p1", "r1", hA)
		b.readOf("p1", "r2", "ok")
		b.noMerge("merging", b.P("p1", ReadYes{}), b.P("p1", AskRead{Row: "r3"}))
		b.ok(b.P("p1", ReadYes{}))
		b.no(b.P("p1", AskRead{Row: "r3"}), "merging")
	})
}

// ---- a live work consumer only under its producer's working state and attempt ----

func TestSequenceLiveWork(t *testing.T) {
	t.Parallel()
	t.Run("old attempts cannot be revived", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c1 := b.start("p1", "r1")
		b.take(c1)
		b.ok(b.C(c1, Withdraw{Reason: "silent"}))
		b.is("p1", Ready)
		c2 := b.start("p1", "r1")
		assert.Equal(t, "p1.w2", c2)
		for _, s := range []seqStep{b.P("p1", Finished{Consumer: c1}), b.P("p1", Released{}), b.P("p1", Broken{Consumer: c1}), b.C(c1, Report{Result: "fail", Reason: "x"}), b.C(c1, Withdraw{Reason: "x"})} {
			if b.step(s) {
				t.Fatalf("%s accepted for a withdrawn attempt-1 consumer", s.label)
			}
		}
		b.take(c2)
		b.report(c2, "ok", hA, "")
		b.no(b.P("p1", Finished{Consumer: c1}), "not producer p1's work consumer of attempt 2")
		b.ok(b.P("p1", Finished{Consumer: c2}))
	})
	t.Run("dealt consumer withdrawn before a report", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c1 := b.start("p1", "r2")
		b.ok(b.C(c1, Withdraw{Reason: "cancelled deal"}))
		b.is("p1", Ready)
		b.is(c1, Fail)
		c2 := b.start("p1", "r3")
		b.take(c2)
		b.no(b.C(c1, Report{Result: "fail", Reason: "x"}), "nothing moves a fail card")
	})
	t.Run("broken, blocked, unblocked, restarted", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c1 := b.run("p1", "r1", "fail", "")
		b.ok(b.P("p1", Broken{Consumer: c1}))
		b.ok(b.P("p1", Block{Reason: "too many"}))
		b.no(b.P("p1", Start{Row: "r1", Branch: "b", Base: base}), "blocked")
		b.ok(b.P("p1", Unblock{Reason: "go"}))
		b.ok(b.P("p1", Resolve{}))
		c2 := b.start("p1", "r1")
		b.is("p1", Working)
		assert.Equal(t, "p1.w2", c2)
	})
	t.Run("a hold under a working producer: the withdraw parks it", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2", "p1"))
		b.ok(b.P("p1", Resolve{}))
		c1 := b.start("p1", "r1")
		b.take(c1)
		b.hold()
		b.ok(b.C(c1, Withdraw{Reason: "silent"}))
		b.is("p1", Waiting)
		assert.Equal(t, "held: stream s1 is held", b.c("p1").Reason)
		b.no(b.P("p1", Start{Row: "r1", Branch: "b", Base: base}), "held")
		b.unhold()
		b.ok(b.P("p1", Resolve{}))
		b.start("p1", "r1")
	})
	t.Run("the report and the producer's move are one op", func(t *testing.T) {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		c1 := b.start("p1", "r1")
		b.take(c1)
		b.ok(b.reportPlan(c1, Report{Result: "fault", Reason: "bench"}))
		b.is("p1", Ready)
		assert.Equal(t, 0, b.c("p1").Bounces)
	})
}

// ---- no ready producer in a held stream ----

func TestSequenceHeldStream(t *testing.T) {
	t.Parallel()
	t.Run("every return parks", func(t *testing.T) {
		b := newBk(t, producer("p1"), producer("p2"), producer("p3"), producer("p4"))
		for _, id := range []string{"p1", "p2", "p3"} {
			b.ok(b.P(id, Resolve{}))
		}
		c1 := b.run("p1", "r1", "fail", "")  // broken
		c2 := b.run("p2", "r2", "fault", "") // faulted
		cc := b.start("p3", "r3")
		b.take(cc) // released by a withdraw
		b.review("p4", "r1", hB)
		b.readOf("p4", "r2", "fix")
		b.hold()
		b.ok(b.P("p1", Broken{Consumer: c1}))
		b.ok(b.P("p2", Faulted{Consumer: c2}))
		b.ok(b.C(cc, Withdraw{Reason: "silent"}))
		b.ok(b.P("p4", ReadFix{Finding: "f"}))
		for _, id := range []string{"p1", "p2", "p3", "p4"} {
			b.is(id, Waiting)
			assert.Contains(t, b.c(id).Reason, "held: stream s1 is held", id)
		}
		b.no(b.P("p1", Resolve{}), "held")
		b.unhold()
		for _, id := range []string{"p1", "p2", "p3", "p4"} {
			b.ok(b.P(id, Resolve{}))
		}
	})
	t.Run("a resolve and a hold in one op", func(t *testing.T) {
		b := newBk(t, producer("n1"), producer("p1"), producer("p2", "n1"), producer("p3", "n1"))
		b.land("n1", hA)
		b.ok(b.P("p1", Resolve{}))
		// the hold lists only p1 (the snapshot's ready set); merged after the resolve it scans again
		b.okMerge(b.P("p2", Resolve{}), seqHold())
		b.is("p1", Waiting)
		b.is("p2", Waiting)
		assert.Equal(t, "freeze", b.c("p1").Reason)
		assert.Equal(t, "freeze", b.c("p2").Reason)
		assert.True(t, b.snap().Streams["s1"].Held)
		b.unhold()
		// hold first, then resolve: the resolve is refused, it would leave a ready producer in a held stream
		b.noMerge("held", seqHold(), b.P("p3", Resolve{}))
		b.hold()
		b.no(b.P("p3", Resolve{}), "held")
		b.no(b.P("p3", Park{}), "waiting")
		b.no(seqHold(), "already held")
	})
}
