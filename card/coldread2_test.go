package card

// The second cold read of layer 2, folded from
// /Users/glenn/rowan-working/children/nsv2-l2-coldread2/card/coldread2_test.go
// (the read at c3fac873). Assertions the rulings reversed — two AskReads
// merged, Relink without InThisOp, a sentinel need, Replace by a consumer,
// a withdraw that would strand, a withdraw head equal to the base — now
// expect the ruled behaviour. The walk takes AskReads, UnholdStream and
// WaiveRed. No clock, no sockets.

import (
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// review puts p1 in review with head h1 written by r1.
func cold2Review(t *testing.T) *world {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	return w
}

// E1. AskReads cuts several reads from one snapshot with distinct indices.
// Two AskReads planned apart and merged write one consumer id twice; Merge
// refuses that, and the plan is not applied. The original probe applied the
// merge and saw store.ErrInvalid.
func TestCold2DefectTwoReadsInOneTick(t *testing.T) {
	w := cold2Review(t)
	a, r := w.plan("p1", AskRead{Row: "r2"})
	require.Nil(t, r)
	b, r := w.plan("p1", AskRead{Row: "r3"})
	require.Nil(t, r)
	_, r = Merge(a, b)
	refusal{r, "merge of AskRead p1 and AskRead p1 refused: a second write to card p1.r1.1 field kind; one op writes a field once; several reads of one head are AskReads"}.check(t)
	_, ok := w.snap().Cards["p1.r1.1"]
	assert.False(t, ok, "the refused merge was applied")
	_, r = w.plan("p1", AskReads{Rows: []string{"r2", "r2"}})
	refusal{r, "row r2 is named twice in AskReads of producer p1; name each row once"}.check(t)
	plan, r := w.plan("p1", AskReads{Rows: []string{"r2", "r3"}})
	require.Nil(t, r)
	w.op("askreads", plan)
	assert.Equal(t, Ready, w.card("p1.r1.1").State)
	assert.Equal(t, Ready, w.card("p1.r1.2").State)
}

// E2. HoldStream with a ready producer named twice emits two state writes to
// one field: ErrInvalid, against "no plan it emits is ErrInvalid".
func TestCold2DefectHoldStreamNamesAProducerTwice(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	p := w.card("p1")
	_, r := MoveStream(StreamFromStore(w.snap().Streams["s1"]), HoldStream{Reason: "release", Ready: []Card{p, p}, Seq: w.snap().Seq})
	refusal{r, "producer p1 is listed twice in HoldStream; name each ready producer once"}.check(t)
	assert.Equal(t, Ready, w.card("p1").State)
	assert.False(t, w.snap().Streams["s1"].Held)
}

// E3. "The drop that fails a need may instead name, per dependant, a Waive
// or a Relink to a replacement, applied in the same op" (Rules), and Relink
// is "as Waive". Waive has InThisOp; Relink has none and always fences the
// need fail, so a relink can never ride in the drop's op: the dependant must
// be failed first and revived, or wait a tick on a need that is fail.
func TestCold2DefectRelinkInTheDropsOp(t *testing.T) {
	w := newWorld(t, producer("n1"), producer("x"), producer("p3", "n1"))
	drop, r := w.plan("n1", Cancel{Reason: "dropped, x replaces it"})
	require.Nil(t, r)
	relink, r := w.plan("p3", Relink{Need: "n1", To: "x", Reason: "x replaces n1", InThisOp: true})
	require.Nil(t, r)
	w.op("drop", mustMerge(t, drop, relink))
	assert.Equal(t, []string{"x"}, w.card("p3").Needs)
}

// E4, as round 4 ruled it. A broken or faulted attempt writes its reason to
// the producer's reason and never to finding. Finding is a reader's, and
// Start copies that finding, not the reason. The failure the work reported
// does not ride into the next attempt.
func TestCold2DefectBrokenReasonNeverReachesTheNextAttempt(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Broken{Consumer: w.work("p1", "r1", "fail", h1, "TestFoo panics on nil map")})
	assert.Equal(t, "TestFoo panics on nil map", w.card("p1").Reason)
	assert.Empty(t, w.card("p1").Finding, "a fault's or a broken attempt's reason stays off finding")
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
	c := w.card("p1.w2")
	assert.Equal(t, h1, c.PriorHead)
	assert.Empty(t, c.Reason, "Start copies no reason")
	assert.Empty(t, c.Finding, "the next attempt carries a reader's finding, not the broken reason")
}

