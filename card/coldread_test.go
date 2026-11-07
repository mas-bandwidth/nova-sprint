package card

// The first cold read of layer 2 (a822dfc7), folded into the suite in round
// 2 with its author's tests kept. Two kinds of test:
//
//   - TestCold*: adversarial probes, including a seeded random walk on
//     store.Fake that checks the design's invariants and fences after every
//     accepted op (kept as a standing test).
//   - TestColdDefect*: one per confirmed defect; each FAILED at a822dfc7 and
//     now asserts the ruling in "Rulings from the first cold read of layer 2"
//     (D5 was ruled the other way: SetPriority applies to sentinels). The
//     suspects S1-S3 were taken and assert their refusals.
//
// Unit tests, no clock, no sockets: rand is seeded, At is t0 plus the op's
// number.

import (
	"errors"
	"fmt"
	"math"
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

var coldH3 = strings.Repeat("d", 40)

// all is every card of the snapshot, and the consumers of each producer in
// id order.
func (w *world) all() (map[string]Card, map[string][]Card) {
	all, cons := map[string]Card{}, map[string][]Card{}
	for _, c := range w.snap().Cards {
		k := fromStore(c)
		all[k.ID] = k
		if k.Of != "" {
			cons[k.Of] = append(cons[k.Of], k)
		}
	}
	for _, cs := range cons {
		sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
	}
	return all, cons
}

func coldProducerEvents() []Event {
	return []Event{Resolve{}, Park{}, Reached{}, Start{Row: "r1", Branch: "b", Base: base}, Finished{Consumer: "p1.w1"},
		Broken{Consumer: "p1.w1"}, Faulted{Consumer: "p1.w1"}, AskRead{Row: "r1"}, AskReads{Rows: []string{"r1"}}, ReadYes{}, ReadFix{Finding: "x"}, WaiveRed{Reason: "x"},
		Landed{Head: h1}, Red{Head: h1, Reason: "x"}, Released{}, Cancel{Reason: "x"}, Replace{By: "p9", Reason: "x"},
		NeedFailed{Need: "n1"}, Waive{Need: "n1"}, Relink{Need: "n1", To: "n2"}, Edit{Prose: "x"}, Rescore{Score: 9},
		SetPriority{Level: "high"}, Block{Reason: "x"}, Unblock{Reason: "x"}}
}

// coldAllowed is the design's from-column per producer event.
var coldAllowed = map[string][]string{
	"Resolve": {Waiting}, "Park": {Ready}, "Reached": {Waiting}, "Start": {Ready},
	"Finished": {Working}, "Broken": {Working}, "Faulted": {Working}, "Released": {Working},
	"AskRead": {Review}, "AskReads": {Review}, "ReadYes": {Review}, "ReadFix": {Review}, "WaiveRed": {Review}, "Landed": {Merging}, "Red": {Merging},
	"Cancel": nonFinal, "Replace": nonFinal, "NeedFailed": nonFinal, "Edit": nonFinal, "Rescore": nonFinal,
	"SetPriority": nonFinal, "Waive": {Waiting}, "Relink": {Waiting}, "Block": {Ready, Review, Working}, "Unblock": {Blocked},
}

// Final states never move, Rescore, SetPriority and Edit included, for every
// consumer list and kind.
func TestColdFinalNeverMoves(t *testing.T) {
	for _, kind := range []string{"producer", "sentinel"} {
		for _, s := range []string{LandedState, Fail} {
			for _, e := range coldProducerEvents() {
				for _, cons := range [][]Card{nil, {wk(Done, "ok", h2)}, {rd(Done, "ok")}} {
					p := pr(s)
					p.Kind = kind
					_, r := Move(p, cons, rows(), Stream{Name: "s1"}, needs(Fail), e)
					if assert.NotNil(t, r, "%s %s %s", kind, s, name(e)) {
						assert.Equal(t, fmt.Sprintf("%s p1 is %s; nothing moves a %s card", kind, s, s), r.Reason)
					}
				}
			}
		}
	}
	for _, s := range []string{Done, Fail} {
		for _, e := range []Event{Take{Row: "r1"}, Report{Row: "r1", Result: "ok", Head: h2}, Withdraw{}} {
			for _, c := range []Card{wk(s, "ok", h2), rd(s, "ok")} {
				_, r := MoveConsumer(c, pr(Review), rows()["r1"], e)
				assert.NotNil(t, r, "%s %s %s", c.Kind, s, name(e))
			}
		}
	}
}

// No producer event is accepted from a state outside its from-column, in any
// context the sweep can build (consumer lists, held stream, need landed or
// failed, producer or sentinel).
func TestColdAcceptedOnlyFromTable(t *testing.T) {
	ctxs := [][]Card{nil, {wk(Done, "ok", h2)}, {wk(Done, "fail", h2)}, {wk(Done, "fault", h2)}, {wk(Fail, "", "")}, {rd(Done, "ok")}, {wk(Done, "ok", h1), rd(Done, "fix")}}
	for _, e := range coldProducerEvents() {
		for _, s := range producerStates {
			for _, kind := range []string{"producer", "sentinel"} {
				for _, cons := range ctxs {
					for _, held := range []bool{false, true} {
						for _, need := range []string{LandedState, Fail} {
							p := pr(s)
							p.Kind = kind
							if _, r := Move(p, cons, rows(), Stream{Name: "s1", Held: held}, needs(need), e); r == nil {
								assert.Contains(t, coldAllowed[name(e)], s, "%s accepted from %s (%s)", name(e), s, kind)
								if kind == "sentinel" {
									assert.NotContains(t, []string{"Start", "Resolve"}, name(e), "a sentinel never has a consumer") // AskRead: D11
								}
							}
						}
					}
				}
			}
		}
	}
}

// Faulted spends no bounce and no attempt; Released and preemption raise no
// bounce.
func TestColdFaultSpendsNothing(t *testing.T) {
	for _, c := range []struct {
		e    Event
		cons []Card
	}{
		{Faulted{Consumer: "p1.w1"}, []Card{wk(Done, "fault", h2)}},
		{Released{}, []Card{wk(Fail, "", "")}},
	} {
		plan, r := Move(pr(Working), c.cons, rows(), Stream{Name: "s1"}, needs(LandedState), c.e)
		require.Nil(t, r, name(c.e))
		for _, w := range plan.Writes {
			assert.NotEqual(t, "store.SetCardBounces", fmt.Sprintf("%T", w), name(c.e))
			assert.NotEqual(t, "store.SetCardAttempt", fmt.Sprintf("%T", w), name(c.e))
		}
	}
}

// ReadYes: zero ok, a withdrawn read, a fix-only read, a read cut before a
// Red (older review), a live read: each refused.
func TestColdReadYesNeedsAnOkReadOfThisReview(t *testing.T) {
	stale := rd(Done, "ok")
	stale.Review = 0
	for _, cons := range [][]Card{nil, {rd(Fail, "")}, {rd(Done, "fix")}, {stale}, {rd(Working, "")}, {wk(Done, "ok", h1)}} {
		assert.NotNil(t, mv(Review, cons, ReadYes{}), "%+v", cons)
	}
	assert.Nil(t, mv(Review, []Card{stale, rd(Done, "fix"), func() Card { c := rd(Done, "ok"); c.ID = "p1.r1.2"; return c }()}, ReadYes{}),
		"one ok of this review is enough; one fix does not outvote it (policy's)")
}

// Report: a non-holder, a short or malformed head, a read's head not the
// producer's, an empty ok head, the base as an ok head.
func TestColdReportRefusals(t *testing.T) {
	work, read := wk(Working, "", ""), rd(Working, "")
	for _, c := range []struct {
		k Card
		e Report
	}{
		{work, Report{Row: "r2", Result: "ok", Head: h2}},
		{work, Report{Row: "r1", Result: "ok", Head: h2[:39]}},
		{work, Report{Row: "r1", Result: "ok", Head: strings.ToUpper(h2)}},
		{work, Report{Row: "r1", Result: "ok", Head: h2 + "0"}},
		{work, Report{Row: "r1", Result: "ok"}},
		{work, Report{Row: "r1", Result: "ok", Head: base}},
		{work, Report{Row: "r1", Result: ""}},
		{read, Report{Row: "r1", Result: "ok", Head: h2}},
		{read, Report{Row: "r1", Result: "fix", Head: coldH3}},
		{read, Report{Row: "r2", Result: "ok", Head: h1}},
	} {
		_, r := MoveConsumer(c.k, pr(Review), rows()["r1"], c.e)
		assert.NotNil(t, r, "%s %+v", c.k.Kind, c.e)
	}
}

// Start: held stream, held row, down row, unknown row, wrong tier, and every
// producer state but ready.
func TestColdStartRefusals(t *testing.T) {
	for _, row := range []string{"r3", "r4", "r5", "r9", ""} {
		assert.NotNil(t, mv(Ready, nil, Start{Row: row, Branch: "b", Base: base}), row)
	}
	_, r := Move(pr(Ready), nil, rows(), Stream{Name: "s1", Held: true}, needs(LandedState), Start{Row: "r1", Branch: "b", Base: base})
	assert.NotNil(t, r)
	for _, s := range producerStates {
		if s != Ready {
			assert.NotNil(t, mv(s, nil, Start{Row: "r1", Branch: "b", Base: base}), s)
		}
	}
}

// Every row event from every row state: accepted only per the row table,
// and every accepted plan lands on Fake as written.
func TestColdRowsEveryState(t *testing.T) {
	for _, down := range []bool{false, true} {
		for _, held := range []bool{false, true} {
			for _, e := range []Event{Seen{}, Gone{}, Width{N: 3}, Width{N: 2}, Hold{Reason: "h"}, Unhold{Reason: "u"}} {
				w := newWorld(t)
				if down {
					w.row("r1", Gone{})
				}
				if held {
					w.row("r1", Hold{})
				}
				r := w.rows()["r1"]
				plan, ref := MoveRow(r, e)
				want := map[string]bool{"Seen": down, "Gone": !down, "Hold": !held, "Unhold": held}[name(e)]
				if v, ok := e.(Width); ok {
					want = v.N != r.Width
				}
				label := fmt.Sprintf("%s down=%v held=%v", name(e), down, held)
				if !want {
					assert.NotNil(t, ref, label)
					continue
				}
				require.Nil(t, ref, label)
				assert.Contains(t, plan.Expect, store.Expect(store.RowGen{Row: "r1", Gen: r.Gen}), label)
				rc, err := w.apply(kind(e), plan)
				require.NoError(t, err, label)
				assert.True(t, rc.Accepted, "%s: %s", label, rc.Reason)
				w.landed(plan)
			}
		}
	}
}

// AskRead while a live read of this review is present is legal (reads of
// one head may be live together); it names the next id. S3 (taken): never to
// the row already holding a live read of this head.
func TestColdAskReadBesideALiveRead(t *testing.T) {
	refusal{mv(Review, []Card{rd(Working, "")}, AskRead{Row: "r1"}), "row r1 already holds live read p1.r1.1 of head " + h1}.check(t)
	other := rd(Working, "")
	other.Holder = "r3"
	plan, r := Move(pr(Review), []Card{other}, rows(), Stream{Name: "s1"}, nil, AskRead{Row: "r1"})
	require.Nil(t, r)
	assert.Contains(t, plan.Expect, store.Expect(store.CardAbsent{Card: "p1.r1.2"}))
	put := plan.Writes[0].(store.PutCard)
	assert.Equal(t, []string{"p1.r1.2", "r1"}, []string{put.ID, put.Holder})
}

// Preempting a read: the withdraw of the read and the blocker's Start in one
// op on a full row; the read's producer stays in review and is asked again.
func TestColdPreemptARead(t *testing.T) {
	w := newWorld(t, producer("p1"), store.PutCard{ID: "p2", Priority: "blocker"})
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.move("p1", AskRead{Row: "r2"})
	w.consume("p1.r1.1", Take{Row: "r2"})
	w.move("p2", Resolve{})
	start, r := w.plan("p2", Start{Row: "r2", Branch: "b", Base: base})
	require.Nil(t, r)
	w.op("preempt", mustMerge(t, w.consumerPlan("p1.r1.1", Withdraw{Reason: "preempted by blocker p2", Preempt: true}), start))
	w.consume("p2.w1", Take{Row: "r2"})
	w.is("p1", Review)
	assert.Zero(t, w.card("p1").Bounces)
	w.refused("p1", ReadYes{}, "producer p1 has no read of review 1 that said ok; ReadYes needs one")
	w.move("p1", AskRead{Row: "r3"})
	w.is("p1.r1.2", Ready)
}

// The random walk: seeded sequences of every event with random arguments,
// each plan built from a fresh snapshot as layer 3 would and applied to Fake.
// After every accepted op the design's invariants hold, and every plan
// carries its fences. A Fake refusal other than width is a machine defect
// (a same-value write, a write refused for kind, a bad fence).
func TestColdWalk(t *testing.T) {
	seen := map[string]int{}
	var mu sync.Mutex
	t.Run("seeds", func(t *testing.T) {
		for seed := int64(1); seed <= 100; seed++ {
			t.Run(fmt.Sprint(seed), func(t *testing.T) {
				t.Parallel() // each seed its own Fake
				got := coldWalk(t, seed, 800)
				mu.Lock()
				defer mu.Unlock()
				for k, n := range got {
					seen[k] += n
				}
			})
		}
	})
	// Released is not in the list: a Withdraw of the work consumer releases
	// its producer in the same plan, so the walk never finds one to release.
	for _, e := range []string{"Resolve", "Start", "Take", "Report", "Finished", "Broken", "Faulted", "AskRead", "ReadYes", "ReadFix",
		"Landed", "Red", "Withdraw", "Block", "Unblock", "Park", "Rescore", "SetPriority", "Edit", "Gone", "Seen", "Hold", "Width", "HoldStream"} {
		assert.Positive(t, seen[e], "the walk never accepted %s", e)
	}
	t.Logf("accepted ops by event over 100 seeds x 800 steps: %v", seen)
}

type coldFinal struct {
	state string
	gen   int
}

func coldWalk(t *testing.T, seed int64, steps int) map[string]int {
	return coldWalkTraced(t, seed, steps, false)
}

// coldWalkTraced is the walk; traced records it for trace validation
// (card/trace_test.go): one record per accepted op.
func coldWalkTraced(t *testing.T, seed int64, steps int, traced bool) map[string]int {
	rng := rand.New(rand.NewSource(seed))
	w := newWorld(t, producer("p1"), producer("p2", "p1"), producer("p3"), store.PutCard{ID: "g1", Kind: "sentinel", Needs: []string{"p2"}})
	tr := newCardTrace(w, traced)
	defer tr.done(seed)
	finals := map[string]coldFinal{}
	accepted := map[string]int{}
	for i := 0; i < steps; i++ {
		plan, label, subject, ok := w.coldStep(rng)
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
		tr.step(label, plan)
		coldInvariants(t, w.snap(), finals, fmt.Sprintf("seed %d step %d %s", seed, i, label))
	}
	return accepted
}

// coldStep picks one move at random: a producer, consumer or row event, or a
// stream hold toggle (layer 3's raw op, to exercise Park).
func (w *world) coldStep(rng *rand.Rand) (Plan, string, string, bool) {
	w.ev = nil // the trace's: a raw hold has no event
	all, cons := w.all()
	var ids []string
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rowNames := []string{"r1", "r2", "r3", "r9"}
	row := rowNames[rng.Intn(len(rowNames))]
	pick := func(ss []string) string { return ss[rng.Intn(len(ss))] }
	if w.snap().Streams["s1"].Held && rng.Intn(6) == 0 { // holds are short, so work gets through
		return Plan{Writes: []store.Write{store.SetStreamHeld{Stream: "s1", Held: false}}}, "HoldStream s1", "", true
	}
	switch n := rng.Intn(60); {
	case n < 34:
		return w.coldStepCard(rng, ids, all, cons, row, pick)
	case n < 56:
		return w.coldStepConsumer(rng, ids, all, row, pick)
	case n < 59:
		r := w.rows()[pick(rowNames[:3])]
		e := []Event{Seen{}, Gone{}, Width{N: 1 + rng.Intn(3)}, Hold{Reason: "h"}, Unhold{Reason: "u"}}[rng.Intn(5)]
		plan, ref := MoveRow(r, e)
		w.ev = e
		return plan, name(e) + " " + r.Name, "", ref == nil
	}
	held := w.snap().Streams["s1"].Held
	return Plan{Writes: []store.Write{store.SetStreamHeld{Stream: "s1", Held: !held}}}, "HoldStream s1", "", true
}

func (w *world) coldStepCard(rng *rand.Rand, ids []string, all map[string]Card, cons map[string][]Card, row string, pick func([]string) string) (Plan, string, string, bool) {
	var prods []string
	for _, id := range ids {
		if !consumer(all[id].Kind) {
			prods = append(prods, id)
		}
	}
	p := all[pick(prods)]
	cs := cons[p.ID]
	cid := workID(p.ID, p.Attempt)
	if len(cs) > 0 && rng.Intn(4) == 0 {
		cid = cs[rng.Intn(len(cs))].ID
	}
	heads := []string{p.Head, p.Head, h1, coldH3}
	evs := []Event{Resolve{}, Resolve{}, Park{}, Reached{}, Start{Row: row, Branch: "b-" + p.ID, Base: base}, Start{Row: row, Branch: "b-" + p.ID, Base: base},
		Finished{Consumer: cid}, Finished{Consumer: cid}, Broken{Consumer: cid}, Faulted{Consumer: cid}, AskRead{Row: row}, AskReads{Rows: []string{row}},
		ReadYes{}, ReadYes{}, ReadFix{Finding: "fix it"}, Landed{Head: pick(heads)}, Red{Head: pick(heads), Reason: "red"}, Red{Head: p.Head, Reason: "red"}, Released{},
		Block{Reason: "bounced"}, Unblock{Reason: "looked"}, Rescore{Score: rng.Intn(4)}, SetPriority{Level: pick(Levels), Reason: "asked"},
		Edit{Prose: fmt.Sprint("prose ", rng.Intn(3))}}
	if rng.Intn(120) == 0 {
		evs = []Event{Cancel{Reason: "c"}, Replace{By: "p3", Reason: "r"}, NeedFailed{Need: "p1"}, Waive{Need: "p1"}, Relink{Need: "p1", To: "p3"}}
	}
	if legal := slices.DeleteFunc(slices.Clone(evs), func(e Event) bool { return !slices.Contains(coldAllowed[name(e)], p.State) }); len(legal) > 0 && rng.Intn(5) > 0 {
		evs = legal // mostly events whose from-state matches, so the walk gets deep
	}
	side := []string{"Rescore", "SetPriority", "Edit", "Block"}
	if moves := slices.DeleteFunc(slices.Clone(evs), func(e Event) bool { return slices.Contains(side, name(e)) }); len(moves) > 0 && rng.Intn(10) > 0 {
		evs = moves // the side verbs are legal everywhere and would crowd out the path
	}
	if p.State == Merging && rng.Intn(3) > 0 {
		evs = []Event{Landed{Head: p.Head}, Landed{Head: p.Head}, Red{Head: p.Head, Reason: "red"}}
	}
	e := evs[rng.Intn(len(evs))]
	st := w.snap().Streams[p.Stream]
	plan, r := Move(p, cs, w.rows(), Stream{Name: st.Name, Held: st.Held}, all, e)
	w.ev = e
	return plan, name(e) + " " + p.ID, p.ID, r == nil
}

func (w *world) coldStepConsumer(rng *rand.Rand, ids []string, all map[string]Card, row string, pick func([]string) string) (Plan, string, string, bool) {
	var cids []string
	for _, id := range ids {
		if consumer(all[id].Kind) {
			cids = append(cids, id)
		}
	}
	if len(cids) == 0 {
		return Plan{}, "", "", false
	}
	if live := slices.DeleteFunc(slices.Clone(cids), func(id string) bool { return final(all[id].Kind, all[id].State) }); len(live) > 0 && rng.Intn(5) > 0 {
		cids = live
	}
	c := all[pick(cids)]
	p := all[c.Of]
	at := c.Holder
	if rng.Intn(6) == 0 {
		at = row
	}
	head := pick([]string{p.Head, p.Head, h1, h2, coldH3, base, "", "abc"})
	result := pick([]string{"ok", "ok", "ok", "fix", "fail", "fault", "maybe"})
	if c.Kind == "read" && rng.Intn(4) > 0 {
		head = p.Head
	}
	evs := []Event{Take{Row: at}, Take{Row: at}, Report{Row: at, Result: result, Head: head, Reason: "r", Cost: int64(rng.Intn(3)), Priced: rng.Intn(2) == 0},
		Report{Row: at, Result: result, Head: head}, Report{Row: at, Result: result, Head: head, Reason: "r"}, Withdraw{Reason: "silent"}}
	if rng.Intn(3) > 0 {
		evs = evs[:5]
	}
	e := evs[rng.Intn(len(evs))]
	if v, ok := e.(Withdraw); ok { // the producer's stream, as layer 3 reads it
		v.Stream = StreamFromStore(w.snap().Streams[p.Stream])
		e = v
	}
	plan, r := MoveConsumer(c, p, w.rows()[c.Holder], e)
	w.ev = e
	return plan, name(e) + " " + c.ID, c.ID, r == nil
}

// coldFences: the moved card is fenced by its gen; every cut consumer by
// CardAbsent, and its holder row by RowGen.
func coldFences(t *testing.T, plan Plan, subject, label string) {
	t.Helper()
	assert.True(t, slices.ContainsFunc(plan.Expect, func(e store.Expect) bool { g, ok := e.(store.CardGen); return ok && g.Card == subject }), "%s: no CardGen of %s", label, subject)
	for _, x := range plan.Writes {
		if put, ok := x.(store.PutCard); ok {
			assert.Contains(t, plan.Expect, store.Expect(store.CardAbsent{Card: put.ID}), "%s: cut %s without CardAbsent", label, put.ID)
			assert.True(t, slices.ContainsFunc(plan.Expect, func(e store.Expect) bool { g, ok := e.(store.RowGen); return ok && g.Row == put.Holder }), "%s: cut on %s without RowGen", label, put.Holder)
		}
	}
}

// coldInvariants are the design's invariants over a snapshot.
func coldInvariants(t *testing.T, s store.Snapshot, finals map[string]coldFinal, label string) {
	t.Helper()
	liveWork := map[string]int{}
	working := map[string]int{}
	for _, c := range s.Cards {
		if was, ok := finals[c.ID]; ok {
			assert.Equal(t, was, coldFinal{c.State, c.Gen}, "%s: final card %s moved", label, c.ID)
		}
		if final(c.Kind, c.State) {
			finals[c.ID] = coldFinal{c.State, c.Gen}
		}
		if !consumer(c.Kind) {
			assert.Empty(t, c.Holder, "%s: producer %s has a holder", label, c.ID)
			continue
		}
		p := s.Cards[c.Of]
		if c.State == Working {
			working[c.Holder]++
		}
		if c.Kind == "read" {
			assert.NotEqual(t, c.Author, c.Holder, "%s: read %s dealt to its author", label, c.ID)
		}
		if c.State != Ready && c.State != Working {
			continue
		}
		if c.Kind == "work" {
			liveWork[c.Of]++
			assert.Equal(t, []any{Working, p.Attempt}, []any{p.State, c.Attempt}, "%s: live work %s under %s", label, c.ID, p.ID)
		} else {
			assert.Equal(t, []any{Review, p.Review, p.Head}, []any{p.State, c.Review, c.Head}, "%s: live read %s under %s", label, c.ID, p.ID)
		}
	}
	for p, n := range liveWork {
		assert.LessOrEqual(t, n, 1, "%s: producer %s has %d live work consumers", label, p, n)
	}
	for name, n := range working {
		assert.LessOrEqual(t, n, s.Rows[name].Width, "%s: row %s over width", label, name)
	}
	for _, p := range s.Cards {
		switch {
		case p.Kind == "producer" && p.State == Working:
			_, ok := s.Cards[workID(p.ID, p.Attempt)]
			assert.True(t, ok, "%s: %s working with no work consumer of attempt %d", label, p.ID, p.Attempt)
		case p.Kind == "producer" && p.State == Merging:
			okRead := func(review int) bool {
				return slices.ContainsFunc(coldValues(s.Cards), func(c store.Card) bool {
					return c.Of == p.ID && c.Kind == "read" && c.Review == review && c.Head == p.Head && c.State == Done && c.Result == "ok"
				})
			}
			// ReadYes has an ok read of this review. WaiveRed has one of the
			// previous review, at this head, and does not raise the review.
			assert.True(t, okRead(p.Review) || (p.Review > 0 && okRead(p.Review-1)), "%s: %s merging without an ok read of review %d or %d at its head", label, p.ID, p.Review, p.Review-1)
		}
	}
	assert.Empty(t, coldCycle(s.Cards), "%s: needs cycle", label)
}

func coldValues(m map[string]store.Card) []store.Card {
	var cs []store.Card
	for _, c := range m {
		cs = append(cs, c)
	}
	return cs
}

// coldCycle is a needs cycle in cards, or nil.
func coldCycle(cards map[string]store.Card) []string {
	state := map[string]int{}
	var path []string
	var walk func(id string) []string
	walk = func(id string) []string {
		switch state[id] {
		case 1:
			return append(slices.Clone(path), id)
		case 2:
			return nil
		}
		state[id] = 1
		path = append(path, id)
		for _, n := range cards[id].Needs {
			if loop := walk(n); loop != nil {
				return loop
			}
		}
		path = path[:len(path)-1]
		state[id] = 2
		return nil
	}
	ids := make([]string, 0, len(cards))
	for id := range cards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if loop := walk(id); loop != nil {
			return loop
		}
	}
	return nil
}

