package card

// Folded from /Users/glenn/rowan-working/children/nsv2-card-coldread3/card/coldread3_test.go
// (the third cold read of layer 2, at 1b6e66ab). Two kinds of test:
//
//   - TestColdread3Defect*: one per confirmed defect; each failed at 1b6e66ab.
//   - TestColdread3Walk: the random walk of coldread_test.go and
//     coldread2_test.go, extended with plans merged from one snapshot (two
//     random moves, a hold with a move, a drop with one or two relinks in
//     its op), Relink with InThisOp, Replace, AskReads, WaiveRed and
//     UnholdStream, and invariants on how a producer last entered review,
//     what a fault spends, and the needs and after graph. It counts every
//     invariant it sees broken and fails once per kind.
//
// Unit tests, no clock, no sockets: rand is seeded, At is t0 plus the op's
// number.

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

// ---- confirmed defects: each FAILS at 1b6e66ab ----

// D1. WaiveRed "moves a producer that a Red sent back to review on to
// merging without a new read" (LAYER-2, second cold read rulings). The
// machine checks only that a done ok read of review-1 exists at p's head, so
// a producer that entered review by Finished, not Red, passes when its new
// attempt pushed a head equal to one read earlier: the reads of the new
// review are skipped. Reached by legal moves: ok + fix reads, ReadFix, a
// broken attempt that pushed h2, then an ok attempt that pushed h1 again.
func TestColdread3DefectWaiveRedSkipsTheReadsOfANewAttempt(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.read("p1", "r2", "ok", "")
	w.read("p1", "r3", "fix", "the tests miss a case")
	w.move("p1", ReadFix{Finding: "the tests miss a case"})
	w.move("p1", Broken{Consumer: w.work("p1", "r1", "fail", h2, "half done")})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	p := w.card("p1")
	require.Equal(t, []any{Review, 2, h1}, []any{p.State, p.Review, p.Head})
	_, r := w.plan("p1", WaiveRed{Reason: "the red was not the code"})
	assert.NotNil(t, r, "WaiveRed sent p1 to merging though it entered review 2 by Finished, not Red, and review 2 has no read")
}

// D2. Merge "validates the merged plan ... so no plan the machine emits is
// ever invalid at the store", and the needs and after graphs stay acyclic
// (LAYER-2, "How it is checked"; LESSONS 2.19). Each Relink fences every card
// its walk read, which stops two relinks applied as two ops (coldread D8),
// but layer 1 checks every condition before any write, so the same two
// relinks merged into one op both pass their fences and close a loop:
// p1 needs x needs y needs z needs p1. The store has no cycle check.
func TestColdread3DefectMergedRelinksMakeACycle(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("a"), producer("b"), producer("y", "b"), producer("x", "y"), producer("p1", "a"), producer("z", "p1"))
	w.move("a", Cancel{Reason: "dropped"})
	w.move("b", Cancel{Reason: "dropped"})
	one, r := w.plan("p1", Relink{Need: "a", To: "x", Reason: "x does it"})
	require.Nil(t, r)
	two, r := w.plan("y", Relink{Need: "b", To: "z", Reason: "z does it"})
	require.Nil(t, r)
	merged, mr := Merge(one, two)
	if mr != nil {
		return // refused: the graph stays acyclic
	}
	rc, err := w.apply("relinks", merged)
	require.NoError(t, err)
	assert.False(t, rc.Accepted, "Merge passed two relinks that the store applied as one op")
	assert.Empty(t, coldCycle(w.snap().Cards), "a needs cycle landed through one merged op")
}

// D3. HoldStream parks the stream's ready producers "in the same op, so a
// card that cannot start is never left in ready" (LAYER-2 Park row; LESSONS
// 2.20), and its AtSeq fence is there so a card that became ready after the
// snapshot refuses the op. Merged with a Resolve planned from the same
// snapshot (p1 waiting, so the hold rightly names no ready producer), every
// condition holds before the writes: the op holds s1 and makes p1 ready in
// it. Broken, Faulted, ReadFix and a releasing Withdraw merge the same way.
func TestColdread3DefectMergedHoldLeavesAReadyProducer(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	s := w.snap()
	hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "held for a release", Seq: s.Seq})
	require.Nil(t, r)
	resolve, r := w.plan("p1", Resolve{})
	require.Nil(t, r)
	merged, mr := Merge(resolve, hold)
	if mr != nil {
		return // refused: nothing is ready in a held stream
	}
	rc, err := w.apply("hold", merged)
	require.NoError(t, err)
	got := w.snap()
	assert.False(t, rc.Accepted && got.Streams["s1"].Held && got.Cards["p1"].State == Ready,
		"one merged op held s1 and left p1 ready in it")
}