// E5. A producer parked in a held stream carries "reason naming the hold"
// (Park row; reversed witness "a producer parked with no named hold"). The
// moves that return into a held stream (Broken, Faulted, ReadFix, Released,
// the Withdraw release) and Unblock write waiting with the consumer's or the
// event's reason, or none at all, never the hold.
func TestCold2DefectParkedWithoutNamingTheHold(t *testing.T) {
	hold := func(w *world) { w.raw("hold", store.SetStreamHeld{Stream: "s1", Held: true}) }
	cases := map[string]func(w *world){
		"Broken": func(w *world) {
			c := w.work("p1", "r1", "fail", h1, "tests red")
			hold(w)
			w.move("p1", Broken{Consumer: c})
		},
		"Withdraw": func(w *world) {
			w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
			hold(w)
			w.consume("p1.w1", Withdraw{Reason: "silent 20 min"})
		},
		"ReadFix": func(w *world) {
			w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
			w.read("p1", "r2", "fix", "rename x")
			hold(w)
			w.move("p1", ReadFix{Finding: "rename x"})
		},
	}
	for label, run := range cases {
		t.Run(label, func(t *testing.T) {
			w := newWorld(t, producer("p1"))
			w.move("p1", Resolve{})
			run(w)
			w.is("p1", Waiting)
			assert.Contains(t, w.card("p1").Reason, "held", "parked in held stream s1 with reason %q", w.card("p1").Reason)
		})
	}
}