// ---- confirmed defects: each FAILS at a822dfc7 ----

// D1. A work consumer may report "fix" (a read's verdict). Nothing then moves
// its producer: Finished needs ok, Broken fail, Faulted fault, Released fail
// (withdrawn), so the producer sits in working with no live consumer and no
// legal forward move (LESSONS 2.21, nothing stalls).
func TestColdDefectWorkReportFixStallsProducer(t *testing.T) {
	_, refused := MoveConsumer(wk(Working, "", ""), pr(Working), rows()["r1"], Report{Row: "r1", Result: "fix", Head: h2})
	moved := false
	for _, e := range []Event{Finished{Consumer: "p1.w1"}, Broken{Consumer: "p1.w1"}, Faulted{Consumer: "p1.w1"}, Released{}} {
		if mv(Working, []Card{wk(Done, "fix", h2)}, e) == nil {
			moved = true
		}
	}
	assert.True(t, refused != nil || moved, "a work consumer's fix report is accepted, and no producer event accepts it")
}

// D2. Block from working is allowed only "after its consumer is withdrawn";
// the machine accepts it over a work consumer that is done ok (its finished
// head never reaches the producer).
func TestColdDefectBlockOverFinishedWork(t *testing.T) {
	assert.NotNil(t, mv(Working, []Card{wk(Done, "ok", h2)}, Block{Reason: "bounced"}), "Block over a done-ok work consumer")
	assert.NotNil(t, mv(Working, nil, Block{Reason: "bounced"}), "Block of a working producer with no withdrawn consumer named")
}

