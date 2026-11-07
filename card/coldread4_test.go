package card

// The fourth cold read of layer 2, at eb3051a5 (v2-card-fix4). Two kinds of
// test:
//
//   - TestColdread4Defect*: one per confirmed defect; each FAILS at eb3051a5
//     and asserts the design line it names. A probe returns early (passes)
//     when the machine refuses the merge, since a refusal keeps the store
//     legal.
//   - TestColdread4Walk: the random walk of the three earlier reads, with
//     every step a move planned from a fresh snapshot (so the same moves can
//     be re-planned one by one), and merged plans of every kind: two random
//     moves, a hold with a move (both orders), a drop with relinks, a Red
//     then a WaiveRed, a preempt (some on the wrong row), a review move with
//     a read cut in the same op, and a nested Merge. Each merged op the
//     machine accepts is also applied one by one on a replica of the store
//     (the op log replayed), and the two are compared: "Merge is sequential
//     composition" (LAYER-2, third cold read rulings).
//
// Unit tests, no clock, no sockets: rand is seeded, At is t0 plus the op's
// number.

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cold4Review is p1 in review 1 at h1, its head written by r1.
func cold4Review(t *testing.T) *world {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	return w
}

// cold4LiveReads is the live read consumers of p in s.
func cold4LiveReads(s store.Snapshot, p string) []string {
	var out []string
	for _, c := range s.Cards {
		if c.Of == p && c.Kind == "read" && (c.State == Ready || c.State == Working) {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// ---- confirmed defects: each FAILS at eb3051a5 ----

// D1. "a producer ... has a live read consumer only in review"; Cancel,
// Block, ReadYes and ReadFix need "no live consumer of p"; "an Edit never
// lands while a consumer is live" (LAYER-2 tables and checks). Merge re-runs
// a later move with the consumer list it was planned with (overlayCons in
// replay.go overlays only the consumers passed), so a read cut by an earlier
// plan of the same merge is invisible to it. AskRead then Cancel, Block,
// Edit, ReadFix or ReadYes on the same producer: the machine accepts, the
// store accepts, and the producer leaves review (or is edited) with a live
// read. Applied one by one, the second move is refused "withdraw it first".
func TestColdread4DefectMergedCutIsInvisibleToTheNextMove(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, read string
		e          Event
	}{
		{"Cancel", "", Cancel{Reason: "dropped"}},
		{"Block", "", Block{Reason: "bounced"}},
		{"Edit", "", Edit{Prose: "new scope", Reason: "scope"}},
		{"ReadFix", "fix", ReadFix{Finding: "rename x"}},
		{"ReadYes", "ok", ReadYes{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := cold4Review(t)
			if c.read != "" {
				w.read("p1", "r2", c.read, "finding")
			}
			ask, r := w.plan("p1", AskRead{Row: "r3"})
			require.Nil(t, r)
			second, r := w.plan("p1", c.e)
			require.Nil(t, r)
			// One by one, the second move sees the read and refuses.
			merged, mr := Merge(ask, second)
			if mr != nil {
				return // refused: the store stays legal
			}
			rc, err := w.apply("askread+"+c.name, merged)
			require.NoError(t, err)
			if !rc.Accepted {
				return
			}
			s := w.snap()
			live := cold4LiveReads(s, "p1")
			if c.name == "Edit" {
				assert.False(t, len(live) > 0 && s.Cards["p1"].Prose == "new scope", "Edit landed while read %v is live", live)
				return
			}
			assert.False(t, len(live) > 0 && s.Cards["p1"].State != Review, "p1 is %s with live read %v", s.Cards["p1"].State, live)
		})
	}
}

// D2. "No producer is ready in a held stream after any step" (card model,
// ParkedUnderHold; LAYER-2 rulings from the card model). Merge re-runs a
// Withdraw with the Stream carried in its event (sealConsumer in replay.go
// passes e unchanged), not the stream the plans ahead of it wrote. A hold
// merged before a releasing Withdraw: the withdraw is re-run, still sees the
// stream free, fences StreamHeld false (true before the op) and writes p1
// ready. The op holds s1 and leaves p1 ready in it. Broken, Faulted,
// ReadFix and Released re-read the stream and park.
func TestColdread4DefectMergedHoldThenWithdrawReadiesInAHeldStream(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
	w.consume("p1.w1", Take{Row: "r1"})
	s := w.snap()
	hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Seq: s.Seq})
	require.Nil(t, r)
	wd := w.consumerPlan("p1.w1", Withdraw{Reason: "silent"})
	merged, mr := Merge(hold, wd)
	if mr != nil {
		return
	}
	rc, err := w.apply("hold+withdraw", merged)
	require.NoError(t, err)
	got := w.snap()
	assert.False(t, rc.Accepted && got.Streams["s1"].Held && got.Cards["p1"].State == Ready,
		"one merged op held s1 and left p1 ready in it (reason %q)", got.Cards["p1"].Reason)
}

// D3. "Merge is sequential composition ... plan 2's move is re-run against
// that state ... two relinks cannot close a cycle" (third cold read
// rulings); README: "each later plan the machine built is run again". A
// merged plan is built by the machine, but Merge returns it with no move to
// run again (mergeSeq leaves again nil), so Merge treats it as hand-built
// and keeps its writes. Merge(one, Merge(two)) of the third read's two
// relinks lands the cycle p1 needs x needs y needs z needs p1.
func TestColdread4DefectNestedMergeSkipsTheReRun(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("a"), producer("b"), producer("y", "b"), producer("x", "y"), producer("p1", "a"), producer("z", "p1"))
	w.move("a", Cancel{Reason: "dropped"})
	w.move("b", Cancel{Reason: "dropped"})
	one, r := w.plan("p1", Relink{Need: "a", To: "x", Reason: "x does it"})
	require.Nil(t, r)
	two, r := w.plan("y", Relink{Need: "b", To: "z", Reason: "z does it"})
	require.Nil(t, r)
	_, flat := Merge(one, two)
	require.NotNil(t, flat, "the flat merge refuses the loop (the third read's fix)")
	inner, r := Merge(two)
	require.Nil(t, r)
	merged, mr := Merge(one, inner)
	if mr != nil {
		return
	}
	rc, err := w.apply("relinks", merged)
	require.NoError(t, err)
	assert.False(t, rc.Accepted, "a nested Merge passed the two relinks the flat Merge refuses")
	assert.Empty(t, coldCycle(w.snap().Cards), "a needs cycle landed through one nested merged op")
}