// E6. Add accepts any id up to 128 bytes, but Start refuses an id with no
// room for ".w<attempt>": the producer is ready forever ("ready lasts one
// tick"; LESSONS 2.21 nothing stalls). Reads stall the same way later, when
// ".r<review>.<n>" grows a digit.
func TestCold2DefectAddedIdTooLongToStart(t *testing.T) {
	id := strings.Repeat("p", store.MaxName-1)
	_, r := MoveAdd(Add{ID: id, Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do it"}, map[string]Card{})
	if assert.NotNil(t, r) {
		assert.Contains(t, r.Reason, "104")
	}
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.is("p1", Working)
}

// ---- suspects: pass at c3fac873, each a behaviour to rule on ----

// S1. Add refuses a sentinel as a need ("a need is a producer"), but the
// design's sentinel is "a phase mark", and the Add ruling refuses only "a
// need that is not a card". A card of phase 2 cannot need the phase-1 mark;
// it must list every card the mark lists.
func TestCold2SuspectSentinelIsNeverANeed(t *testing.T) {
	cards := map[string]Card{"g1": {ID: "g1", Kind: "sentinel", State: Waiting}}
	_, r := MoveAdd(Add{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "b", Needs: []string{"g1"}}, cards)
	assert.Nil(t, r, "a sentinel may be a need")
}

// S2. Replace{by} names "an existing card other than the producer"; a
// consumer (even the producer's own) and a failed card are accepted as the
// replacement, and `by` is not fenced.
func TestCold2SuspectReplaceByAConsumer(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Faulted{Consumer: w.work("p1", "r1", "fault", "", "bench")})
	_, r := w.plan("p1", Replace{By: "p1.w1", Reason: "x"})
	refusal{r, "card p1.w1 is a work consumer; Replace needs a producer or a sentinel that is not final"}.check(t)
}

// S3. MoveConsumer fences the producer only when the withdraw releases it.
// A live work consumer is always its producer's consumer of the current
// attempt, so a withdraw that does not release is only possible from a
// producer record that is not the one the store holds; the plan then lands
// and strands the producer in working with its consumer fail.
func TestCold2SuspectWithdrawFromAStaleProducerStrands(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	stale := w.card("p1") // ready, attempt 0
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	_, r := MoveConsumer(w.card("p1.w1"), stale, w.rows()["r1"], Withdraw{Reason: "silent", Stream: Stream{Name: "s1"}})
	refusal{r, "producer p1 is ready; withdrawing live work consumer p1.w1 would leave it working; pass the producer in working, so the withdraw releases it"}.check(t)
	assert.Equal(t, []string{Working, Ready}, []string{w.card("p1").State, w.card("p1.w1").State})
}

// S4. NeedFailed{InThisOp} alone asserts nothing about the need: applied
// without the drop it fails a dependant whose need is merely waiting.
func TestCold2SuspectNeedFailedInThisOpAlone(t *testing.T) {
	w := newWorld(t, producer("n1"), producer("p3", "n1"))
	plan, r := w.plan("p3", NeedFailed{Need: "n1", InThisOp: true})
	require.Nil(t, r)
	w.op("needfailed", plan)
	assert.Equal(t, []string{Fail, Waiting}, []string{w.card("p3").State, w.card("n1").State})
}

// S5. Withdraw{Head} accepts the base itself as the pushed head; it becomes
// the producer's head and the next attempt's prior_head.
func TestCold2SuspectWithdrawHeadIsTheBase(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.consume("p1.w1", Take{Row: "r1"})
	c := w.card("p1.w1")
	_, r := MoveConsumer(c, w.card("p1"), w.rows()["r1"], Withdraw{Reason: "silent", Head: base, Stream: StreamFromStore(w.snap().Streams["s1"])})
	refusal{r, "head " + base + " is the base; nothing was done"}.check(t)
	assert.Equal(t, []string{Working, Working}, []string{w.card("p1").State, w.card("p1.w1").State})
	assert.Empty(t, w.card("p1").Head)
}

// ---- rulings, each from the right and the wrong state and actor (pass) ----

func TestCold2Rulings(t *testing.T) {
	t.Run("work reports ok fail fault, read ok fix fault", func(t *testing.T) {
		for _, res := range []string{"ok", "fail", "fault"} {
			_, r := MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Report{Row: "r1", Result: res, Head: h2})
			assert.Nil(t, r, res)
		}
		_, r := MoveConsumer(rd(Working, ""), pr(Review), rows()["r1"], Report{Row: "r1", Result: "fail", Head: h1})
		refusal{r, "a read consumer reports ok, fix or fault; fail is a work's result"}.check(t)
	})
	t.Run("wrong actor", func(t *testing.T) {
		_, r := MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r2"], Report{Row: "r2", Result: "ok", Head: h2})
		refusal{r, "consumer p1.w1 is held by r1, not r2"}.check(t)
		_, r = MoveConsumer(wk(Ready, "", ""), pr(Working), rows()["r2"], Take{Row: "r2"})
		refusal{r, "consumer p1.w1 is held by r1, not r2"}.check(t)
		refusal{mv(Review, nil, AskRead{Row: "r2"}), "r2 wrote producer p1's head and may not read it"}.check(t)
	})
	t.Run("working needs its consumer passed", func(t *testing.T) {
		for _, e := range []Event{Cancel{Reason: "x"}, Edit{Prose: "x"}, Block{Reason: "x"}} {
			r := mv(Working, nil, e)
			if assert.NotNil(t, r, name(e)) {
				assert.Contains(t, r.Reason, "was not passed", name(e))
			}
		}
	})
	t.Run("block from working with a reported consumer", func(t *testing.T) {
		for res, apply := range map[string]string{"ok": "Finished", "fail": "Broken", "fault": "Faulted"} {
			refusal{mv(Working, []Card{wk(Done, res, h2)}, Block{Reason: "x"}), "consumer p1.w1 reported " + res + "; apply it with " + apply + " before Block"}.check(t)
		}
		assert.Nil(t, mv(Working, []Card{wk(Fail, "", "")}, Block{Reason: "x"}))
	})
}