// D3. noLive trusts the caller's consumer list. In working the machine knows
// its consumer's id (p.w<attempt>) but does not require it: with the list
// left out, Cancel (and Block, Edit, NeedFailed) plan without fencing the
// live consumer, and Fake accepts: a failed producer with a live consumer.
func TestColdDefectOmittedLiveConsumerIsNotFenced(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.consume("p1.w1", Take{Row: "r1"})
	all, _ := w.all()
	plan, r := Move(all["p1"], nil, w.rows(), Stream{Name: "s1"}, all, Cancel{Reason: "x"})
	refusal{r, "producer p1 is working; its work consumer p1.w1 was not passed (the consumer list must be complete)"}.check(t)
	if r != nil {
		return
	}
	rc, err := w.apply("cancel", plan)
	require.NoError(t, err)
	assert.False(t, rc.Accepted && w.card("p1").State == Fail && w.card("p1.w1").State == Working,
		"producer p1 is fail while p1.w1 is still working: the plan carried no fence on p1.w1")
}

// D4. Replace names a card that need not exist, or the card itself.
func TestColdDefectReplaceByUnknownOrSelf(t *testing.T) {
	assert.NotNil(t, mv(Ready, nil, Replace{By: "nope", Reason: "x"}), "replaced by a card that does not exist")
	assert.NotNil(t, mv(Ready, nil, Replace{By: "p1", Reason: "x"}), "replaced by itself")
}