// ---- the random walk, extended ----

type cold3Break struct {
	seeds, steps int
	first        string
}

func cold3Note(m map[string]*cold3Break, kind, where string) {
	b, ok := m[kind]
	if !ok {
		b = &cold3Break{first: where}
		m[kind] = b
	}
	b.steps++
}

func TestColdread3Walk(t *testing.T) {
	t.Parallel()
	seeds, steps := walkSeeds(60, 20), 1000 // 60,000 steps (a third under -short); odd seeds merge plans, even seeds do not, so the walk also gets deep
	var mu sync.Mutex
	seen := map[string]int{}
	var extra int
	broken, notes := map[string]*cold3Break{}, map[string]*cold3Break{}
	fold := func(into, from map[string]*cold3Break) {
		for k, b := range from {
			if a, ok := into[k]; ok {
				a.seeds++
				a.steps += b.steps
			} else {
				into[k] = &cold3Break{seeds: 1, steps: b.steps, first: b.first}
			}
		}
	}
	t.Run("seeds", func(t *testing.T) {
		for seed := int64(1); seed <= seeds; seed++ {
			t.Run(fmt.Sprint(seed), func(t *testing.T) {
				t.Parallel() // each seed its own Fake
				acc, br, nt, n := cold3Walk(t, seed, steps)
				mu.Lock()
				defer mu.Unlock()
				extra += n
				for k, n := range acc {
					seen[k] += n
				}
				fold(broken, br)
				fold(notes, nt)
			})
		}
	})
	for _, e := range []string{"WaiveRed", "UnholdStream", "HoldStream", "Relink", "Replace", "AskReads", "Drop+Relink", "Merge", "Finished", "ReadYes", "ReadFix", "Landed"} {
		assert.Positive(t, seen[e], "the walk never accepted %s", e)
	}
	t.Logf("accepted ops by event over %d seeds x %d steps: %v", seeds, steps, seen)
	t.Logf("merge cost: %d extra moves (one re-run for each plan after the first, refused merges included)", extra)
	for _, k := range cold3Keys(notes) {
		t.Logf("note %q: %d seeds, %d steps; first: %s", k, notes[k].seeds, notes[k].steps, notes[k].first)
	}
	for _, k := range cold3Keys(broken) {
		t.Errorf("invariant %q broken: %d seeds, %d steps; first: %s", k, broken[k].seeds, broken[k].steps, broken[k].first)
	}
}

func cold3Keys(m map[string]*cold3Break) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func cold3Walk(t *testing.T, seed int64, steps int) (map[string]int, map[string]*cold3Break, map[string]*cold3Break, int) {
	rng := rand.New(rand.NewSource(seed))
	w := newWorld(t, producer("p1"), producer("p2", "p1"), producer("p3"), producer("p4", "p3"), store.PutCard{ID: "g1", Kind: "sentinel", Needs: []string{"p2"}})
	accepted := map[string]int{}
	broken, notes := map[string]*cold3Break{}, map[string]*cold3Break{}
	entry := map[string]string{} // how each producer last entered review
	extra := 0
	for i := 0; i < steps; i++ {
		before := w.snap()
		plan, label, ok := w.cold3Step(rng, seed%2 == 1)
		extra += plan.replays
		if !ok {
			continue
		}
		where := fmt.Sprintf("seed %d step %d %s", seed, i, label)
		rc, err := w.apply(label, plan)
		require.NoError(t, err, "%s: the store found the plan invalid: %+v", where, plan)
		if !rc.Accepted {
			switch {
			case strings.Contains(rc.Reason, "width"):
			case strings.HasPrefix(label, "Merge") || strings.HasPrefix(label, "Drop+Relink"):
				cold3Note(notes, "store refused a merged plan", where+": "+rc.Reason)
			default:
				cold3Note(broken, "store refused a single machine plan", where+": "+rc.Reason)
			}
			continue
		}
		accepted[strings.Fields(label)[0]]++
		for _, v := range cold3Violations(before, w.snap(), entry) {
			if strings.HasPrefix(v[0], "note: ") {
				cold3Note(notes, strings.TrimPrefix(v[0], "note: "), where+": "+v[1])
			} else {
				cold3Note(broken, v[0], where+": "+v[1])
			}
		}
	}
	return accepted, broken, notes, extra
}