func TestCold2RulingsReplaceAndReads(t *testing.T) {
	t.Run("replace by self and unknown", func(t *testing.T) {
		refusal{mv(Ready, nil, Replace{By: "p1"}), "producer p1 cannot be replaced by itself"}.check(t)
		refusal{mv(Ready, nil, Replace{By: "zz"}), "card zz does not exist (Replace takes the card it is replaced by among the cards passed)"}.check(t)
	})
	t.Run("sentinel gets no consumer, edit or score", func(t *testing.T) {
		g := Card{ID: "g1", Kind: "sentinel", Stream: "s1", State: Ready, Tier: "pro", Priority: "normal"}
		for _, c := range []struct {
			e    Event
			want string
		}{{Start{Row: "r1", Branch: "b", Base: base}, "sentinel g1 never has a consumer"}, {Edit{Prose: "x"}, "sentinel g1 has no brief or prose to edit"}, {Rescore{Score: 3}, "sentinel g1 has no score of its own"}} {
			_, r := Move(g, nil, rows(), Stream{Name: "s1"}, nil, c.e)
			refusal{r, c.want}.check(t)
		}
		g.State = Review
		_, r := Move(g, nil, rows(), Stream{Name: "s1"}, nil, AskRead{Row: "r1"})
		refusal{r, "sentinel g1 never has a consumer"}.check(t)
	})
	t.Run("take on down and held rows", func(t *testing.T) {
		c := wk(Ready, "", "")
		c.Holder = "r3"
		_, r := MoveConsumer(c, pr(Working), rows()["r3"], Take{Row: "r3"})
		refusal{r, "row r3 is down; it cannot take"}.check(t)
		c.Holder = "r4"
		_, r = MoveConsumer(c, pr(Working), rows()["r4"], Take{Row: "r4"})
		assert.Nil(t, r, "a held row takes what it was dealt")
	})
	t.Run("readfix on zero fix reads and an old review", func(t *testing.T) {
		refusal{mv(Review, nil, ReadFix{Finding: "x"}), "producer p1 has no read of review 1 that said fix; ReadFix needs one"}.check(t)
		old := rd(Done, "fix")
		old.Review = 0
		refusal{mv(Review, []Card{old}, ReadFix{Finding: "x"}), "producer p1 has no read of review 1 that said fix; ReadFix needs one"}.check(t)
		other := rd(Done, "fix")
		other.Head = h2
		refusal{mv(Review, []Card{other}, ReadFix{Finding: "x"}), "producer p1 has no read of review 1 that said fix; ReadFix needs one"}.check(t)
	})
}

func TestCold2RulingsValues(t *testing.T) {
	t.Run("values refused before a plan", func(t *testing.T) {
		for _, c := range []struct {
			e    Event
			want string
		}{
			{Cancel{Reason: "a\x00b"}, `reason "a\x00b" is not valid UTF-8 or holds a NUL byte`},
			{Edit{Prose: "\xff"}, `prose "\xff" is not valid UTF-8 or holds a NUL byte`},
			{SetPriority{Level: "urgent"}, "priority urgent is not one of low, normal, high, critical or blocker"},
			{Rescore{Score: -1}, "score -1 is below 0"},
			{Edit{Paths: []string{"a\x00"}}, `path "a\x00" is not valid UTF-8 or holds a NUL byte`},
			{Block{Reason: string([]byte{0})}, `reason "\x00" is not valid UTF-8 or holds a NUL byte`},
		} {
			refusal{mv(Ready, nil, c.e), c.want}.check(t)
		}
		_, r := MoveAdd(Add{ID: strings.Repeat("x", 129), Kind: "producer", Stream: "s1", Tier: "pro"}, nil)
		assert.NotNil(t, r)
		_, r = MoveAdd(Add{ID: "p1", Kind: "producer", Stream: "s1", Tier: "ultra"}, nil)
		assert.NotNil(t, r)
		_, r = MoveAdd(Add{ID: "p1", Kind: "producer", Stream: "", Tier: "pro"}, nil)
		assert.NotNil(t, r)
	})
	t.Run("add cycle and kinds", func(t *testing.T) {
		cards := map[string]Card{"a": {ID: "a", Kind: "producer", Needs: []string{"b"}}, "b": {ID: "b", Kind: "producer"}, "a.w1": {ID: "a.w1", Kind: "work", Of: "a"}}
		_, r := MoveAdd(Add{ID: "n", Kind: "producer", Stream: "s1", Tier: "pro", Needs: []string{"a.w1"}}, cards)
		refusal{r, "need a.w1 of producer n is a work; a need is a producer or a sentinel"}.check(t)
		_, r = MoveAdd(Add{ID: "n", Kind: "producer", Stream: "s1", Tier: "pro", Needs: []string{"a"}}, map[string]Card{"a": cards["a"]})
		refusal{r, "card b, reached from a, was not passed; Add needs every card reachable through needs and after"}.check(t)
		_, r = MoveAdd(Add{ID: "n", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: []string{"a"}}, cards)
		assert.Nil(t, r)
	})
}

