package card

// Findings of the breaker's pass on the card machine at b2b0b8c2 (v2-card-fix4).
// Each test FAILS at that commit and asserts the design line it names. The
// sequences that held are in break_card_held_test.go (TestBreakHeld*) and the
// pair and triple sweeps in break_card_sweep_test.go. Unit tests: no clock, no
// sockets, store.Fake.

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/require"
)

// B1. LAYER-2 "Consumer ids are the machine's: <producer>.w<attempt> for work,
// <producer>.r<review>.<n> for reads, fenced with CardAbsent at cut", and
// "nothing stalls" (a ready producer is started on the next tick). Add accepts
// a producer whose id IS a future consumer id. Sequence: Add p1; Add "p1.w1"
// (accepted); Resolve p1; Start p1: the machine plans it, the store refuses
// "card p1.w1 exists", and so does every retry, so p1 can never start (a read
// id "p1.r1.1" does the same to AskRead and strands p1 in review).
func TestBreakCardAddIdCollidesWithConsumerId(t *testing.T) {
	for _, id := range []string{"p1.w1", "p1.r1.1"} {
		w := newWorld(t, producer("p1"))
		all, _ := w.all()
		_, r := MoveAdd(Add{ID: id, Kind: "producer", Stream: "s1", Tier: "pro", Brief: "x"}, all)
		if r == nil && id != "p1.w1" {
			t.Errorf("Add accepted id %q, the id of p1's first read of review 1; AskRead of p1 will be refused by the store forever", id)
			continue
		}
		if r == nil {
			w.raw("add", store.PutCard{ID: id, Kind: "producer", Stream: "s1", Tier: "pro", Brief: "x"})
			w.move("p1", Resolve{})
			plan, mr := w.plan("p1", Start{Row: "r1", Branch: "b", Base: base})
			if mr == nil {
				rc, err := w.apply("start", plan)
				require.NoError(t, err)
				t.Errorf("Add accepted id %q; Start of p1 is planned and the store refuses it forever: %q", id, rc.Reason)
			}
			continue
		}
		require.Contains(t, r.Reason, "consumer")
	}
}

// B2. LAYER-2 Waive/NeedFailed/Relink rows: "need is in p's needs and is fail,
// or is failed by an earlier write of the same op", and "a merged op equals
// applying its plans one by one". Merge(Rescore p3, NeedFailed p2 InThisOp)
// has no drop in it; one by one the NeedFailed is refused ("need n1 is
// waiting, not fail"), merged it is accepted and p2 fails over a waiting need.
func TestBreakCardMergeInThisOpWithoutADrop(t *testing.T) {
	w := newWorld(t, producer("n1"), producer("p2", "n1"), producer("p3"))
	a, r := w.plan("p3", Rescore{Score: 4})
	require.Nil(t, r)
	b, r := w.plan("p2", NeedFailed{Need: "n1", InThisOp: true})
	require.Nil(t, r)
	_, mr := Merge(a, b)
	if mr == nil {
		t.Errorf("Merge accepted NeedFailed{InThisOp} with no earlier write that fails n1 (n1 is %s)", w.card("n1").State)
	}
	// the same for Waive: a healthy waiting need dropped from a producer
	c, r := w.plan("p2", Waive{Need: "n1", InThisOp: true})
	require.Nil(t, r)
	if _, mr := Merge(a, c); mr == nil {
		t.Errorf("Merge accepted Waive{InThisOp} on a waiting, unfailed need")
	}
}

// B3. LAYER-2 "A Withdraw whose reason is preempt refuses unless the consumer
// is working"; an ordinary withdraw has no such rule. The merge guard
// (replay.go preemptGuard) applies to every Withdraw merged with a blocker's
// Start, whatever its reason, so a silence withdraw on another row and a
// blocker's start, each legal one by one, are refused, with a text about
// preemption though nothing preempts.
func TestBreakCardBlockerStartWithUnrelatedWithdraw(t *testing.T) {
	b := newBk(t, producer("p2"), producer("pb"))
	b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
	b.ok(b.P("p2", Resolve{}))
	b.ok(b.P("pb", Resolve{}))
	b.take(b.start("p2", "r2"))
	if !b.merge(b.C("p2.w1", Withdraw{Reason: "silent for 20 minutes"}), b.P("pb", Start{Row: "r1", Branch: "bb", Base: base})) {
		t.Errorf("merge refused: %s", b.last)
	}
}

// B4. LAYER-2 "Merge: sequential composition ... nested merges compose".
// ReportPlan is a nested Merge whose seen records are the post-fold state, so
// Merge(P, ReportPlan) with P not having read the reporting cards re-runs the
// Report against a consumer already done: "consumer p1.w1 is done; Report
// moves a done card". Merge(ReportPlan, P) is accepted. The two commute.
func TestBreakCardNestedMergeIsOrderDependent(t *testing.T) {
	setup := func() *bk {
		b := newBk(t, producer("p1"))
		b.ok(b.P("p1", Resolve{}))
		b.take(b.start("p1", "r1"))
		return b
	}
	for _, e := range []Event{Hold{Reason: "x"}, Width{N: 3}, Gone{}} {
		rowStep := cold4R("r2", e)
		first := setup()
		okFirst := first.merge(first.reportPlan("p1.w1", Report{Result: "ok", Head: hA}), rowStep)
		second := setup()
		okSecond := second.merge(rowStep, second.reportPlan("p1.w1", Report{Result: "ok", Head: hA}))
		if okFirst && !okSecond {
			t.Errorf("%s: Merge(ReportPlan, row) accepted, Merge(row, ReportPlan) refused: %s", name(e), second.last)
		}
	}
}

// B5. LAYER-2 "An event naming a ... card ... that does not exist: a refusal
// naming the unknown name" and "refusals name the fix". A card that exists but
// was not passed is reported as not existing: Relink to a real producer
// missing from needs says "card x does not exist", so the caller hunts a
// typo. Replace's text already says "among the cards passed".
func TestBreakCardUnpassedCardIsNotMissing(t *testing.T) {
	w := newWorld(t, producer("n"), producer("x"), producer("p1", "n"))
	w.move("n", Cancel{Reason: "dropped"})
	all, cons := w.all()
	partial := map[string]Card{"n": all["n"]}
	_, r := Move(all["p1"], cons["p1"], w.rows(), StreamFromStore(w.snap().Streams["s1"]), partial, Relink{Need: "n", To: "x"})
	require.NotNil(t, r)
	if !containsAny(r.Reason, "passed", "pass ") {
		t.Errorf("x exists in the store; refusal says %q and does not say it was not passed", r.Reason)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}