// cold3Step is cold2Step, and with merges on: one time in twelve two random
// moves from one snapshot merged; one in twelve a HoldStream merged with a
// move from the same snapshot; one in ten a drop merged with one or two
// relinks of its dependants.
func (w *world) cold3Step(rng *rand.Rand, merges bool) (Plan, string, bool) {
	k := rng.Intn(60)
	if !merges {
		k = 99
	}
	if rng.Intn(3) == 0 {
		return w.cold3Drive(rng)
	}
	switch {
	case k < 5:
		a, la, _, oka := w.cold2Step(rng)
		b, lb, _, okb := w.cold2Step(rng)
		if !oka || !okb {
			return Plan{}, "", false
		}
		m, r := Merge(a, b)
		return m, "Merge " + la + " + " + lb, r == nil
	case k < 10:
		h, lh, _, okh := w.cold2Hold()
		b, lb, _, okb := w.coldStep(rng)
		if !okh || !okb {
			return Plan{}, "", false
		}
		m, r := Merge(b, h)
		return m, "Merge " + lb + " + " + lh, r == nil
	case k < 16:
		return w.cold3DropRelink(rng)
	}
	plan, label, _, ok := w.cold2Step(rng)
	return plan, label, ok
}

// cold3Drive takes one producer one legal step down its path, so the walk
// goes deep into review: start, take, report (ok, fail or fault, with one of
// three heads, so an attempt can push a head read before), apply the report,
// ask reads, report them ok or fix, then ReadYes, ReadFix or WaiveRed, and
// land or go red.
func (w *world) cold3Drive(rng *rand.Rand) (Plan, string, bool) {
	all, cons := w.all()
	var active []string
	for id, c := range all {
		if c.Kind == "producer" && slices.Contains([]string{Ready, Working, Review, Merging}, c.State) {
			active = append(active, id)
		}
	}
	if len(active) == 0 {
		return Plan{}, "", false
	}
	sort.Strings(active)
	p := all[active[rng.Intn(len(active))]]
	rs, stream, own := w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), cons[p.ID]
	switch p.State {
	case Ready, Working:
		return cold3Work(rng, p, own, rs, stream, all)
	case Merging:
		return cold3Land(rng, p, own, rs, stream, all)
	default:
		return cold3Review(rng, p, own, rs, stream, all)
	}
}

func cold3Work(rng *rand.Rand, p Card, cons []Card, rs map[string]Row, stream Stream, needs map[string]Card) (Plan, string, bool) {
	if p.State == Ready && p.Red && rng.Intn(2) == 0 {
		return cold3Move(p, cons, rs, stream, needs, WaiveRed{Reason: "the red was not the code"})
	}
	if p.State == Ready {
		row := []string{"r1", "r2", "r3"}[rng.Intn(3)]
		return cold3Move(p, cons, rs, stream, needs, Start{Row: row, Branch: "b-" + p.ID, Base: base})
	}
	c := needs[workID(p.ID, p.Attempt)]
	switch c.State {
	case Working:
		h := []string{h1, h2, coldH3}[rng.Intn(3)]
		res := []string{"ok", "ok", "fail", "fault"}[rng.Intn(4)]
		return cold3Consume(c, p, rs, Report{Row: c.Holder, Result: res, Head: h, Reason: "r"})
	case Done:
		ev := map[string]Event{"ok": Finished{Consumer: c.ID}, "fail": Broken{Consumer: c.ID}, "fault": Faulted{Consumer: c.ID}}[c.Result]
		return cold3Move(p, cons, rs, stream, needs, ev)
	}
	return Plan{}, "", false
}

func cold3Land(rng *rand.Rand, p Card, cons []Card, rs map[string]Row, stream Stream, needs map[string]Card) (Plan, string, bool) {
	e := Event(Landed{Head: p.Head})
	if rng.Intn(3) == 0 {
		e = Red{Head: p.Head, Reason: "red"}
	}
	return cold3Move(p, cons, rs, stream, needs, e)
}

func cold3Review(rng *rand.Rand, p Card, cons []Card, rs map[string]Row, stream Stream, needs map[string]Card) (Plan, string, bool) {
	var okRead, fixRead, prev bool
	for _, c := range cons {
		switch {
		case c.Kind != "read":
		case live(c):
			res := []string{"ok", "fix"}[rng.Intn(2)]
			return cold3Consume(c, p, rs, Report{Row: c.Holder, Result: res, Head: p.Head, Reason: "finding " + fmt.Sprint(rng.Intn(3))})
		case c.State == Done && c.Review == p.Review && c.Result == "ok":
			okRead = true
		case c.State == Done && c.Review == p.Review && c.Result == "fix":
			fixRead = true
		case c.State == Done && c.Review == p.Review-1 && c.Result == "ok" && c.Head == p.Head:
			prev = true
		}
	}
	return cold3ReviewMove(rng, p, cons, rs, stream, needs, okRead, fixRead, prev)
}