// D5, ruled the other way: SetPriority applies to producers and sentinels
// (a release gate may be a blocker).
func TestColdDefectSetPriorityOnSentinel(t *testing.T) {
	g := pr(Waiting)
	g.Kind = "sentinel"
	plan, r := Move(g, nil, rows(), Stream{Name: "s1"}, needs(LandedState), SetPriority{Level: "high"})
	require.Nil(t, r)
	assert.Contains(t, plan.Writes, store.Write(store.SetCardPriority{Card: "p1", Priority: "high"}))
}

// D6. A fix continues "with the finding in hand": Start copies p.reason to
// the work consumer. But SetPriority, Park, Unblock and Edit overwrite the
// same column, so the finding a ReadFix left is replaced by the reason for
// a priority change (or "stream s1 is held") before the next Start.
func TestColdDefectFindingOverwrittenBeforeStart(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.read("p1", "r2", "fix", "rename x")
	w.move("p1", ReadFix{Finding: "rename x"})
	w.move("p1", SetPriority{Level: "high", Reason: "raised for the release"})
	w.move("p1", Start{Row: "r3", Branch: "b", Base: base})
	assert.Equal(t, "rename x", w.card("p1.w2").Finding, "the fix attempt lost the reader's finding")
	assert.Equal(t, "raised for the release", w.card("p1").Reason, "reason stays the status reason")
}