// The preemption op: withdraw the youngest working consumer on a row and
// deal the blocker to it, as one op, on the Fake; the withdrawn producer goes
// back to ready with its head kept, no bounce.
func TestCold2PreemptionMerge(t *testing.T) {
	w := newWorld(t, producer("p1"), producer("b1"))
	w.move("p1", Resolve{})
	w.move("b1", Resolve{})
	w.move("b1", SetPriority{Level: "blocker", Reason: "asked"})
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
	w.consume("p1.w1", Take{Row: "r2"})
	withdraw := w.consumerPlan("p1.w1", Withdraw{Reason: "preempted by blocker b1", Head: h2})
	start, r := w.plan("b1", Start{Row: "r2", Branch: "b", Base: base})
	require.Nil(t, r)
	w.op("preempt", mustMerge(t, withdraw, start))
	w.is("p1", Ready)
	assert.Equal(t, []any{h2, 0}, []any{w.card("p1").Head, w.card("p1").Bounces})
	w.consume("b1.w1", Take{Row: "r2"})
}

// ---- the random walk, extended ----

func TestCold2Walk(t *testing.T) {
	seen := map[string]int{}
	var mu sync.Mutex
	t.Run("seeds", func(t *testing.T) {
		for seed := int64(1); seed <= 120; seed++ {
			t.Run(fmt.Sprint(seed), func(t *testing.T) {
				t.Parallel()
				got := cold2Walk(t, seed, 900)
				mu.Lock()
				defer mu.Unlock()
				for k, n := range got {
					seen[k] += n
				}
			})
		}
	})
	for _, e := range []string{"Add", "HoldStream", "UnholdStream", "Drop+NeedFailed", "Drop+Waive", "Preempt", "Withdraw", "Relink", "Replace", "Finished", "ReadYes", "ReadFix", "Landed", "Reached", "AskReads", "WaiveRed"} {
		assert.Positive(t, seen[e], "the walk never accepted %s", e)
	}
	t.Logf("accepted: %v", seen)
}

func cold2Walk(t *testing.T, seed int64, steps int) map[string]int {
	rng := rand.New(rand.NewSource(seed))
	w := newWorld(t, producer("p1"), producer("p2", "p1"), producer("p3"), store.PutCard{ID: "g1", Kind: "sentinel", Needs: []string{"p2"}})
	finals := map[string]coldFinal{}
	accepted := map[string]int{}
	for i := 0; i < steps; i++ {
		plan, label, subject, ok := w.cold2Step(rng)
		if !ok {
			continue
		}
		if subject != "" {
			coldFences(t, plan, subject, label)
		}
		rc, err := w.apply(label, plan)
		require.NoError(t, err, "seed %d step %d %s: the store found the plan invalid: %+v", seed, i, label, plan)
		if !rc.Accepted {
			assert.Contains(t, rc.Reason, "width", "seed %d step %d %s: the store refused the machine's plan: %s", seed, i, label, rc.Reason)
			continue
		}
		accepted[strings.Fields(label)[0]]++
		w.landed(plan)
		where := fmt.Sprintf("seed %d step %d %s", seed, i, label)
		s := w.snap()
		coldInvariants(t, s, finals, where)
		cold2Invariants(t, s, where)
	}
	return accepted
}