// D4. Merge is sequential composition (third cold read rulings), and "the
// same checks as a single plan" so no plan it emits is refused for its own
// shape. A later move re-run on the folded state fences what it read there,
// but layer 1 checks every condition before any write (store/apply.go run):
// a hold merged before a Broken emits StreamHeld false (the hold's) and
// StreamHeld true (the Broken's park), which no store state satisfies. The
// machine accepts, the store always refuses, and one by one (hold, then
// Broken) is accepted and parks p1. The same with UnholdStream before a
// Broken in a held stream, and with Faulted, ReadFix, Released and Unblock.
func TestColdread4DefectMergedFencesContradict(t *testing.T) {
	t.Parallel()
	t.Run("HoldThenBroken", func(t *testing.T) {
		w := newWorld(t, producer("p1"))
		w.move("p1", Resolve{})
		c := w.work("p1", "r1", "fail", h1, "tests red")
		s := w.snap()
		hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Seq: s.Seq})
		require.Nil(t, r)
		broken, r := w.plan("p1", Broken{Consumer: c})
		require.Nil(t, r)
		cold4Contradiction(t, w, hold, broken, Waiting)
	})
	t.Run("UnholdThenBroken", func(t *testing.T) {
		w := newWorld(t, producer("p1"))
		w.move("p1", Resolve{})
		w.move("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
		s := w.snap()
		hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Seq: s.Seq})
		require.Nil(t, r)
		w.op("hold", hold)
		w.consume("p1.w1", Take{Row: "r1"})
		w.consume("p1.w1", Report{Row: "r1", Result: "fail", Head: h1, Reason: "tests red"})
		unhold, r := MoveStream(StreamFromStore(w.snap().Streams["s1"]), UnholdStream{Reason: "thaw"})
		require.Nil(t, r)
		broken, r := w.plan("p1", Broken{Consumer: "p1.w1"})
		require.Nil(t, r)
		cold4Contradiction(t, w, unhold, broken, Ready)
	})
}

func cold4Contradiction(t *testing.T, w *world, first, second Plan, want string) {
	t.Helper()
	merged, mr := Merge(first, second)
	if mr != nil {
		return // refused by the machine: not this defect
	}
	held := map[bool]bool{}
	for _, e := range merged.Expect {
		if h, ok := e.(store.StreamHeld); ok && h.Stream == "s1" {
			held[h.Held] = true
		}
	}
	assert.False(t, held[true] && held[false], "the merged plan fences s1 both held and not held: %+v", merged.Expect)
	rc, err := w.apply("merged", merged)
	require.NoError(t, err)
	assert.True(t, rc.Accepted, "the store refused a merged plan the machine accepted and that applies one by one: %s", rc.Reason)
	if rc.Accepted {
		assert.Equal(t, want, w.card("p1").State)
	}
}

// D5. "any refusal refuses the whole merge naming both plans" (third cold
// read rulings; the author's claim at this head). The second-write refusal
// (dupWrites, checked after the re-runs) names the field and not the plans:
// two AskReads of one head are refused "a second write to card p1.r1.1
// field kind; ...", with no "AskRead p1" in it. (coldread2_test.go pins that
// text from round 3, before this ruling.)
func TestColdread4DefectSecondWriteRefusalNamesNoPlan(t *testing.T) {
	t.Parallel()
	w := cold4Review(t)
	a, r := w.plan("p1", AskRead{Row: "r2"})
	require.Nil(t, r)
	b, r := w.plan("p1", AskRead{Row: "r3"})
	require.Nil(t, r)
	_, mr := Merge(a, b)
	require.NotNil(t, mr)
	assert.Contains(t, mr.Reason, "AskRead p1", "the merge refusal names no plan: %s", mr.Reason)
}

// ---- the random walk, every merge kind, checked against one by one ----

// cold4Step is one move, planned from w's current snapshot, so a merge of
// steps can be re-planned one by one on a replica.
type cold4Step struct {
	label string
	plan  func(w *world) (Plan, *Refusal)
}

func cold4P(id string, e Event) cold4Step {
	return cold4Step{name(e) + " " + id, func(w *world) (Plan, *Refusal) {
		all, cons := w.all()
		p, ok := all[id]
		if !ok {
			return Plan{}, no("no card %s", id)
		}
		return Move(p, cons[id], w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), all, e)
	}}
}

func cold4C(id string, e Event) cold4Step {
	return cold4Step{name(e) + " " + id, func(w *world) (Plan, *Refusal) {
		all, _ := w.all()
		c, ok := all[id]
		if !ok {
			return Plan{}, no("no card %s", id)
		}
		p := all[c.Of]
		ev := e
		if v, ok := ev.(Withdraw); ok { // the producer's stream, as layer 3 reads it
			v.Stream = StreamFromStore(w.snap().Streams[p.Stream])
			ev = v
		}
		return MoveConsumer(c, p, w.rows()[c.Holder], ev)
	}}
}

func cold4Hold() cold4Step {
	return cold4Step{"HoldStream s1", func(w *world) (Plan, *Refusal) {
		s := w.snap()
		var ready []Card
		for _, c := range s.Cards {
			if c.Kind == "producer" && c.State == Ready && c.Stream == "s1" {
				ready = append(ready, FromStore(c))
			}
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
		return MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Ready: ready, Seq: s.Seq})
	}}
}