// D7. Waive is legal when the need "is failed by an earlier write of the
// same op (the drop that names the waive)". The machine always fences
// CardState{need, fail}, and layer 1 checks every condition before any
// write, so the drop and its waive can never be one op.
func TestColdDefectWaiveInTheDropsOp(t *testing.T) {
	w := newWorld(t, producer("n1"), producer("p3", "n1"))
	drop, r := w.plan("n1", Cancel{Reason: "dropped"})
	require.Nil(t, r)
	all, _ := w.all()
	waive, r := Move(all["p3"], nil, w.rows(), Stream{Name: "s1"}, all, Waive{Need: "n1", Reason: "not needed", InThisOp: true})
	require.Nil(t, r)
	rc, err := w.apply("drop", mustMerge(t, drop, waive))
	require.NoError(t, err)
	assert.True(t, rc.Accepted, "drop + waive as one op refused: %s", rc.Reason)
	assert.Empty(t, w.card("p3").Needs)
	assert.Equal(t, Fail, w.card("n1").State)
}

// D8. Relink's cycle check reads the needs of every card on the path but
// fences only p and the target. Two relinks from one snapshot each pass the
// check and both land: p1 needs x needs y needs z needs p1.
func TestColdDefectConcurrentRelinksMakeACycle(t *testing.T) {
	w := newWorld(t, producer("a"), producer("b"), producer("y", "b"), producer("x", "y"), producer("p1", "a"), producer("z", "p1"))
	w.move("a", Cancel{Reason: "dropped"})
	w.move("b", Cancel{Reason: "dropped"})
	one, r := w.plan("p1", Relink{Need: "a", To: "x", Reason: "x does it"})
	require.Nil(t, r)
	two, r := w.plan("y", Relink{Need: "b", To: "z", Reason: "z does it"})
	require.Nil(t, r)
	for _, p := range []Plan{one, two} {
		_, err := w.apply("relink", p)
		require.NoError(t, err)
	}
	assert.Empty(t, coldCycle(w.snap().Cards), "a needs cycle landed")
}