// cold2Invariants: no producer stranded in working (its consumer of this
// attempt is live or reported, never withdrawn); no producer ready in a held
// stream; a producer past waiting has every need landed.
func cold2Invariants(t *testing.T, s store.Snapshot, where string) {
	t.Helper()
	for _, p := range s.Cards {
		if consumer(p.Kind) {
			continue
		}
		if p.State == Working {
			assert.NotEqual(t, Fail, s.Cards[workID(p.ID, p.Attempt)].State, "%s: %s stranded in working with its consumer withdrawn", where, p.ID)
		}
		if p.State == Ready {
			assert.False(t, s.Streams[p.Stream].Held, "%s: %s ready in held stream %s", where, p.ID, p.Stream)
		}
		if slices.Contains([]string{Ready, Working, Review, Merging}, p.State) {
			for _, n := range p.Needs {
				assert.Equal(t, LandedState, s.Cards[n].State, "%s: %s is %s with need %s %s", where, p.ID, p.State, n, s.Cards[n].State)
			}
		}
	}
}

// cold2Step is coldStep, except a hold is the machine's HoldStream, fenced at
// the snapshot seq, and a hold of a stream already held is UnholdStream.
func (w *world) cold2Step(rng *rand.Rand) (Plan, string, string, bool) {
	if rng.Intn(4) == 0 {
		return w.cold2New(rng)
	}
	plan, label, subject, ok := w.coldStep(rng)
	if label != "HoldStream s1" {
		return plan, label, subject, ok
	}
	s := w.snap()
	if s.Streams["s1"].Held {
		plan, r := MoveStream(StreamFromStore(s.Streams["s1"]), UnholdStream{Reason: "release"})
		return plan, "UnholdStream s1", "", r == nil
	}
	return w.cold2Hold()
}

func (w *world) cold2Hold() (Plan, string, string, bool) {
	s := w.snap()
	var ready []Card
	for _, c := range s.Cards {
		if c.Kind == "producer" && c.State == Ready && c.Stream == "s1" {
			ready = append(ready, FromStore(c))
		}
	}
	plan, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{
		Reason: "stream s1 is held for a release", Ready: ready, Seq: s.Seq,
	})
	return plan, "HoldStream s1", "", r == nil
}

type cold2Set struct{ prods, sents, working []string }

func cold2IDs(all map[string]Card) cold2Set {
	var ids cold2Set
	for id, c := range all {
		switch {
		case c.Kind == "producer":
			ids.prods = append(ids.prods, id)
		case c.Kind == "sentinel":
			ids.sents = append(ids.sents, id)
		case c.State == Working:
			ids.working = append(ids.working, id)
		}
	}
	sort.Strings(ids.prods)
	sort.Strings(ids.sents)
	sort.Strings(ids.working)
	return ids
}

func (w *world) cold2New(rng *rand.Rand) (Plan, string, string, bool) {
	all, cons := w.all()
	ids := cold2IDs(all)
	if plan, label, subject, ok, used := w.cold2WaiveRed(rng, all, cons, ids.prods); used {
		return plan, label, subject, ok
	}
	if plan, label, subject, ok, used := w.cold2Ask(rng, all, cons, ids.prods); used {
		return plan, label, subject, ok
	}
	return w.cold2Case(rng, all, cons, ids)
}