func cold4Unhold() cold4Step {
	return cold4Step{"UnholdStream s1", func(w *world) (Plan, *Refusal) {
		return MoveStream(StreamFromStore(w.snap().Streams["s1"]), UnholdStream{Reason: "thaw"})
	}}
}

func cold4Add(id string, needs []string) cold4Step {
	return cold4Step{"Add " + id, func(w *world) (Plan, *Refusal) {
		all, _ := w.all()
		return MoveAdd(Add{ID: id, Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do " + id, Needs: needs}, all)
	}}
}

func cold4R(row string, e Event) cold4Step {
	return cold4Step{name(e) + " " + row, func(w *world) (Plan, *Refusal) { return MoveRow(w.rows()[row], e) }}
}

// cold4Run is a walk: its world, the op log (every op applied, refused
// ones too, since they take a seq), and the seed cards to rebuild a replica.
type cold4Run struct {
	t     *testing.T
	w     *world
	log   []store.Op
	cards []store.PutCard
}

func (r *cold4Run) apply(w *world, kind string, plan Plan, log bool) (store.Receipt, error) {
	if reason := w.overWidth(plan); reason != "" { // the dealer's check (round 22), as world.apply
		return store.Receipt{Reason: reason}, nil
	}
	w.n++
	op := store.Op{ID: fmt.Sprintf("op%d", w.n), Epoch: w.snap().Epoch, Actor: "card", Kind: kind, At: t0.Add(time.Duration(w.n) * time.Second),
		Expect: plan.Expect, Writes: plan.Writes}
	rc, err := w.f.Apply(context.Background(), op)
	if log && err == nil {
		r.log = append(r.log, op)
	}
	return rc, err
}

// replica is a world holding what r.w held before the op now being checked.
func (r *cold4Run) replica() *world {
	rep := newWorld(r.t, r.cards...)
	for _, op := range r.log {
		_, err := rep.f.Apply(context.Background(), op)
		require.NoError(r.t, err)
	}
	rep.n = r.w.n
	return rep
}

// sequential applies steps one by one on rep, each planned from the state
// the one before left: "" when every one is accepted, else why not.
func (r *cold4Run) sequential(rep *world, steps []cold4Step) string {
	for _, s := range steps {
		plan, ref := s.plan(rep)
		if ref != nil {
			return "machine refused " + s.label + ": " + ref.Reason
		}
		rc, err := r.apply(rep, s.label, plan, false)
		if err != nil {
			return "store found " + s.label + " invalid: " + err.Error()
		}
		if !rc.Accepted {
			return "store refused " + s.label + ": " + rc.Reason
		}
	}
	return ""
}

// cold4Diff is how two snapshots' cards, rows and streams differ, gens aside.
func cold4Diff(a, b store.Snapshot) string {
	ids := map[string]bool{}
	for id := range a.Cards {
		ids[id] = true
	}
	for id := range b.Cards {
		ids[id] = true
	}
	var keys []string
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		x, okx := a.Cards[id]
		y, oky := b.Cards[id]
		if okx != oky {
			return fmt.Sprintf("card %s exists in one only", id)
		}
		cx, cy := FromStore(x), FromStore(y)
		cx.Gen, cy.Gen = 0, 0
		if !reflect.DeepEqual(cx, cy) {
			return fmt.Sprintf("card %s: merged %+v, one by one %+v", id, cx, cy)
		}
	}
	for n, s := range a.Streams {
		if b.Streams[n].Held != s.Held {
			return fmt.Sprintf("stream %s held %v vs %v", n, s.Held, b.Streams[n].Held)
		}
	}
	for n, x := range a.Rows {
		rx, ry := RowFromStore(x, 0, nil), RowFromStore(b.Rows[n], 0, nil)
		rx.Gen, ry.Gen = 0, 0
		if !reflect.DeepEqual(rx, ry) {
			return fmt.Sprintf("row %s: %+v vs %+v", n, rx, ry)
		}
	}
	return ""
}

// The one-by-one check replays the op log on a fresh Fake: one merged op in
// cold4CheckOneIn, in the first cold4CheckSteps steps of each seed.
const cold4CheckSteps, cold4CheckOneIn = 1000, 1

func TestColdread4Walk(t *testing.T) {
	t.Parallel()
	seeds, steps := walkSeeds(60, 6), 1000
	var mu sync.Mutex
	accepted, kinds := map[string]int{}, map[string]int{}
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
				acc, kd, br, nt := cold4Walk(t, seed, steps)
				mu.Lock()
				defer mu.Unlock()
				for k, n := range acc {
					accepted[k] += n
				}
				for k, n := range kd {
					kinds[k] += n
				}
				fold(broken, br)
				fold(notes, nt)
			})
		}
	})
	t.Logf("accepted ops by first event over %d seeds x %d steps: %v", seeds, steps, accepted)
	var ks []string
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		t.Logf("merge %s: %d", k, kinds[k])
	}
	for _, k := range []string{"two", "hold", "drop", "preempt", "review", "nested"} {
		assert.Positive(t, kinds[k+" accepted"], "the walk never landed a %s merge", k)
	}
	assert.Positive(t, kinds["red+waive tried"], "the walk never tried a Red then WaiveRed")
	for _, k := range cold3Keys(notes) {
		t.Logf("note %q: %d seeds, %d steps; first: %s", k, notes[k].seeds, notes[k].steps, notes[k].first)
	}
	for _, k := range cold3Keys(broken) {
		t.Errorf("invariant %q broken: %d seeds, %d steps; first: %s", k, broken[k].seeds, broken[k].steps, broken[k].first)
	}
}