func cold3ReviewMove(rng *rand.Rand, p Card, cons []Card, rs map[string]Row, stream Stream, needs map[string]Card, okRead, fixRead, prev bool) (Plan, string, bool) {
	var evs []Event
	if okRead {
		evs = append(evs, ReadYes{})
	}
	if fixRead {
		evs = append(evs, ReadFix{Finding: "fix " + fmt.Sprint(rng.Intn(3))})
	}
	if prev {
		evs = append(evs, WaiveRed{Reason: "the red was not the code"})
	}
	if len(evs) == 0 || rng.Intn(4) == 0 {
		return cold3Ask(rng, p, cons, rs, stream, needs)
	}
	return cold3Move(p, cons, rs, stream, needs, evs[rng.Intn(len(evs))])
}

func cold3Ask(rng *rand.Rand, p Card, cons []Card, rs map[string]Row, stream Stream, needs map[string]Card) (Plan, string, bool) {
	var open []string
	for _, r := range []string{"r1", "r2", "r3"} {
		if r != p.Author && rs[r].State == Up && !rs[r].Held {
			open = append(open, r)
		}
	}
	if len(open) == 0 {
		return Plan{}, "", false
	}
	return cold3Move(p, cons, rs, stream, needs, AskReads{Rows: open[:1+rng.Intn(len(open))]})
}

func cold3Move(p Card, cons []Card, rs map[string]Row, stream Stream, needs map[string]Card, e Event) (Plan, string, bool) {
	plan, r := Move(p, cons, rs, stream, needs, e)
	return plan, name(e) + " " + p.ID, r == nil
}

func cold3Consume(c, p Card, rs map[string]Row, e Event) (Plan, string, bool) {
	plan, r := MoveConsumer(c, p, rs[c.Holder], e)
	return plan, name(e) + " " + c.ID, r == nil
}

// cold3DropRelink picks one or two waiting dependants, drops (Cancel) a need
// of each that has not failed, and relinks each need, InThisOp when the drop
// is in the op; the second dependant is relinked to the first one time in
// two, and the first to the second one time in two.
func (w *world) cold3DropRelink(rng *rand.Rand) (Plan, string, bool) {
	all, cons := w.all()
	var deps, prods []string
	for id, c := range all {
		if !consumer(c.Kind) && c.State == Waiting && len(c.Needs) > 0 {
			deps = append(deps, id)
		}
		if c.Kind == "producer" && !final(c.Kind, c.State) {
			prods = append(prods, id)
		}
	}
	if len(deps) == 0 || len(prods) == 0 {
		return Plan{}, "", false
	}
	sort.Strings(deps)
	sort.Strings(prods)
	picked := []string{deps[rng.Intn(len(deps))]}
	if rng.Intn(2) == 0 {
		picked = append(picked, deps[rng.Intn(len(deps))])
	}
	stream := StreamFromStore(w.snap().Streams["s1"])
	var plans []Plan
	dropped := map[string]bool{}
	for k, id := range picked {
		d := all[id]
		n := d.Needs[rng.Intn(len(d.Needs))]
		if !strings.HasPrefix(n, "a") && rng.Intn(5) > 1 {
			return Plan{}, "", false // mostly drop added cards, so p1..p4 keep the walk alive
		}
		if !dropped[n] && all[n].State != Fail {
			drop, r := Move(all[n], cons[n], w.rows(), stream, all, Cancel{Reason: "dropped"})
			if r != nil {
				return Plan{}, "", false
			}
			plans = append(plans, drop)
			dropped[n] = true
		}
		to := prods[rng.Intn(len(prods))]
		if len(picked) == 2 && rng.Intn(2) == 0 {
			to = picked[1-k]
		}
		rl, r := Move(d, cons[d.ID], w.rows(), stream, all, Relink{Need: n, To: to, Reason: "relinked", InThisOp: dropped[n]})
		if r != nil {
			return Plan{}, "", false
		}
		plans = append(plans, rl)
	}
	m, r := Merge(plans...)
	return m, "Drop+Relink " + strings.Join(picked, ","), r == nil
}