func (w *world) cold2Case(rng *rand.Rand, all map[string]Card, cons map[string][]Card, ids cold2Set) (Plan, string, string, bool) {
	k := rng.Intn(12)
	if k >= 6 && len(ids.working) > 0 {
		k = 2 + rng.Intn(2) // preempt or withdraw while there is work in flight
	}
	if (k == 0 && len(all) > 30) || ((k == 1 || k == 4) && rng.Intn(5) > 0) {
		return Plan{}, "", "", false // keep the world small and alive, so the walk gets deep
	}
	switch k {
	case 0:
		return w.cold2Add(rng, all, ids.prods)
	case 1:
		return w.cold2Drop(rng, all, cons, ids.prods)
	case 2:
		return w.cold2Preempt(rng, all, cons, ids)
	case 3:
		return w.cold2Withdraw(rng, all, ids.working)
	case 4:
		return w.cold2Relink(rng, all, cons, ids)
	default:
		return w.cold2Reached(rng, all, ids.sents)
	}
}

func (w *world) cold2Add(rng *rand.Rand, all map[string]Card, prods []string) (Plan, string, string, bool) {
	e := Add{ID: fmt.Sprintf("a%d", len(all)), Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do it", Score: rng.Intn(3)}
	if rng.Intn(4) == 0 {
		e.Kind, e.Brief, e.Score = "sentinel", "", 0
	}
	for _, id := range prods {
		if rng.Intn(4) == 0 {
			e.Needs = append(e.Needs, id)
		}
		if rng.Intn(6) == 0 {
			e.After = append(e.After, id)
		}
	}
	plan, r := MoveAdd(e, all)
	return plan, "Add " + e.ID, "", r == nil
}

func (w *world) cold2Drop(rng *rand.Rand, all map[string]Card, cons map[string][]Card, prods []string) (Plan, string, string, bool) {
	dep := all[prods[rng.Intn(len(prods))]]
	if len(dep.Needs) == 0 {
		return Plan{}, "", "", false
	}
	n := dep.Needs[rng.Intn(len(dep.Needs))]
	stream := StreamFromStore(w.snap().Streams["s1"])
	drop, r := Move(all[n], cons[n], w.rows(), stream, all, Cancel{Reason: "dropped"})
	if r != nil {
		return Plan{}, "", "", false
	}
	e, label := Event(NeedFailed{Need: n, InThisOp: true}), "Drop+NeedFailed "
	if rng.Intn(2) == 0 {
		e, label = Waive{Need: n, Reason: "not needed", InThisOp: true}, "Drop+Waive "
	}
	plan, r := Move(dep, cons[dep.ID], w.rows(), stream, all, e)
	if r != nil {
		return Plan{}, "", "", false
	}
	merged, mr := Merge(drop, plan)
	return merged, label + dep.ID, dep.ID, mr == nil
}

func (w *world) cold2Preempt(rng *rand.Rand, all map[string]Card, cons map[string][]Card, ids cold2Set) (Plan, string, string, bool) {
	if len(ids.working) == 0 {
		return Plan{}, "", "", false
	}
	c := all[ids.working[rng.Intn(len(ids.working))]]
	b := all[ids.prods[rng.Intn(len(ids.prods))]]
	if b.State != Ready || b.ID == c.Of {
		return Plan{}, "", "", false
	}
	stream := StreamFromStore(w.snap().Streams["s1"])
	head := ""
	if rng.Intn(2) == 0 {
		head = coldH3
	}
	wd, r := MoveConsumer(c, all[c.Of], w.rows()[c.Holder], Withdraw{Reason: "preempted by blocker " + b.ID, Head: head, Stream: stream})
	if r != nil {
		return Plan{}, "", "", false
	}
	st, r := Move(b, cons[b.ID], w.rows(), stream, all, Start{Row: c.Holder, Branch: "b-" + b.ID, Base: base})
	if r != nil {
		return Plan{}, "", "", false
	}
	merged, mr := Merge(wd, st)
	return merged, "Preempt " + c.ID, c.ID, mr == nil
}

func (w *world) cold2Withdraw(rng *rand.Rand, all map[string]Card, working []string) (Plan, string, string, bool) {
	if len(working) == 0 {
		return Plan{}, "", "", false
	}
	c := all[working[rng.Intn(len(working))]]
	heads := []string{"", h2, coldH3, c.Head}
	stream := StreamFromStore(w.snap().Streams["s1"])
	plan, r := MoveConsumer(c, all[c.Of], w.rows()[c.Holder], Withdraw{Reason: "silent", Head: heads[rng.Intn(len(heads))], Stream: stream})
	return plan, "Withdraw " + c.ID, c.ID, r == nil
}

func (w *world) cold2Relink(rng *rand.Rand, all map[string]Card, cons map[string][]Card, ids cold2Set) (Plan, string, string, bool) {
	p := all[ids.prods[rng.Intn(len(ids.prods))]]
	by := append(append([]string{}, ids.prods...), ids.sents...)
	var e Event = Replace{By: by[rng.Intn(len(by))], Reason: "r"}
	if len(p.Needs) > 0 {
		e = Relink{Need: p.Needs[0], To: ids.prods[rng.Intn(len(ids.prods))], Reason: "relinked"}
	}
	plan, r := Move(p, cons[p.ID], w.rows(), StreamFromStore(w.snap().Streams["s1"]), all, e)
	return plan, name(e) + " " + p.ID, p.ID, r == nil
}

func (w *world) cold2Reached(rng *rand.Rand, all map[string]Card, sents []string) (Plan, string, string, bool) {
	if len(sents) == 0 {
		return Plan{}, "", "", false
	}
	g := all[sents[rng.Intn(len(sents))]]
	plan, r := Move(g, nil, w.rows(), StreamFromStore(w.snap().Streams["s1"]), all, Reached{})
	return plan, "Reached " + g.ID, g.ID, r == nil
}

// cold2WaiveRed attempts WaiveRed whenever a producer in review has a done
// ok read of the previous review at the same head, so the walk accepts it.
func (w *world) cold2WaiveRed(rng *rand.Rand, all map[string]Card, cons map[string][]Card, prods []string) (Plan, string, string, bool, bool) {
	var targets []string
	for _, id := range prods {
		if all[id].State == Review && cold2PriorOK(cons[id], all[id]) {
			targets = append(targets, id)
		}
	}
	if len(targets) == 0 {
		return Plan{}, "", "", false, false
	}
	id := targets[rng.Intn(len(targets))]
	p := all[id]
	plan, r := Move(p, cons[id], w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), all, WaiveRed{Reason: "the red was not the code"})
	return plan, "WaiveRed " + id, id, r == nil, true
}