// D9. Released writes "head = the consumer's last pushed head", and a
// preempted consumer's partial work is to be "the next Start's prior_head".
// No consumer event carries a head without finishing the consumer (Withdraw
// has none, Report moves it to done), so a withdrawn consumer's head is
// always empty and that write can never fire.
func TestColdDefectWithdrawnHeadIsUnrecordable(t *testing.T) {
	_ = Withdraw{Head: coldH3} // compiles: Withdraw records the pushed head (no reflection)
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.consume("p1.w1", Take{Row: "r1"})
	w.consume("p1.w1", Withdraw{Reason: "preempted by blocker p2", Head: coldH3, Preempt: true}) // releases p1 in the same plan
	w.is("p1", Ready)
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
	assert.Equal(t, coldH3, w.card("p1.w2").PriorHead, "the preempted attempt's pushed head is the next attempt's prior head")
}

// D10. Plans the store refuses as ErrInvalid (a caller bug, logged nowhere):
// a negative cost, a score or width past int32, a NUL in a branch, a row with
// no name. The machine must refuse these as values.
func TestColdDefectPlansTheStoreFindsInvalid(t *testing.T) {
	cases := []struct {
		label string
		plan  func(w *world) (Plan, *Refusal)
	}{
		{"cost -1", func(w *world) (Plan, *Refusal) {
			w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
			w.consume("p1.w1", Take{Row: "r1"})
			c := w.card("p1.w1")
			return MoveConsumer(c, w.card("p1"), w.rows()["r1"], Report{Row: "r1", Result: "fail", Cost: -1})
		}},
		{"score past int32", func(w *world) (Plan, *Refusal) { return w.plan("p1", Rescore{Score: math.MaxInt32 + 1}) }},
		{"width past int32", func(w *world) (Plan, *Refusal) { return MoveRow(w.rows()["r1"], Width{N: math.MaxInt32 + 1}) }},
		{"NUL in branch", func(w *world) (Plan, *Refusal) { return w.plan("p1", Start{Row: "r1", Branch: "b\x00", Base: base}) }},
		{"row with no name", func(w *world) (Plan, *Refusal) { return MoveRow(Row{}, Width{N: 1}) }},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			w := newWorld(t, producer("p1"))
			w.move("p1", Resolve{})
			plan, r := c.plan(w)
			assert.NotNil(t, r, "the machine refuses the value")
			if r != nil {
				return
			}
			_, err := w.apply(c.label, plan)
			assert.False(t, errors.Is(err, store.ErrInvalid), "machine accepted, store says: %v", err)
		})
	}
}