// cold3Violations are the design's invariants over the step from before to
// after, each as {kind, detail}; a kind prefixed "note: " is reported, not
// failed. entry keeps how each producer last entered review.
func cold3Violations(before, after store.Snapshot, entry map[string]string) [][2]string {
	var out [][2]string
	bad := func(kind, format string, args ...any) {
		out = append(out, [2]string{kind, fmt.Sprintf(format, args...)})
	}
	if loop := cold3Cycle(after.Cards); loop != nil {
		bad("needs/after cycle", "%s", strings.Join(loop, " -> "))
	}
	working, liveWork := map[string]int{}, map[string]int{}
	for _, c := range after.Cards {
		was, existed := before.Cards[c.ID]
		if existed && final(was.Kind, was.State) && (c.State != was.State || c.Gen != was.Gen) {
			bad("final card moved", "%s %s -> %s", c.ID, was.State, c.State)
		}
		if !consumer(c.Kind) {
			cold3Producer(before, after, c, entry, bad)
			continue
		}
		p := after.Cards[c.Of]
		if !existed && before.Streams[c.Stream].Held {
			bad("consumer cut in a held stream", "%s", c.ID)
		}
		if !existed && after.Streams[c.Stream].Held {
			bad("note: consumer cut in the op that held its stream", "%s", c.ID)
		}
		if c.State == Working {
			working[c.Holder]++
		}
		if c.State != Ready && c.State != Working {
			continue
		}
		if c.Kind == "work" {
			liveWork[c.Of]++
			if p.State != Working || c.Attempt != p.Attempt {
				bad("live work under a producer not working at its attempt", "%s under %s %s", c.ID, p.ID, p.State)
			}
		} else if p.State != Review || c.Review != p.Review || c.Head != p.Head {
			bad("live read under a producer not in its review", "%s under %s %s", c.ID, p.ID, p.State)
		}
	}
	for p, n := range liveWork {
		if n > 1 {
			bad("two live work consumers", "%s has %d", p, n)
		}
	}
	for name, n := range working {
		if n > worldWidth[name] {
			bad("row over width", "%s holds %d working, width %d", name, n, worldWidth[name])
		}
	}
	return out
}

// cold3Producer: ready never in a held stream; working always with a live or
// reported consumer of its attempt; attempt rises only by Start, by one; a
// fault or a withdraw spends no bounce, a Broken one; merging only on an ok
// read of this review, or by WaiveRed after a Red.
func cold3Producer(before, after store.Snapshot, p store.Card, entry map[string]string, bad func(string, string, ...any)) {
	if p.State == Ready && after.Streams[p.Stream].Held {
		bad("ready in a held stream", "%s in %s", p.ID, p.Stream)
	}
	if p.Kind == "producer" && p.State == Working {
		if c, ok := after.Cards[workID(p.ID, p.Attempt)]; !ok || c.State == Fail {
			bad("working with no consumer of its attempt", "%s attempt %d", p.ID, p.Attempt)
		}
	}
	was, existed := before.Cards[p.ID]
	if !existed {
		return
	}
	started := was.State == Ready && p.State == Working
	if p.Attempt != was.Attempt && !(started && p.Attempt == was.Attempt+1) {
		bad("attempt moved without a Start", "%s %d -> %d (%s -> %s)", p.ID, was.Attempt, p.Attempt, was.State, p.State)
	}
	if was.State == Working && (p.State == Ready || p.State == Waiting) {
		c := after.Cards[workID(p.ID, was.Attempt)]
		want := was.Bounces
		if c.State == Done && c.Result == "fail" {
			want++
		}
		if p.Bounces != want {
			bad("bounces wrong on a return", "%s %d -> %d, consumer %s %s", p.ID, was.Bounces, p.Bounces, c.State, c.Result)
		}
		if c.State == Done && c.Result == "fault" && was.Finding != "" && p.Finding != was.Finding {
			bad("note: a fault rewrote the reader's finding", "%s %q -> %q", p.ID, was.Finding, p.Finding)
		}
	}
	if p.State == Review && was.State != Review {
		entry[p.ID] = map[string]string{Working: "Finished", Merging: "Red"}[was.State]
	}
	if p.State == Merging && was.State == Review && !cold3OkRead(after, p, p.Review) {
		if entry[p.ID] != "Red" || !cold3OkRead(after, p, p.Review-1) {
			bad("merging without a read of this review, not after a Red", "%s review %d entered by %q", p.ID, p.Review, entry[p.ID])
		}
	}
}

func cold3OkRead(s store.Snapshot, p store.Card, review int) bool {
	return slices.ContainsFunc(coldValues(s.Cards), func(c store.Card) bool {
		return c.Of == p.ID && c.Kind == "read" && c.Review == review && c.Head == p.Head && c.State == Done && c.Result == "ok"
	})
}

// cold3Cycle is a cycle through needs and after in cards, or nil.
func cold3Cycle(cards map[string]store.Card) []string {
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
		for _, n := range append(slices.Clone(cards[id].Needs), cards[id].After...) {
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