func cold4Walk(t *testing.T, seed int64, steps int) (map[string]int, map[string]int, map[string]*cold3Break, map[string]*cold3Break) {
	rng := rand.New(rand.NewSource(seed))
	cards := []store.PutCard{producer("p1"), producer("p2", "p1"), producer("p3"), producer("p4", "p3"), {ID: "g1", Kind: "sentinel", Needs: []string{"p2"}}}
	run := &cold4Run{t: t, cards: cards}
	run.w = newWorld(t, cards...)
	w := run.w
	accepted, kinds := map[string]int{}, map[string]int{}
	broken, notes := map[string]*cold3Break{}, map[string]*cold3Break{}
	entry := map[string]string{}
	for i := 0; i < steps; i++ {
		before := w.snap()
		kind, stepList, plan, status := w.cold4Pick(rng)
		if kind != "" {
			kinds[kind+" tried"]++
		}
		switch status {
		case "none":
			continue
		case "step":
			if kind != "" {
				kinds[kind+" a move refused from the snapshot"]++
			}
			continue
		case "merge":
			kinds[kind+" refused by the machine"]++
			continue
		}
		cold4Land(run, rng, seed, i, before, kind, stepList, plan, accepted, kinds, broken, notes, entry)
	}
	return accepted, kinds, broken, notes
}

// cold4Land applies one accepted plan and compares a merged op with the same
// moves applied one by one.
func cold4Land(run *cold4Run, rng *rand.Rand, seed int64, i int, before store.Snapshot, kind string, stepList []cold4Step, plan Plan, accepted, kinds map[string]int, broken, notes map[string]*cold3Break, entry map[string]string) {
	w := run.w
	where := fmt.Sprintf("seed %d step %d %s", seed, i, cold4Labels(stepList))
	rc, err := run.apply(w, kind+":"+cold4Labels(stepList), plan, true)
	if err != nil {
		cold3Note(broken, "the store found a machine plan invalid", where+": "+err.Error())
		return
	}
	cold4AgainstOne(run, rng, i, kind, stepList, rc, where, kinds, broken)
	if !rc.Accepted {
		switch {
		case strings.Contains(rc.Reason, "width"):
		case kind != "":
			kinds[kind+" refused by the store"]++
		default:
			cold3Note(broken, "store refused a single machine plan", where+": "+rc.Reason)
		}
		return
	}
	if kind != "" {
		kinds[kind+" accepted"]++
	}
	accepted[strings.Fields(stepList[0].label)[0]]++
	for _, v := range cold3Violations(before, w.snap(), entry) {
		if strings.HasPrefix(v[0], "note: ") {
			cold3Note(notes, strings.TrimPrefix(v[0], "note: "), where+": "+v[1])
		} else {
			cold3Note(broken, v[0], where+": "+v[1])
		}
	}
	for _, v := range cold4Violations(before, w.snap(), plan, kind, stepList, entry) {
		cold3Note(broken, v[0], where+": "+v[1])
	}
}

// cold4AgainstOne replays a merged op as separate moves and compares.
func cold4AgainstOne(run *cold4Run, rng *rand.Rand, i int, kind string, stepList []cold4Step, rc store.Receipt, where string, kinds map[string]int, broken map[string]*cold3Break) {
	if kind == "" || i >= cold4CheckSteps || rng.Intn(cold4CheckOneIn) != 0 { // replay cost grows with the log
		return
	}
	kinds[kind+" checked one by one"]++
	rep := run.replicaBefore()
	seq := run.sequential(rep, stepList)
	pair := " (" + kind + ": " + cold4Events(stepList) + ")"
	w := run.w
	if seq == "" && rc.Accepted {
		if d := cold4Diff(w.snap(), rep.snap()); d != "" {
			cold3Note(broken, "a merged op differs from its plans applied one by one"+pair, where+": "+d)
		}
	}
	if seq != "" && rc.Accepted {
		cold3Note(broken, "a merged op landed what one by one refuses"+pair, where+": "+seq)
	}
	if seq == "" && !rc.Accepted && !strings.Contains(rc.Reason, "width") {
		cold3Note(broken, "the store refused a merged op that applies one by one"+pair, where+": "+rc.Reason)
	}
}

// replicaBefore is the replica without the last logged op: the state the
// merged op was planned on.
func (r *cold4Run) replicaBefore() *world {
	last := r.log[len(r.log)-1]
	r.log = r.log[:len(r.log)-1]
	rep := r.replica()
	r.log = append(r.log, last)
	rep.n = r.w.n
	return rep
}

// cold4Events is the event names of steps, in order: "HoldStream+Withdraw".
func cold4Events(steps []cold4Step) string {
	var ns []string
	for _, s := range steps {
		ns = append(ns, strings.Fields(s.label)[0])
	}
	return strings.Join(ns, "+")
}

func cold4Labels(steps []cold4Step) string {
	var ls []string
	for _, s := range steps {
		ls = append(ls, s.label)
	}
	return strings.Join(ls, " + ")
}