// D11. "A sentinel ... never a consumer": Start refuses a sentinel, AskRead
// does not, and cuts a read consumer of it. Unreachable through the
// machine's own moves (a sentinel never reaches review), so defence in depth
// only; the guard is one line in askRead.
func TestColdDefectAskReadCutsAReadOfASentinel(t *testing.T) {
	g := pr(Review)
	g.Kind = "sentinel"
	_, r := Move(g, nil, rows(), Stream{Name: "s1"}, nil, AskRead{Row: "r1"})
	assert.NotNil(t, r, "a read consumer cut of sentinel p1")
}

// ---- suspected: probes that pass today, recorded with t.Log ----

// S1. A fix attempt may report ok with head equal to its prior_head (the
// head the reads just sent back): nothing new was pushed, yet Finished takes
// it to review. Only head == base_sha is refused (LESSONS 3.5 says "an empty,
// partial or not-done head is broken").
func TestColdSuspectOkHeadEqualsPriorHead(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.read("p1", "r2", "fix", "rename x")
	w.move("p1", ReadFix{Finding: "rename x"})
	w.move("p1", Start{Row: "r3", Branch: "b", Base: base})
	w.consume("p1.w2", Take{Row: "r3"})
	c := w.card("p1.w2")
	_, r := MoveConsumer(c, w.card("p1"), w.rows()["r3"], Report{Row: "r3", Result: "ok", Head: h1})
	refusal{r, "head " + h1 + " is the prior head; nothing new was pushed"}.check(t)
}

// S2. ReadYes counts reads by review number only; it never compares the
// read's head with p.head or checks the read is p's (Of). Unreachable through
// the machine's own moves today (Finished and Red raise review), but the
// design states the rule on heads.
func TestColdSuspectReadYesIgnoresHeadAndOf(t *testing.T) {
	other := rd(Done, "ok")
	other.Head = h2
	assert.NotNil(t, mv(Review, []Card{other}, ReadYes{}), "a read of another head does not count")
	foreign := rd(Done, "ok")
	foreign.ID, foreign.Of = "p9.r1.1", "p9"
	assert.NotNil(t, mv(Review, []Card{foreign}, ReadYes{}), "another producer's read does not count")
}