func cold2PriorOK(cons []Card, p Card) bool {
	prev := p.Review - 1
	if prev < 0 {
		return false
	}
	for _, c := range cons {
		if c.Kind == "read" && c.State == Done && c.Result == "ok" && c.Review == prev && c.Head == p.Head {
			return true
		}
	}
	return false
}

// cold2Ask attempts AskReads one time in three while a producer is in review,
// on rows that are up and are not the author.
func (w *world) cold2Ask(rng *rand.Rand, all map[string]Card, cons map[string][]Card, prods []string) (Plan, string, string, bool, bool) {
	if rng.Intn(3) != 0 {
		return Plan{}, "", "", false, false
	}
	var reviews []string
	for _, id := range prods {
		if all[id].State == Review {
			reviews = append(reviews, id)
		}
	}
	if len(reviews) == 0 {
		return Plan{}, "", "", false, false
	}
	id := reviews[rng.Intn(len(reviews))]
	p := all[id]
	rs := w.rows()
	var open []string
	for _, name := range []string{"r1", "r2", "r3"} {
		if row, ok := rs[name]; ok && row.State == Up && name != p.Author {
			open = append(open, name)
		}
	}
	if len(open) == 0 {
		return Plan{}, "", "", false, false
	}
	n := 1
	if len(open) > 1 && rng.Intn(2) == 0 {
		n = 2
	}
	plan, r := Move(p, cons[id], rs, StreamFromStore(w.snap().Streams[p.Stream]), all, AskReads{Rows: open[:n]})
	return plan, "AskReads " + id, id, r == nil, true
}