// cold4Violations are this read's invariants over the step: a red only on a
// producer whose last entry into review was a Red (Start and Finished clear
// it); review rises by at most one; a fault writes no finding; every state
// write is to a card of the snapshot or one the plan puts; a preempt
// withdraws a working consumer.
func cold4Violations(before, after store.Snapshot, plan Plan, kind string, steps []cold4Step, entry map[string]string) [][2]string {
	var out [][2]string
	bad := func(k, format string, args ...any) { out = append(out, [2]string{k, fmt.Sprintf(format, args...)}) }
	for _, p := range after.Cards {
		if p.Kind != "producer" {
			continue
		}
		if p.Red && p.State == Working {
			bad("red survived a Start", "%s attempt %d", p.ID, p.Attempt)
		}
		if p.Red && p.State == Review && entry[p.ID] == "Finished" {
			bad("red survived a Finished", "%s review %d", p.ID, p.Review)
		}
		was, ok := before.Cards[p.ID]
		if !ok {
			continue
		}
		if p.Review > was.Review+1 {
			bad("review rose by more than one in an op", "%s %d -> %d", p.ID, was.Review, p.Review)
		}
		if was.State == Working && p.State != Working {
			if c := after.Cards[workID(p.ID, was.Attempt)]; c.Result == "fault" && p.Finding != was.Finding {
				bad("a fault wrote finding", "%s %q -> %q", p.ID, was.Finding, p.Finding)
			}
		}
	}
	put := map[string]bool{}
	for _, wr := range plan.Writes {
		if pc, ok := wr.(store.PutCard); ok {
			put[pc.ID] = true
		}
		if sc, ok := wr.(store.SetCardState); ok {
			if _, in := before.Cards[sc.Card]; !in && !put[sc.Card] {
				bad("a state write to a card not in the snapshot", "%s", sc.Card)
			}
		}
	}
	if kind == "preempt" {
		for _, wr := range plan.Writes {
			if sc, ok := wr.(store.SetCardState); ok && sc.State == Fail {
				if c, in := before.Cards[sc.Card]; in && consumer(c.Kind) && c.State != Working {
					bad("a preempt withdrew a consumer that was not working", "%s was %s", sc.Card, c.State)
				}
			}
		}
	}
	return out
}

// cold4Pick is one step of the walk: kind "" for a single move, else the
// merge kind; status "ok", "none" (nothing to try), "step" (a move refused
// from the snapshot) or "merge" (Merge refused).
func (w *world) cold4Pick(rng *rand.Rand) (string, []cold4Step, Plan, string) {
	var kind string
	var steps []cold4Step
	switch k := rng.Intn(100); {
	case k < 8:
		kind, steps = "two", []cold4Step{w.cold4One(rng), w.cold4One(rng)}
	case k < 16:
		kind, steps = "hold", w.cold4HoldPair(rng)
	case k < 22:
		kind, steps = "drop", w.cold4Drop(rng)
	case k < 26:
		kind, steps = "red+waive", w.cold4RedWaive()
	case k < 32:
		kind, steps = "preempt", w.cold4Preempt(rng)
	case k < 38:
		kind, steps = "review", w.cold4ReviewPair(rng)
	case k < 41:
		kind, steps = "nested", []cold4Step{w.cold4One(rng), w.cold4One(rng)}
	default:
		steps = []cold4Step{w.cold4One(rng)}
	}
	if len(steps) == 0 {
		return kind, nil, Plan{}, "none"
	}
	plans := make([]Plan, len(steps))
	for i, s := range steps {
		p, r := s.plan(w)
		if r != nil {
			return kind, steps, Plan{}, "step" // a move refused from the snapshot: no merge to try
		}
		plans[i] = p
	}
	if kind == "" {
		return "", steps, plans[0], "ok"
	}
	var m Plan
	var r *Refusal
	if kind == "nested" {
		if m, r = Merge(plans[1]); r == nil {
			m, r = Merge(plans[0], m)
		}
	} else {
		m, r = Merge(plans...)
	}
	if r != nil {
		return kind, steps, Plan{}, "merge"
	}
	return kind, steps, m, "ok"
}

// cold4One is one move: one time in two a drive step down a producer's path
// (cold3Drive's), else a random producer, consumer, row or stream event.
func (w *world) cold4One(rng *rand.Rand) cold4Step {
	if rng.Intn(2) == 0 {
		if s, ok := w.cold4Drive(rng); ok {
			return s
		}
	}
	all, cons := w.all()
	var prods, cids []string
	for id, c := range all {
		if consumer(c.Kind) {
			if !final(c.Kind, c.State) {
				cids = append(cids, id)
			}
		} else {
			prods = append(prods, id)
		}
	}
	sort.Strings(prods)
	sort.Strings(cids)
	row := []string{"r1", "r2", "r3"}[rng.Intn(3)]
	switch n := rng.Intn(20); {
	case n == 19:
		var need []string
		if rng.Intn(3) > 0 {
			need = []string{prods[rng.Intn(len(prods))]}
		}
		return cold4Add(fmt.Sprintf("a%d", len(all)), need)
	case n < 10:
		return w.cold4Prod(rng, all, cons, prods, row)
	case n < 15 && len(cids) > 0:
		c := all[cids[rng.Intn(len(cids))]]
		head := []string{all[c.Of].Head, h1, h2, coldH3}[rng.Intn(4)]
		if c.Kind == "read" {
			head = c.Head
		}
		res := []string{"ok", "ok", "fix", "fail", "fault"}[rng.Intn(5)]
		evs := []Event{Take{Row: c.Holder}, Report{Row: c.Holder, Result: res, Head: head, Reason: "r"}, Withdraw{Reason: "silent"}}
		return cold4C(c.ID, evs[rng.Intn(len(evs))])
	case n < 18:
		e := []Event{Seen{}, Gone{}, Hold{Reason: "h"}, Unhold{Reason: "u"}}[rng.Intn(4)]
		return cold4R(row, e)
	}
	if w.snap().Streams["s1"].Held {
		return cold4Unhold()
	}
	return cold4Hold()
}

func (w *world) cold4Prod(rng *rand.Rand, all map[string]Card, cons map[string][]Card, prods []string, row string) cold4Step {
	p := all[prods[rng.Intn(len(prods))]]
	cid := workID(p.ID, p.Attempt)
	if cs := cons[p.ID]; len(cs) > 0 && rng.Intn(4) == 0 {
		cid = cs[rng.Intn(len(cs))].ID
	}
	evs := []Event{Resolve{}, Park{}, Reached{}, Start{Row: row, Branch: "b-" + p.ID, Base: base}, Finished{Consumer: cid},
		Broken{Consumer: cid}, Faulted{Consumer: cid}, AskRead{Row: row}, ReadYes{}, ReadFix{Finding: "fix it"},
		WaiveRed{Reason: "not the code"}, Landed{Head: p.Head}, Red{Head: p.Head, Reason: "red"}, Released{},
		Block{Reason: "bounced"}, Unblock{Reason: "looked"}, Rescore{Score: rng.Intn(4)}, SetPriority{Level: Levels[rng.Intn(len(Levels))]},
		Edit{Prose: fmt.Sprint("prose ", rng.Intn(3))}}
	if rng.Intn(15) == 0 {
		evs = []Event{Cancel{Reason: "c"}, Replace{By: prods[rng.Intn(len(prods))], Reason: "r"}}
	}
	return cold4P(p.ID, evs[rng.Intn(len(evs))])
}

// cold4Drive is cold3Drive as a step.
func (w *world) cold4Drive(rng *rand.Rand) (cold4Step, bool) {
	all, cons := w.all()
	var active []string
	for id, c := range all {
		if c.Kind == "producer" && slices.Contains([]string{Waiting, Ready, Working, Review, Merging}, c.State) {
			active = append(active, id)
		}
	}
	if len(active) == 0 {
		return cold4Step{}, false
	}
	sort.Strings(active)
	p := all[active[rng.Intn(len(active))]]
	switch p.State {
	case Waiting:
		return cold4P(p.ID, Resolve{}), true
	case Ready:
		return cold4P(p.ID, Start{Row: []string{"r1", "r2", "r3"}[rng.Intn(3)], Branch: "b-" + p.ID, Base: base}), true
	case Working:
		c := all[workID(p.ID, p.Attempt)]
		switch c.State {
		case Ready:
			return cold4C(c.ID, Take{Row: c.Holder}), true
		case Working:
			h := []string{h1, h2, coldH3}[rng.Intn(3)]
			return cold4C(c.ID, Report{Row: c.Holder, Result: []string{"ok", "ok", "fail", "fault"}[rng.Intn(4)], Head: h, Reason: "r"}), true
		case Done:
			return cold4P(p.ID, map[string]Event{"ok": Finished{Consumer: c.ID}, "fail": Broken{Consumer: c.ID}, "fault": Faulted{Consumer: c.ID}}[c.Result]), true
		}
		return cold4Step{}, false
	case Merging:
		if rng.Intn(3) == 0 {
			return cold4P(p.ID, Red{Head: p.Head, Reason: "red"}), true
		}
		return cold4P(p.ID, Landed{Head: p.Head}), true
	}
	return w.cold4ReviewDrive(rng, p, cons[p.ID]), true
}

func (w *world) cold4ReviewDrive(rng *rand.Rand, p Card, cons []Card) cold4Step {
	var okRead, fixRead, prev bool
	for _, c := range cons {
		switch {
		case c.Kind != "read":
		case c.State == Ready:
			return cold4C(c.ID, Take{Row: c.Holder})
		case c.State == Working:
			return cold4C(c.ID, Report{Row: c.Holder, Result: []string{"ok", "fix"}[rng.Intn(2)], Head: p.Head, Reason: "finding"})
		case c.State == Done && c.Review == p.Review && c.Result == "ok":
			okRead = true
		case c.State == Done && c.Review == p.Review && c.Result == "fix":
			fixRead = true
		case c.State == Done && c.Review == p.Review-1 && c.Result == "ok" && c.Head == p.Head:
			prev = true
		}
	}
	evs := []Event{AskReads{Rows: cold4Readers(w, p, rng)}}
	if okRead {
		evs = append(evs, ReadYes{}, ReadYes{})
	}
	if fixRead {
		evs = append(evs, ReadFix{Finding: "fix " + fmt.Sprint(rng.Intn(3))})
	}
	if prev && p.Red {
		evs = append(evs, WaiveRed{Reason: "not the code"})
	}
	return cold4P(p.ID, evs[rng.Intn(len(evs))])
}

func cold4Readers(w *world, p Card, rng *rand.Rand) []string {
	rs := w.rows()
	var open []string
	for _, r := range []string{"r1", "r2", "r3"} {
		if r != p.Author && rs[r].State == Up && !rs[r].Held {
			open = append(open, r)
		}
	}
	if len(open) == 0 {
		return []string{"r1"}
	}
	return open[:1+rng.Intn(len(open))]
}

// cold4HoldPair is a hold (or an unhold of a held stream) with a move, in
// either order; the move is most often one that returns a producer to ready
// (a releasing Withdraw, Broken, Faulted, ReadFix, Resolve, Unblock).
func (w *world) cold4HoldPair(rng *rand.Rand) []cold4Step {
	h := cold4Hold()
	if w.snap().Streams["s1"].Held {
		h = cold4Unhold()
	}
	m := w.cold4One(rng)
	all, cons := w.all()
	var returns []cold4Step
	for _, p := range all {
		switch {
		case p.Kind != "producer":
		case p.State == Working:
			c := all[workID(p.ID, p.Attempt)]
			if live(c) {
				returns = append(returns, cold4C(c.ID, Withdraw{Reason: "silent"}))
			} else if c.State == Done {
				returns = append(returns, cold4P(p.ID, map[string]Event{"ok": Finished{Consumer: c.ID}, "fail": Broken{Consumer: c.ID}, "fault": Faulted{Consumer: c.ID}}[c.Result]))
			}
		case p.State == Waiting:
			returns = append(returns, cold4P(p.ID, Resolve{}))
		case p.State == Blocked:
			returns = append(returns, cold4P(p.ID, Unblock{Reason: "looked"}))
		case p.State == Review && len(cons[p.ID]) > 0:
			returns = append(returns, cold4P(p.ID, ReadFix{Finding: "f"}))
		}
	}
	sort.Slice(returns, func(i, j int) bool { return returns[i].label < returns[j].label })
	if len(returns) > 0 && rng.Intn(3) > 0 {
		m = returns[rng.Intn(len(returns))]
	}
	if rng.Intn(2) == 0 {
		return []cold4Step{h, m}
	}
	return []cold4Step{m, h}
}

// cold4Drop is cold3DropRelink as steps: a Cancel of a need of one or two
// waiting dependants, and a Relink (InThisOp) of each; sometimes a Waive or
// a NeedFailed instead of the Relink.
func (w *world) cold4Drop(rng *rand.Rand) []cold4Step {
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
		return nil
	}
	sort.Strings(deps)
	sort.Strings(prods)
	picked := []string{deps[rng.Intn(len(deps))]}
	if rng.Intn(2) == 0 {
		picked = append(picked, deps[rng.Intn(len(deps))])
	}
	var steps []cold4Step
	dropped := map[string]bool{}
	for k, id := range picked {
		d := all[id]
		n := d.Needs[rng.Intn(len(d.Needs))]
		if !strings.HasPrefix(n, "a") && rng.Intn(4) > 0 {
			return nil // mostly drop added cards, so p1..p4 keep the walk alive
		}
		if !dropped[n] && all[n].State != Fail {
			if len(cons[n]) > 0 && slices.ContainsFunc(cons[n], live) {
				return nil
			}
			steps = append(steps, cold4P(n, Cancel{Reason: "dropped"}))
			dropped[n] = true
		}
		to := prods[rng.Intn(len(prods))]
		if len(picked) == 2 && rng.Intn(2) == 0 {
			to = picked[1-k]
		}
		var e Event = Relink{Need: n, To: to, Reason: "relinked", InThisOp: dropped[n]}
		switch rng.Intn(5) {
		case 0:
			e = Waive{Need: n, Reason: "waived", InThisOp: dropped[n]}
		case 1:
			e = NeedFailed{Need: n, InThisOp: dropped[n]}
		}
		steps = append(steps, cold4P(id, e))
	}
	return steps
}

// cold4RedWaive is a Red then a WaiveRed of one merging producer.
func (w *world) cold4RedWaive() []cold4Step {
	all, _ := w.all()
	var ids []string
	for id, c := range all {
		if c.Kind == "producer" && c.State == Merging {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	p := all[ids[0]]
	return []cold4Step{cold4P(p.ID, Red{Head: p.Head, Reason: "ci red"}), cold4P(p.ID, WaiveRed{Reason: "not the code"})}
}

// cold4Preempt withdraws a live consumer "preempted by blocker b" and starts
// b; the row is the consumer's three times in four, another row otherwise,
// and the consumer is sometimes ready (the machine must refuse that).
func (w *world) cold4Preempt(rng *rand.Rand) []cold4Step {
	all, _ := w.all()
	var live, ready []string
	for id, c := range all {
		if consumer(c.Kind) && (c.State == Working || c.State == Ready) {
			live = append(live, id)
		}
		if c.Kind == "producer" && c.State == Ready {
			ready = append(ready, id)
		}
	}
	if len(live) == 0 || len(ready) == 0 {
		return nil
	}
	sort.Strings(live)
	sort.Strings(ready)
	c := all[live[rng.Intn(len(live))]]
	b := ready[rng.Intn(len(ready))]
	if b == c.Of {
		return nil
	}
	row := c.Holder
	if rng.Intn(4) == 0 {
		row = []string{"r1", "r2", "r3"}[rng.Intn(3)]
	}
	return []cold4Step{cold4C(c.ID, Withdraw{Reason: "preempted by blocker " + b, Preempt: true}), cold4P(b, Start{Row: row, Branch: "b-" + b, Base: base})}
}

// cold4ReviewPair is a read cut (AskRead or AskReads) of a producer in
// review merged with a second move of the same producer.
func (w *world) cold4ReviewPair(rng *rand.Rand) []cold4Step {
	all, _ := w.all()
	var ids []string
	for id, c := range all {
		if c.Kind == "producer" && c.State == Review {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	p := all[ids[rng.Intn(len(ids))]]
	rows := cold4Readers(w, p, rng)
	var ask Event = AskReads{Rows: rows}
	if len(rows) == 1 || rng.Intn(2) == 0 {
		ask = AskRead{Row: rows[0]}
	}
	second := []Event{Cancel{Reason: "dropped"}, Block{Reason: "bounced"}, Edit{Prose: fmt.Sprint("scope ", rng.Intn(9)), Reason: "scope"},
		ReadFix{Finding: "f"}, ReadYes{}, WaiveRed{Reason: "not the code"}, Rescore{Score: rng.Intn(4)}}[rng.Intn(7)]
	return []cold4Step{cold4P(p.ID, ask), cold4P(p.ID, second)}
}

// ---- ReportPlan (37047a14) ----

// cold4Taken is p1 working on r1 with p1.w1 taken; held holds s1 first.
func cold4Taken(t *testing.T, held bool) *world {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
	if held {
		s := w.snap()
		hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Seq: s.Seq})
		require.Nil(t, r)
		w.op("hold", hold)
	}
	w.consume("p1.w1", Take{Row: "r1"})
	return w
}

func (w *world) cold4ReportPlan(id string, rep Report) (Plan, *Refusal) {
	all, cons := w.all()
	c := all[id]
	p := all[c.Of]
	return ReportPlan(c, p, cons[p.ID], w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), all, rep)
}

// One plan equals Report then Finished, Broken or Faulted applied as two
// ops, in a free stream and a held one (the return parks), with and without
// a head, and on a fix attempt (prior head set): every card, the stream and
// the rows, gens included.
func TestColdread4ReportPlanEqualsOneByOne(t *testing.T) {
	t.Parallel()
	follow := map[string]func(string) Event{
		"ok":    func(id string) Event { return Finished{Consumer: id} },
		"fail":  func(id string) Event { return Broken{Consumer: id} },
		"fault": func(id string) Event { return Faulted{Consumer: id} },
	}
	for _, held := range []bool{false, true} {
		for _, result := range []string{"ok", "fail", "fault"} {
			for _, head := range []string{h2, ""} {
				if result == "ok" && head == "" {
					continue
				}
				t.Run(fmt.Sprint(held, result, head != ""), func(t *testing.T) {
					rep := Report{Row: "r1", Result: result, Head: head, Reason: "because", Cost: 3, Priced: true}
					one, two := cold4Taken(t, held), cold4Taken(t, held)
					plan, r := one.cold4ReportPlan("p1.w1", rep)
					require.Nil(t, r)
					one.op("report", plan)
					two.consume("p1.w1", rep)
					two.move("p1", follow[result]("p1.w1"))
					a, b := one.snap(), two.snap()
					assert.Equal(t, b.Cards, a.Cards)
					assert.Equal(t, b.Streams, a.Streams)
					assert.Equal(t, b.Rows, a.Rows)
					if held && result != "ok" {
						assert.Equal(t, Waiting, a.Cards["p1"].State, "a return into a held stream parks")
					}
				})
			}
		}
	}
	t.Run("fix attempt", func(t *testing.T) {
		mk := func() *world {
			w := cold4Taken(t, false)
			w.consume("p1.w1", Report{Row: "r1", Result: "fail", Head: h2, Reason: "half"})
			w.move("p1", Broken{Consumer: "p1.w1"})
			w.move("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
			w.consume("p1.w2", Take{Row: "r1"})
			return w
		}
		one, two := mk(), mk()
		rep := Report{Row: "r1", Result: "ok", Head: coldH3}
		plan, r := one.cold4ReportPlan("p1.w2", rep)
		require.Nil(t, r)
		one.op("report", plan)
		two.consume("p1.w2", rep)
		two.move("p1", Finished{Consumer: "p1.w2"})
		assert.Equal(t, two.snap().Cards, one.snap().Cards)
	})
}

// Stale: an ok head equal to the base or the prior head is refused before
// any plan; a plan built from a snapshot that moved is refused by the store
// on its gens, and nothing lands.
func TestColdread4ReportPlanStale(t *testing.T) {
	t.Parallel()
	w := cold4Taken(t, false)
	_, r := w.cold4ReportPlan("p1.w1", Report{Row: "r1", Result: "ok", Head: base})
	refusal{r, "head " + base + " is the base; nothing was done"}.check(t)
	_, r = w.cold4ReportPlan("p1.w1", Report{Row: "r2", Result: "ok", Head: h2})
	refusal{r, "consumer p1.w1 is held by r1, not r2"}.check(t)
	plan, r := w.cold4ReportPlan("p1.w1", Report{Row: "r1", Result: "ok", Head: h2})
	require.Nil(t, r)
	w.move("p1", Rescore{Score: 9}) // the snapshot moves under the plan
	rc, err := w.apply("stale report", plan)
	require.NoError(t, err)
	assert.False(t, rc.Accepted)
	assert.Equal(t, []string{Working, Working}, []string{w.card("p1").State, w.card("p1.w1").State})

	w2 := cold4Taken(t, false)
	w2.consume("p1.w1", Report{Row: "r1", Result: "fail", Head: h2})
	w2.move("p1", Broken{Consumer: "p1.w1"})
	w2.move("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
	w2.consume("p1.w2", Take{Row: "r1"})
	_, r = w2.cold4ReportPlan("p1.w2", Report{Row: "r1", Result: "ok", Head: h2})
	refusal{r, "head " + h2 + " is the prior head; nothing new was pushed"}.check(t)
}

// A read's ReportPlan is MoveConsumer's Report plan: no write to the
// producer, fenced on its gen, and a read head other than the producer's is
// refused.
func TestColdread4ReportPlanReadIsTheConsumerAlone(t *testing.T) {
	t.Parallel()
	w := cold4Review(t)
	w.move("p1", AskRead{Row: "r2"})
	w.consume("p1.r1.1", Take{Row: "r2"})
	rep := Report{Row: "r2", Result: "ok", Head: h1, Reason: "fine"}
	plan, r := w.cold4ReportPlan("p1.r1.1", rep)
	require.Nil(t, r)
	alone, r := MoveConsumer(w.card("p1.r1.1"), w.card("p1"), w.rows()["r2"], rep)
	require.Nil(t, r)
	assert.Equal(t, alone.Expect, plan.Expect)
	assert.Equal(t, alone.Writes, plan.Writes)
	_, r = w.cold4ReportPlan("p1.r1.1", Report{Row: "r2", Result: "ok", Head: h2})
	refusal{r, "read p1.r1.1 was cut at " + h1 + ", the report names " + h2}.check(t)
}

// D3 reaches ReportPlan: it returns a merged plan, which a later Merge keeps
// as written. A stream hold merged ahead of a work report that fails leaves
// the Broken un-re-run: p1 ready in the held stream (the card model's
// ParkedUnderHold). One by one, the Broken parks it.
func TestColdread4DefectReportPlanMergedAfterAHold(t *testing.T) {
	t.Parallel()
	w := cold4Taken(t, false)
	s := w.snap()
	hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Seq: s.Seq})
	require.Nil(t, r)
	report, r := w.cold4ReportPlan("p1.w1", Report{Row: "r1", Result: "fail", Head: h2, Reason: "tests red"})
	require.Nil(t, r)
	merged, mr := Merge(hold, report)
	if mr != nil {
		return
	}
	rc, err := w.apply("hold+report", merged)
	require.NoError(t, err)
	got := w.snap()
	assert.False(t, rc.Accepted && got.Streams["s1"].Held && got.Cards["p1"].State == Ready,
		"one merged op held s1 and left p1 ready in it")
}
