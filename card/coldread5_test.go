package card

// The fifth cold read of layer 2, at b2b0b8c2 (v2-card-fix4). Two kinds of
// test:
//
//   - TestColdread5Defect*: one per confirmed defect; each FAILS at b2b0b8c2
//     and asserts the design line it names.
//   - TestColdread5Walk: 100 seeds x 2,000 steps of moves planned from a
//     fresh snapshot, with merged plans of every kind, each merged op
//     compared with its plans applied one by one on a shadow Fake kept in
//     lockstep, and the design's invariants checked after every op.
//
// Unit tests, no clock, no sockets: rand is seeded, At is t0 plus the op's
// number. TestFunctionalColdread5PG (a real PostgreSQL, like store's
// functional tests) replays walks on PG and compares every receipt and every
// refused op's before and after with the Fake.

import (
	"context"
	"fmt"
	"maps"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cold5Report is ReportPlan as layer 3 builds it: needs is p's needs only
// (the cards Move reads), never every card of the snapshot.
func (w *world) cold5Report(id string, rep Report) (Plan, *Refusal) {
	all, cons := w.all()
	c := all[id]
	p := all[c.Of]
	needs := map[string]Card{}
	for _, n := range p.Needs {
		needs[n] = all[n]
	}
	return ReportPlan(c, p, cons[p.ID], w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), needs, rep)
}

// cold5Flat is the two plans ReportPlan composes, unmerged: the consumer's
// Report and the producer's follow-up built on the reported consumer.
func (w *world) cold5Flat(t *testing.T, id string, rep Report) []Plan {
	t.Helper()
	all, cons := w.all()
	c := all[id]
	p := all[c.Of]
	s := StreamFromStore(w.snap().Streams[p.Stream])
	cp, r := MoveConsumer(c, p, w.rows()[c.Holder], rep)
	require.Nil(t, r)
	ev, r := workFollow(c.ID, rep.Result)
	require.Nil(t, r)
	pp, r := Move(p, reportedCons(cons[p.ID], c, rep), w.rows(), s, map[string]Card{}, ev)
	require.Nil(t, r)
	return []Plan{cp, pp}
}

// cold5Working is p1 and p2 working on r1 (width 2), both consumers in hand.
func cold5Working(t *testing.T) *world {
	w := newWorld(t, producer("p1"), producer("p2"))
	for _, id := range []string{"p1", "p2"} {
		w.move(id, Resolve{})
		w.move(id, Start{Row: "r1", Branch: "b-" + id, Base: base})
	}
	return w
}

// ---- confirmed defects: each FAILS at b2b0b8c2 ----

// D1. "a merged plan stays re-runnable so nested merges and Merge(HoldStream,
// ReportPlan(...)) compose like flat ones" (the author's claim; README:
// "The merged plan itself runs again the same way, so nested merges
// compose"); layer 3 packs groups, each a card.Merge, into one op
// (LAYER-3-STEPS, the group and the packer). compose stores a merged plan's
// seen as the state after its own writes (replay.go:95, done.seen = cur).
// Merged into a later position, foldPlans first unions that post-state into
// the fold (replay.go:111, unionMissing) for every card the plans ahead did
// not read, then re-runs the inner moves against it: the Report finds its
// consumer already done, the Start finds its producer already working and
// its own consumer live. The nested merge is refused where the flat one
// lands. (The fourth read's TestColdread4DefectReportPlanMergedAfterAHold
// passes only because the merge is refused.)
func TestColdread5DefectNestedPlanReadsItsOwnWrites(t *testing.T) {
	t.Parallel()
	t.Run("HoldThenReportPlan", func(t *testing.T) {
		w := cold4Taken(t, false)
		s := w.snap()
		hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Seq: s.Seq})
		require.Nil(t, r)
		rep := Report{Row: "r1", Result: "fail", Head: h2, Reason: "tests red"}
		flat, fr := Merge(append([]Plan{hold}, w.cold5Flat(t, "p1.w1", rep)...)...)
		require.Nil(t, fr, "the flat merge composes")
		report, r := w.cold5Report("p1.w1", rep)
		require.Nil(t, r)
		nested, nr := Merge(hold, report)
		require.Nil(t, nr, "Merge(HoldStream, ReportPlan) is refused where the flat merge lands")
		assert.ElementsMatch(t, flat.Writes, nested.Writes)
	})
	t.Run("TwoReportPlans", func(t *testing.T) {
		w := cold5Working(t)
		ra := Report{Row: "r1", Result: "ok", Head: h1, Reason: "done"}
		rb := Report{Row: "r1", Result: "fail", Head: h2, Reason: "red"}
		flat, fr := Merge(append(w.cold5Flat(t, "p1.w1", ra), w.cold5Flat(t, "p2.w1", rb)...)...)
		require.Nil(t, fr, "the flat merge composes")
		a, r := w.cold5Report("p1.w1", ra)
		require.Nil(t, r)
		b, r := w.cold5Report("p2.w1", rb)
		require.Nil(t, r)
		packed, pr := Merge(a, b)
		require.Nil(t, pr, "two report groups packed into one op are refused")
		assert.ElementsMatch(t, flat.Writes, packed.Writes)
	})
	t.Run("RowMoveThenStart", func(t *testing.T) {
		w := newWorld(t, producer("p1"))
		w.move("p1", Resolve{})
		hold, r := MoveRow(w.rows()["r3"], Hold{Reason: "maintenance"})
		require.Nil(t, r)
		start, r := w.plan("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
		require.Nil(t, r)
		_, fr := Merge(hold, start)
		require.Nil(t, fr, "the flat merge composes")
		inner, r := Merge(start)
		require.Nil(t, r)
		_, nr := Merge(hold, inner)
		require.Nil(t, nr, "Merge(row Hold, Merge(Start)) is refused where Merge(row Hold, Start) lands")
	})
	// Not only a refusal: the post-state hides a ready producer from a
	// nested HoldStream's re-run, which then parks nothing. "No producer is
	// ready in a held stream after any step" (card model, ParkedUnderHold).
	// The walk found it as pack Merge(Merge(Start p5, Hold r1),
	// Merge(HoldStream s1, Width r1)) at seed 42.
	t.Run("ReadyLeftInAHeldStream", func(t *testing.T) {
		w := newWorld(t, producer("p1"), producer("p2"))
		w.move("p2", Resolve{})
		rescore, r := w.cold5Narrow1("p1", Rescore{Score: 3})
		require.Nil(t, r)
		s := w.snap()
		hold, r := MoveStream(StreamFromStore(s.Streams["s1"]), HoldStream{Reason: "freeze", Ready: []Card{w.card("p2")}, Seq: s.Seq})
		require.Nil(t, r)
		inner, r := Merge(hold)
		require.Nil(t, r)
		merged, mr := Merge(rescore, inner)
		if mr != nil {
			return // refused: the store stays legal
		}
		rc, err := w.apply("rescore+hold", merged)
		require.NoError(t, err)
		got := w.snap()
		assert.False(t, rc.Accepted && got.Streams["s1"].Held && got.Cards["p2"].State == Ready,
			"one merged op held s1 and left p2 ready in it")
	})
}

// cold5Narrow1 is Move of producer id on e with layer 3's needs (p's own).
func (w *world) cold5Narrow1(id string, e Event) (Plan, *Refusal) { return cold5P(id, e).plan(w) }

// D2. "a preempt merged with a blocker's Start is checked by event fields
// (blocker priority, the blocker's row)" and "nested merges compose like
// flat ones" (the author's claims; README, Preemption). preemptGuard
// (replay.go:201) reads only the top-level plans' events; a merged plan
// carries no event (ev is never set on it), so a blocker's Start inside a
// nested merge is never checked. Flat, a preempt of p1.w1 working on r2
// merged with blocker p2's Start on r1 is refused "consumer p1.w1 is on r2;
// blocker p2 is dealt to r1"; nested, the same two moves land: r2's
// consumer is withdrawn and the blocker is dealt to another row.
func TestColdread5DefectNestedStartSkipsThePreemptGuard(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"), producer("p2"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r2", Branch: "b-p1", Base: base})
	w.move("p2", Resolve{})
	w.move("p2", SetPriority{Level: "blocker", Reason: "asked"})
	wd := w.consumerPlan("p1.w1", Withdraw{Reason: "preempted by blocker p2", Preempt: true})
	st, r := w.plan("p2", Start{Row: "r1", Branch: "b-p2", Base: base})
	require.Nil(t, r)
	_, fr := Merge(wd, st)
	refusal{fr, "merge of Withdraw p1.w1 and Start p2 refused: consumer p1.w1 is on r2; blocker p2 is dealt to r1"}.check(t)
	inner, r := Merge(st)
	require.Nil(t, r)
	nested, nr := Merge(inner, wd)
	if nr == nil {
		rc, err := w.apply("preempt", nested)
		require.NoError(t, err)
		assert.False(t, rc.Accepted && w.card("p1.w1").State == Fail && w.card("p2.w1").Holder == "r1",
			"a nested merge withdrew p1.w1 on r2 and dealt blocker p2 to r1")
	}
	require.NotNil(t, nr, "the nested merge skipped the preempt check the flat merge makes")
}

// D3. "Merge is sequential composition: plan 1's writes are applied to a
// copy of the snapshot, plan 2's move is re-run against that state"
// (LAYER-2, third cold read rulings); README: "A hand-built plan is kept as
// written"; "the needs and after graphs stay acyclic" (LESSONS 2.19). A
// hand-built plan has no records (seen is empty), so folding its writes
// creates a partial record of each card it writes (editCard, replay.go:573:
// the id and the written column, nothing else), and unionMissing never
// replaces a record already present with the snapshot's. A later move is
// then re-run against those partial cards: a producer whose score was set
// is read as kind "" ("sentinel p1 never has a consumer"), and a Relink's
// cycle walk reads a card whose needs it never had. The merge below lands
// the cycle p -> x -> y -> z -> p; one by one the Relink is refused.
func TestColdread5DefectHandBuiltPlanBlindsTheReRun(t *testing.T) {
	t.Parallel()
	t.Run("Cycle", func(t *testing.T) {
		w := newWorld(t, producer("a"), producer("z"), producer("y", "z"), producer("x", "y"), producer("p", "a"))
		w.move("a", Cancel{Reason: "dropped"})
		relink, r := w.plan("p", Relink{Need: "a", To: "x", Reason: "x does it"})
		require.Nil(t, r)
		hand := Plan{Writes: []store.Write{store.SetCardNeeds{Card: "z", Needs: []string{"p"}}, store.SetCardScore{Card: "y", Score: 7}}}
		merged, mr := Merge(hand, relink)
		if mr != nil {
			return // refused: the store stays acyclic
		}
		rc, err := w.apply("hand+relink", merged)
		require.NoError(t, err)
		assert.False(t, rc.Accepted, "the merge of a hand-built plan and a Relink was accepted")
		assert.Empty(t, coldCycle(w.snap().Cards), "a needs cycle landed through one merged op")
	})
	t.Run("PartialProducer", func(t *testing.T) {
		w := newWorld(t, producer("p1"))
		w.move("p1", Resolve{})
		start, r := w.plan("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
		require.Nil(t, r)
		hand := Plan{Writes: []store.Write{store.SetCardScore{Card: "p1", Score: 4}}}
		_, mr := Merge(hand, start)
		require.Nil(t, mr, "a rescore written by hand, then Start, applies one by one")
	})
}

// D4. "Merge is sequential composition ... any refusal refuses the whole
// merge" (LAYER-2, third cold read rulings): a merge is refused only when one
// of its moves, re-run in order, is refused. preemptGuard (replay.go:201)
// treats every Withdraw in an op that also starts a blocker as that
// blocker's preempt, whatever the withdraw is for. Layer 3 packs groups
// into one op (LAYER-3-STEPS, the packer), so a blocker dealt to a free row
// and an unrelated withdraw (a held row's untaken consumer after its grace,
// LAYER-3 Holds) are refused together: "consumer p1.w1 is ready; a preempt
// withdraws a working consumer", though nothing was preempted and the two
// moves apply one by one.
func TestColdread5DefectAnyWithdrawIsReadAsAPreempt(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"), producer("p2"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r2", Branch: "b-p1", Base: base})
	w.move("p2", Resolve{})
	w.move("p2", SetPriority{Level: "blocker", Reason: "asked"})
	wd := w.consumerPlan("p1.w1", Withdraw{Reason: "row r2 held past its grace"})
	st, r := w.plan("p2", Start{Row: "r1", Branch: "b-p2", Base: base})
	require.Nil(t, r)
	_, mr := Merge(wd, st)
	require.Nil(t, mr, "a withdraw and a blocker's deal to a free row, each legal, are refused as one op")
}

// D5. "every refusal names the fix" (LAYER-2, What it refuses, naming the
// fix). dupWrites (values.go:222) adds "several reads of one head are
// AskReads" to every second write of a card's kind, so two Starts of one
// producer, or two Adds of one id, are told to use AskReads, which cuts
// reads and fixes neither. (The walk: "merge of Start # and Start #
// refused: ... several reads of one head are AskReads".)
func TestColdread5DefectSecondCutNamesTheWrongFix(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	a, r := w.plan("p1", Start{Row: "r1", Branch: "b-p1", Base: base})
	require.Nil(t, r)
	b, r := w.plan("p1", Start{Row: "r2", Branch: "b-p1", Base: base})
	require.Nil(t, r)
	_, mr := Merge(a, b)
	require.NotNil(t, mr)
	assert.NotContains(t, mr.Reason, "AskReads", "two Starts of p1: %s", mr.Reason)
	all, _ := w.all()
	x, r := MoveAdd(Add{ID: "a9", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do a9"}, all)
	require.Nil(t, r)
	y, r := MoveAdd(Add{ID: "a9", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do a9 too"}, all)
	require.Nil(t, r)
	_, mr = Merge(x, y)
	require.NotNil(t, mr)
	assert.NotContains(t, mr.Reason, "AskReads", "two Adds of a9: %s", mr.Reason)
}

// ---- the random walk: 100 seeds x 2,000 steps, every merge one by one ----

const cold5Seeds, cold5Steps = 100, 2000

// cold5Unit is one move as it is merged (step) and as it is applied one by
// one (seq): a ReportPlan is one merged plan and two ops.
type cold5Unit struct {
	step cold4Step
	seq  []cold4Step
}

func cold5One(s cold4Step) cold5Unit { return cold5Unit{step: s, seq: []cold4Step{s}} }

func cold5Units(ss []cold4Step) []cold5Unit {
	out := make([]cold5Unit, len(ss))
	for i, s := range ss {
		out[i] = cold5One(s)
	}
	return out
}

// cold5Run is a walk: the world w, a shadow s kept in lockstep where every
// merged op is applied one by one, w's op log (to rebuild s), and an
// optional PostgreSQL mirror of w.
type cold5Run struct {
	t             *testing.T
	seed          int64
	rng           *rand.Rand
	w, s          *world
	log           []store.Op
	cards         []store.PutCard
	pg            store.Store
	shadow        bool
	kinds         map[string]int
	reasons       map[string]int
	broken, notes map[string]*cold3Break
	entry         map[string]string
	sn            int
}

func cold5Seed() []store.PutCard {
	return []store.PutCard{producer("p1"), producer("p2", "p1"), producer("p3"), producer("p4", "p3"), producer("p5"),
		{ID: "g1", Kind: "sentinel", Needs: []string{"p2"}}}
}

func newCold5Run(t *testing.T, seed int64, pg store.Store, shadow bool) *cold5Run {
	r := &cold5Run{t: t, seed: seed, rng: rand.New(rand.NewSource(seed)), cards: cold5Seed(), pg: pg, shadow: shadow,
		kinds: map[string]int{}, reasons: map[string]int{}, broken: map[string]*cold3Break{}, notes: map[string]*cold3Break{}, entry: map[string]string{}}
	r.w = newWorld(t, r.cards...)
	if pg != nil {
		r.mirrorSeed()
	}
	if shadow {
		r.resync()
	}
	return r
}

// mirrorSeed applies to PG the ops newWorld applied to the Fake.
func (r *cold5Run) mirrorSeed() {
	for _, op := range r.seedOps() {
		rc, err := r.pg.Apply(context.Background(), op)
		require.NoError(r.t, err)
		require.True(r.t, rc.Accepted, "pg seed %s: %s", op.ID, rc.Reason)
	}
	r.pgSame("seed")
}

// apply applies plan to w as one op (logged), mirrored on PG.
func (r *cold5Run) apply(kind string, plan Plan) (store.Receipt, error) {
	if reason := r.w.overWidth(plan); reason != "" { // the dealer's check (round 22), as cold4Run.apply
		return store.Receipt{Reason: reason}, nil
	}
	r.w.n++
	op := store.Op{ID: fmt.Sprintf("op%d", r.w.n), Epoch: r.w.snap().Epoch, Actor: "card", Kind: kind, At: t0.Add(time.Duration(r.w.n) * time.Second),
		Expect: plan.Expect, Writes: plan.Writes}
	rc, err := r.w.f.Apply(context.Background(), op)
	if err != nil {
		return rc, err
	}
	r.log = append(r.log, op)
	if r.pg != nil {
		pc, perr := r.pg.Apply(context.Background(), op)
		switch {
		case perr != nil:
			cold3Note(r.broken, "PG found an op the Fake applied invalid", r.where(kind)+": "+perr.Error())
		case pc.Accepted != rc.Accepted || pc.Seq != rc.Seq:
			cold3Note(r.broken, "PG and the Fake disagree on an op", fmt.Sprintf("%s: fake %v %q, pg %v %q", r.where(kind), rc.Accepted, rc.Reason, pc.Accepted, pc.Reason))
		case !rc.Accepted || r.w.n%25 == 0:
			r.pgSame(kind)
		}
	}
	return rc, nil
}

// applyShadow applies plan to s as one op of s's own.
func (r *cold5Run) applyShadow(kind string, plan Plan) (store.Receipt, error) {
	if reason := r.s.overWidth(plan); reason != "" {
		return store.Receipt{Reason: reason}, nil
	}
	r.sn++
	op := store.Op{ID: fmt.Sprintf("s%d", r.sn), Epoch: r.s.snap().Epoch, Actor: "card", Kind: kind, At: t0.Add(time.Duration(r.sn) * time.Second),
		Expect: plan.Expect, Writes: plan.Writes}
	return r.s.f.Apply(context.Background(), op)
}

// resync rebuilds s from w's op log.
func (r *cold5Run) resync() {
	s := &world{t: r.t, f: store.NewFake(), width: maps.Clone(worldWidth)}
	for _, op := range r.log0() {
		_, err := s.f.Apply(context.Background(), op)
		require.NoError(r.t, err)
	}
	r.s = s
	r.kinds["shadow rebuilt"]++
}

// log0 is every op w took: newWorld's seed and Seen ops, then r.log.
func (r *cold5Run) log0() []store.Op { return append(r.seedOps(), r.log...) }

// seedOps are newWorld's ops: the seed, then Seen of each row.
func (r *cold5Run) seedOps() []store.Op {
	seed := []store.Write{store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1}}
	for _, row := range []store.PutRow{{Name: "r1"}, {Name: "r2"}, {Name: "r3"}} {
		seed = append(seed, row)
	}
	for _, c := range r.cards {
		c.Stream, c.Tier = "s1", "pro"
		if c.Kind == "" {
			c.Kind, c.Brief = "producer", "do "+c.ID
		}
		seed = append(seed, c)
	}
	ops := []store.Op{{ID: "seed1", Epoch: 1, Actor: "card", Kind: "seed", At: t0, Writes: seed}}
	for _, row := range []string{"r1", "r2", "r3"} {
		ops = append(ops, store.Op{ID: "seen-" + row, Epoch: 1, Actor: "card", Kind: "seen", At: t0,
			Expect: []store.Expect{store.RowGen{Row: row, Gen: 0}}, Writes: []store.Write{store.SetRowState{Row: row, State: Up}}})
	}
	return ops
}

func (r *cold5Run) where(label string) string {
	return fmt.Sprintf("seed %d op %d %s", r.seed, r.w.n, label)
}

// pgSame notes a difference between PG and the Fake, gens included.
func (r *cold5Run) pgSame(label string) {
	ps, err := r.pg.Snapshot(context.Background())
	if err != nil {
		cold3Note(r.broken, "PG snapshot failed", r.where(label)+": "+err.Error())
		return
	}
	if d := cold5Exact(r.w.snap(), ps); d != "" {
		cold3Note(r.broken, "PG differs from the Fake", r.where(label)+": "+d)
	}
}

// cold5Exact is how two snapshots' cards, rows and streams differ, gens
// included; empty and nil lists are the same.
func cold5Exact(a, b store.Snapshot) string {
	norm := func(s []string) []string {
		if len(s) == 0 {
			return nil
		}
		return s
	}
	if len(a.Cards) != len(b.Cards) {
		return fmt.Sprintf("%d cards vs %d", len(a.Cards), len(b.Cards))
	}
	for id, x := range a.Cards {
		y, ok := b.Cards[id]
		cx, cy := FromStore(x), FromStore(y)
		for _, c := range []*Card{&cx, &cy} {
			c.Needs, c.After, c.Paths, c.DoneWhen = norm(c.Needs), norm(c.After), norm(c.Paths), norm(c.DoneWhen)
		}
		if !ok || !reflect.DeepEqual(cx, cy) || x.PR != y.PR {
			return fmt.Sprintf("card %s: %+v vs %+v", id, cx, cy)
		}
	}
	for n, x := range a.Rows {
		rx, ry := RowFromStore(x, 0, nil), RowFromStore(b.Rows[n], 0, nil)
		rx.Tiers, ry.Tiers = norm(rx.Tiers), norm(ry.Tiers)
		if !reflect.DeepEqual(rx, ry) {
			return fmt.Sprintf("row %s: %+v vs %+v", n, rx, ry)
		}
	}
	for n, x := range a.Streams {
		if b.Streams[n] != x {
			return fmt.Sprintf("stream %s: %+v vs %+v", n, x, b.Streams[n])
		}
	}
	return ""
}

// cold5P is cold4P with needs as layer 3 passes them: p's needs only, or,
// for the events that walk or name other cards, every card.
func cold5P(id string, e Event) cold4Step {
	return cold4Step{name(e) + " " + id, func(w *world) (Plan, *Refusal) {
		all, cons := w.all()
		p, ok := all[id]
		if !ok {
			return Plan{}, no("no card %s", id)
		}
		needs := all
		switch e.(type) {
		case Relink, Replace:
		default:
			needs = map[string]Card{}
			for _, n := range p.Needs {
				if c, ok := all[n]; ok {
					needs[n] = c
				}
			}
		}
		return Move(p, cons[id], w.rows(), StreamFromStore(w.snap().Streams[p.Stream]), needs, e)
	}}
}

// cold5Narrow is a move with layer 3's needs: a random producer event, or
// a consumer, row or stream event (those read no needs).
func (w *world) cold5Narrow(rng *rand.Rand) cold4Step {
	s := w.cold4One(rng)
	f := strings.Fields(s.label)
	if len(f) != 2 {
		return s
	}
	all, _ := w.all()
	c, ok := all[f[1]]
	if !ok || consumer(c.Kind) {
		return s
	}
	ev := map[string]Event{"Resolve": Resolve{}, "Start": Start{Row: []string{"r1", "r2", "r3"}[rng.Intn(3)], Branch: "b-" + c.ID, Base: base},
		"Rescore": Rescore{Score: rng.Intn(4)}, "Edit": Edit{Prose: fmt.Sprint("prose ", rng.Intn(3))}, "Cancel": Cancel{Reason: "c"},
		"Block": Block{Reason: "b"}, "Unblock": Unblock{Reason: "u"}, "Reached": Reached{}, "ReadYes": ReadYes{}, "Park": Park{}}[f[0]]
	if ev == nil {
		return s
	}
	return cold5P(c.ID, ev)
}

// cold5ReportUnit is ReportPlan of a working work consumer, and the same as
// two ops: the Report, then Finished, Broken or Faulted.
func (w *world) cold5ReportUnit(rng *rand.Rand) (cold5Unit, bool) {
	all, _ := w.all()
	var ids []string
	for id, c := range all {
		if c.Kind == "work" && c.State == Working {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return cold5Unit{}, false
	}
	sort.Strings(ids)
	c := all[ids[rng.Intn(len(ids))]]
	res := []string{"ok", "ok", "fail", "fault"}[rng.Intn(4)]
	head := []string{h1, h2, coldH3}[rng.Intn(3)]
	if res != "ok" && rng.Intn(3) == 0 {
		head = ""
	}
	rep := Report{Row: c.Holder, Result: res, Head: head, Reason: "r", Cost: int64(rng.Intn(3)), Priced: rng.Intn(2) == 0}
	follow := map[string]Event{"ok": Finished{Consumer: c.ID}, "fail": Broken{Consumer: c.ID}, "fault": Faulted{Consumer: c.ID}}[res]
	merged := cold4Step{"ReportPlan " + c.ID, func(w *world) (Plan, *Refusal) {
		all, _ := w.all()
		if _, ok := all[c.ID]; !ok {
			return Plan{}, no("no card %s", c.ID)
		}
		return w.cold5Report(c.ID, rep)
	}}
	return cold5Unit{step: merged, seq: []cold4Step{cold4C(c.ID, rep), cold4P(c.Of, follow)}}, true
}

// cold5Partner is what a ReportPlan is merged with: a hold or unhold, a
// withdraw of a live consumer, an edit, another report, a row move.
func (w *world) cold5Partner(rng *rand.Rand) cold5Unit {
	all, _ := w.all()
	switch rng.Intn(6) {
	case 0:
		if w.snap().Streams["s1"].Held {
			return cold5One(cold4Unhold())
		}
		return cold5One(cold4Hold())
	case 1:
		var live []string
		for id, c := range all {
			if consumer(c.Kind) && (c.State == Ready || c.State == Working) {
				live = append(live, id)
			}
		}
		if len(live) > 0 {
			sort.Strings(live)
			return cold5One(cold4C(live[rng.Intn(len(live))], Withdraw{Reason: "silent"}))
		}
	case 2:
		ids := []string{"p1", "p2", "p3", "p4", "p5"}
		return cold5One(cold5P(ids[rng.Intn(len(ids))], Edit{Prose: fmt.Sprint("scope ", rng.Intn(9)), Reason: "scope"}))
	case 3:
		if u, ok := w.cold5ReportUnit(rng); ok {
			return u
		}
	case 4:
		row := []string{"r1", "r2", "r3"}[rng.Intn(3)]
		return cold5One(cold4R(row, []Event{Hold{Reason: "h"}, Unhold{Reason: "u"}, Gone{}, Seen{}}[rng.Intn(4)]))
	}
	return cold5One(w.cold5Narrow(rng))
}

// cold5Pick is one step: kind ("" for a single move), its units, and the
// merge shape.
func (r *cold5Run) pick() (string, []cold5Unit, string) {
	w, rng := r.w, r.rng
	switch k := rng.Intn(100); {
	case k < 7:
		return "two", cold5Units([]cold4Step{w.cold4One(rng), w.cold4One(rng)}), "flat"
	case k < 13:
		return "hold", cold5Units(w.cold4HoldPair(rng)), "flat"
	case k < 18:
		return "drop", cold5Units(w.cold4Drop(rng)), "flat"
	case k < 20:
		return "red+waive", cold5Units(w.cold4RedWaive()), "flat"
	case k < 25:
		return "preempt", cold5Units(w.cold4Preempt(rng)), "flat"
	case k < 30:
		return "review", cold5Units(w.cold4ReviewPair(rng)), "flat"
	case k < 37:
		us := []cold5Unit{w.cold5Partner(rng), w.cold5Partner(rng), w.cold5Partner(rng)}
		return "nested3", us, []string{"right", "left", "mid", "first"}[rng.Intn(4)]
	case k < 46:
		u, ok := w.cold5ReportUnit(rng)
		if !ok {
			return "report", nil, "flat"
		}
		if rng.Intn(4) == 0 {
			return "report alone", []cold5Unit{u}, "flat"
		}
		us := []cold5Unit{u, w.cold5Partner(rng)}
		if rng.Intn(2) == 0 {
			us[0], us[1] = us[1], us[0]
		}
		return "report", us, "flat"
	case k < 51:
		return "pack", []cold5Unit{w.cold5Partner(rng), w.cold5Partner(rng), w.cold5Partner(rng), w.cold5Partner(rng)}, "pack"
	case k < 56:
		return "narrow", cold5Units([]cold4Step{w.cold5Narrow(rng), w.cold5Narrow(rng)}), "flat"
	}
	if rng.Intn(3) == 0 {
		return "", cold5Units([]cold4Step{w.cold5Narrow(rng)}), "flat"
	}
	return "", cold5Units([]cold4Step{w.cold4One(rng)}), "flat"
}

// cold5Shape merges plans in the shape named: flat; three deep (right,
// left, mid, first); or pack (two groups of two, each a Merge).
func cold5Shape(shape string, ps []Plan) (Plan, *Refusal) {
	m := func(p Plan, r *Refusal, then func(Plan) (Plan, *Refusal)) (Plan, *Refusal) {
		if r != nil {
			return Plan{}, r
		}
		return then(p)
	}
	switch {
	case shape == "right" && len(ps) == 3:
		in, r := Merge(ps[2])
		return m(in, r, func(in Plan) (Plan, *Refusal) {
			mid, r := Merge(ps[1], in)
			return m(mid, r, func(mid Plan) (Plan, *Refusal) { return Merge(ps[0], mid) })
		})
	case shape == "left" && len(ps) == 3:
		in, r := Merge(ps[0])
		return m(in, r, func(in Plan) (Plan, *Refusal) {
			mid, r := Merge(in, ps[1])
			return m(mid, r, func(mid Plan) (Plan, *Refusal) { return Merge(mid, ps[2]) })
		})
	case shape == "mid" && len(ps) == 3:
		in, r := Merge(ps[1], ps[2])
		return m(in, r, func(in Plan) (Plan, *Refusal) { return Merge(ps[0], in) })
	case shape == "first" && len(ps) == 3:
		in, r := Merge(ps[0], ps[1])
		return m(in, r, func(in Plan) (Plan, *Refusal) { return Merge(in, ps[2]) })
	case shape == "pack" && len(ps) == 4:
		a, r := Merge(ps[0], ps[1])
		return m(a, r, func(a Plan) (Plan, *Refusal) {
			b, r := Merge(ps[2], ps[3])
			return m(b, r, func(b Plan) (Plan, *Refusal) { return Merge(a, b) })
		})
	}
	return Merge(ps...)
}

// cold5ByDesign is a merge refusal the design keeps: a second write to one
// field, two creates of one card, or a preempt's own check in a preempt.
func cold5ByDesign(kind, reason string) bool {
	return strings.Contains(reason, "a second write to") ||
		strings.Contains(reason, "a preempt withdraws a working consumer") ||
		(kind == "preempt" && strings.Contains(reason, "; blocker "))
}

var cold5IDs = regexp.MustCompile(`\b([0-9a-f]{40}|[a-z]+[0-9]+(\.[wr][0-9]+(\.[0-9]+)?)?)\b`)

// cold5Template is a reason with its names and shas blanked.
func cold5Template(reason string) string { return cold5IDs.ReplaceAllString(reason, "#") }

func cold5Labels(us []cold5Unit) string {
	var ls []string
	for _, u := range us {
		ls = append(ls, u.step.label)
	}
	return strings.Join(ls, " + ")
}

// step is one step of the walk.
func (r *cold5Run) step() {
	before := r.w.snap()
	kind, units, shape := r.pick()
	if kind != "" {
		r.kinds[kind+" tried"]++
	}
	if len(units) == 0 {
		return
	}
	plans := make([]Plan, len(units))
	for j, u := range units {
		p, ref := u.step.plan(r.w)
		if ref != nil {
			if kind != "" {
				r.kinds[kind+" a move refused from the snapshot"]++
			}
			return
		}
		plans[j] = p
	}
	label := cold5Labels(units)
	if kind == "" {
		r.single(units[0], plans[0], before)
		return
	}
	merged, mr := cold5Shape(shape, plans)
	if shape != "flat" {
		r.vsFlat(kind+" "+shape, label, plans, merged, mr)
	}
	if mr != nil {
		r.machineRefused(kind, label, units, mr)
		return
	}
	r.fences(kind, label, merged)
	rc, err := r.apply(kind+":"+label, merged)
	if err != nil {
		cold3Note(r.broken, "the store found a machine plan invalid", r.where(label)+": "+err.Error())
		return
	}
	r.vsOneByOne(kind, label, units, rc)
	r.after(kind, label, before, merged, rc, units)
}

// single applies one move to w and, planned again there, to the shadow.
func (r *cold5Run) single(u cold5Unit, plan Plan, before store.Snapshot) {
	rc, err := r.apply(u.step.label, plan)
	if err != nil {
		cold3Note(r.broken, "the store found a machine plan invalid", r.where(u.step.label)+": "+err.Error())
		return
	}
	if r.shadow {
		res, n := r.sequential(u.seq)
		if (res == "") != rc.Accepted {
			cold3Note(r.broken, "the shadow diverged on a single move", r.where(u.step.label)+": "+res)
		}
		if (res == "") != rc.Accepted || (!rc.Accepted && n > 0) {
			r.resync()
		}
	}
	r.after("", u.step.label, before, plan, rc, []cold5Unit{u})
}

// sequential applies steps one by one on the shadow, each planned from the
// state the one before left: "" when every one is accepted, else why not,
// and how many landed.
func (r *cold5Run) sequential(steps []cold4Step) (string, int) {
	for i, s := range steps {
		plan, ref := s.plan(r.s)
		if ref != nil {
			return "machine refused " + s.label + ": " + ref.Reason, i
		}
		rc, err := r.applyShadow(s.label, plan)
		if err != nil {
			return "store found " + s.label + " invalid: " + err.Error(), i
		}
		if !rc.Accepted {
			return "store refused " + s.label + ": " + rc.Reason, i
		}
	}
	return "", len(steps)
}

func cold5Seq(us []cold5Unit) []cold4Step {
	var out []cold4Step
	for _, u := range us {
		out = append(out, u.seq...)
	}
	return out
}

func cold5Events(us []cold5Unit) string {
	var ns []string
	for _, u := range us {
		ns = append(ns, strings.Fields(u.step.label)[0])
	}
	return strings.Join(ns, "+")
}

// vsOneByOne compares a merged op w took with its moves applied one by one
// on the shadow, then brings the shadow back to w when they differ.
func (r *cold5Run) vsOneByOne(kind, label string, us []cold5Unit, rc store.Receipt) {
	if !r.shadow {
		return
	}
	r.kinds[kind+" checked one by one"]++
	res, n := r.sequential(cold5Seq(us))
	pair := " (" + kind + ": " + cold5Events(us) + ")"
	where := r.where(label)
	switch {
	case rc.Accepted && res == "":
		if d := cold4Diff(r.w.snap(), r.s.snap()); d != "" {
			cold3Note(r.broken, "a merged op differs from its moves applied one by one"+pair, where+": "+d)
			r.resync()
		}
		return
	case rc.Accepted:
		cold3Note(r.broken, "a merged op landed what one by one refuses"+pair, where+": "+res)
	case res == "" && !strings.Contains(rc.Reason, "width"):
		cold3Note(r.broken, "the store refused a merged op that applies one by one"+pair, where+": "+rc.Reason)
	}
	if n > 0 || rc.Accepted {
		r.resync()
	}
}

// machineRefused records a merge the machine refused and, unless the design
// keeps that refusal, whether its moves apply one by one.
func (r *cold5Run) machineRefused(kind, label string, us []cold5Unit, mr *Refusal) {
	tpl := cold5Template(mr.Reason)
	r.kinds[kind+" refused by the machine"]++
	r.reasons[kind+": "+tpl]++
	if strings.Contains(mr.Reason, "  ") || strings.Contains(mr.Reason, `""`) || strings.Contains(mr.Reason, " a ;") {
		cold3Note(r.broken, "a refusal with an empty name: "+tpl, r.where(label)+": "+mr.Reason)
	}
	if cold5ByDesign(kind, mr.Reason) || !r.shadow {
		return
	}
	res, n := r.sequential(cold5Seq(us))
	if res == "" {
		short := tpl
		if i := strings.LastIndex(short, "refused: "); i >= 0 {
			short = short[i+len("refused: "):]
		}
		cold3Note(r.broken, "the machine refused a merge whose moves apply one by one ("+kind+"): "+short, r.where(label)+": "+mr.Reason)
	}
	if n > 0 {
		r.resync()
	}
}

// vsFlat compares a nested merge with the flat Merge of the same plans.
func (r *cold5Run) vsFlat(kind, label string, plans []Plan, nested Plan, nr *Refusal) {
	flat, fr := Merge(plans...)
	where := r.where(label)
	switch {
	case fr == nil && nr != nil:
		cold3Note(r.broken, "a nested merge is refused where the flat one composes ("+kind+")", where+": "+nr.Reason)
	case fr != nil && nr == nil:
		cold3Note(r.broken, "a nested merge composes what the flat one refuses ("+kind+")", where+": "+fr.Reason)
	case fr == nil && (cold5Bag(flat.Writes) != cold5Bag(nested.Writes) || cold5Set(flat.Expect) != cold5Set(nested.Expect)):
		cold3Note(r.broken, "a nested merge differs from the flat one ("+kind+")", where)
	}
}

func cold5Bag[T any](xs []T) string {
	ss := make([]string, len(xs))
	for i, x := range xs {
		ss[i] = fmt.Sprintf("%#v", x)
	}
	sort.Strings(ss)
	return strings.Join(ss, "\n")
}

func cold5Set[T any](xs []T) string {
	ss := strings.Split(cold5Bag(xs), "\n")
	return strings.Join(slices.Compact(ss), "\n")
}

// fences notes a merged plan whose conditions no store state satisfies: one
// key fenced with two values, or a card fenced both absent and at a gen.
func (r *cold5Run) fences(kind, label string, plan Plan) {
	vals := map[string]string{}
	for _, e := range plan.Expect {
		var k, v string
		switch x := e.(type) {
		case store.CardGen:
			k, v = "card "+x.Card, fmt.Sprint("gen ", x.Gen)
		case store.CardAbsent:
			k, v = "card "+x.Card, "absent"
		case store.CardState:
			k, v = "state "+x.Card, x.State
		case store.CardRed:
			k, v = "red "+x.Card, fmt.Sprint(x.Red)
		case store.StreamHeld:
			k, v = "stream "+x.Stream, fmt.Sprint(x.Held)
		case store.RowIs:
			k, v = "row state "+x.Row, x.State
		case store.RowHeld:
			k, v = "row held "+x.Row, fmt.Sprint(x.Held)
		case store.RowGen:
			k, v = "row gen "+x.Row, fmt.Sprint(x.Gen)
		case store.AtSeq:
			k, v = "seq", fmt.Sprint(x.Seq)
		default:
			continue
		}
		if was, ok := vals[k]; ok && was != v {
			cold3Note(r.broken, "a merged op's fences contradict ("+kind+")", fmt.Sprintf("%s: %s %s and %s", r.where(label), k, was, v))
		}
		vals[k] = v
	}
}

// after checks an op w took: a refused one changed nothing; an accepted one
// keeps every invariant.
func (r *cold5Run) after(kind, label string, before store.Snapshot, plan Plan, rc store.Receipt, us []cold5Unit) {
	now := r.w.snap()
	where := r.where(label)
	if !rc.Accepted {
		if d := cold5Exact(before, now); d != "" {
			cold3Note(r.broken, "a refused op changed the Fake", where+": "+d)
		}
		if kind == "" && !strings.Contains(rc.Reason, "width") {
			cold3Note(r.broken, "store refused a single machine plan", where+": "+rc.Reason)
		}
		if kind != "" {
			r.kinds[kind+" refused by the store"]++
		}
		return
	}
	if kind != "" {
		r.kinds[kind+" accepted"]++
	}
	r.kinds["ops accepted"]++
	var steps []cold4Step
	for _, u := range us {
		steps = append(steps, u.step)
	}
	for _, v := range cold3Violations(before, now, r.entry) {
		if strings.HasPrefix(v[0], "note: ") {
			cold3Note(r.notes, strings.TrimPrefix(v[0], "note: "), where+": "+v[1])
		} else {
			cold3Note(r.broken, v[0], where+": "+v[1])
		}
	}
	for _, v := range append(cold4Violations(before, now, plan, kind, steps, r.entry), cold5Violations(before, now)...) {
		cold3Note(r.broken, v[0], where+": "+v[1])
	}
}

// cold5Violations: red only on producers; an Edit never lands while a
// consumer of the producer is live before the op (an Edit then a Start in
// one op is the sequence edit, then deal, and is legal).
func cold5Violations(before, after store.Snapshot) [][2]string {
	var out [][2]string
	liveOf := map[string]bool{}
	for _, c := range before.Cards {
		if consumer(c.Kind) && (c.State == Ready || c.State == Working) {
			liveOf[c.Of] = true
		}
	}
	for _, c := range after.Cards {
		if c.Red && c.Kind != "producer" {
			out = append(out, [2]string{"red on a card that is not a producer", c.ID})
		}
		was, ok := before.Cards[c.ID]
		if !ok || consumer(c.Kind) {
			continue
		}
		edited := was.Prose != c.Prose || !slices.Equal(was.Paths, c.Paths) || !slices.Equal(was.DoneWhen, c.DoneWhen)
		if edited && liveOf[c.ID] {
			out = append(out, [2]string{"an Edit landed under a live consumer", c.ID})
		}
	}
	return out
}

func cold5Fold(into map[string]*cold3Break, from map[string]*cold3Break) {
	for k, b := range from {
		if a, ok := into[k]; ok {
			a.seeds++
			a.steps += b.steps
		} else {
			into[k] = &cold3Break{seeds: 1, steps: b.steps, first: b.first}
		}
	}
}

// cold5Report logs what the walk did and fails on every broken invariant
// whose key starts with only ("" for all; the rest are logged).
func cold5Report(t *testing.T, runs []*cold5Run, seeds, steps int, require []string, only string) {
	kinds, reasons := map[string]int{}, map[string]int{}
	broken, notes := map[string]*cold3Break{}, map[string]*cold3Break{}
	for _, r := range runs {
		if r == nil {
			continue
		}
		for k, n := range r.kinds {
			kinds[k] += n
		}
		for k, n := range r.reasons {
			reasons[k] += n
		}
		cold5Fold(broken, r.broken)
		cold5Fold(notes, r.notes)
	}
	t.Logf("%d seeds x %d steps", seeds, steps)
	for _, k := range cold3Keys2(kinds) {
		t.Logf("kind %s: %d", k, kinds[k])
	}
	for _, k := range cold3Keys2(reasons) {
		t.Logf("machine refusal %q: %d", k, reasons[k])
	}
	for _, k := range require {
		assert.Positive(t, kinds[k+" accepted"], "the walk never landed a %s merge", k)
	}
	for _, k := range cold3Keys(notes) {
		t.Logf("note %q: %d seeds, %d steps; first: %s", k, notes[k].seeds, notes[k].steps, notes[k].first)
	}
	for _, k := range cold3Keys(broken) {
		if only != "" && !strings.HasPrefix(k, only) {
			t.Logf("seen, not this test's: %q: %d seeds, %d steps; first: %s", k, broken[k].seeds, broken[k].steps, broken[k].first)
			continue
		}
		t.Errorf("broken %q: %d seeds, %d steps; first: %s", k, broken[k].seeds, broken[k].steps, broken[k].first)
	}
}

func cold3Keys2(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// TestColdread5Walk: 100 seeds x 2,000 steps; every merged op the machine
// builds is applied one by one on the shadow and compared; every op is
// checked against the design's invariants. It FAILS at b2b0b8c2 with the
// defects above.
func TestColdread5Walk(t *testing.T) {
	t.Parallel()
	seeds, steps := walkSeeds(cold5Seeds), cold5Steps
	if testing.Short() { // half the seeds, a third of the steps: a seed's walk is the critical path, and these still land every kind
		seeds, steps = cold5Seeds/2, cold5Steps/3
	}
	runs := make([]*cold5Run, seeds)
	t.Run("seeds", func(t *testing.T) {
		for seed := int64(1); seed <= seeds; seed++ {
			t.Run(fmt.Sprint(seed), func(t *testing.T) {
				t.Parallel() // each seed its own Fakes
				r := newCold5Run(t, seed, nil, true)
				for i := 0; i < steps; i++ {
					r.step()
				}
				runs[seed-1] = r
			})
		}
	})
	cold5Report(t, runs, int(seeds), steps, []string{"two", "hold", "drop", "preempt", "review", "nested3", "report", "report alone", "pack", "narrow"}, "")
}

// ---- PostgreSQL: the same walk mirrored on a real store ----

// cold5StartPG starts a PostgreSQL of this test's own, as store's functional
// tests do (initdb into TempDir, a free port, fsync off), and returns the
// dsn. Skipped when initdb is not found.
func cold5StartPG(t *testing.T) string {
	t.Helper()
	bin := ""
	if p, err := exec.LookPath("initdb"); err == nil {
		bin = filepath.Dir(p)
	} else if found, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb"); len(found) > 0 { // ignored: a constant pattern
		bin = filepath.Dir(found[len(found)-1])
	} else if found, _ := filepath.Glob("/opt/homebrew/opt/postgresql@18/bin/initdb"); len(found) > 0 { // ignored: as above
		bin = filepath.Dir(found[0])
	} else {
		t.Skip("functional tests need PostgreSQL: initdb not found")
	}
	dir := t.TempDir()
	init := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8", "--no-locale")
	init.Env = append(os.Environ(), "LC_ALL=C")
	out, err := init.CombinedOutput()
	require.NoError(t, err, "%s", out)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	// pg_ctl starts it and stops it (-m immediate: the postmaster stops its
	// children before it exits, so none writes into the removed TempDir).
	ctl := filepath.Join(bin, "pg_ctl")
	start := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-l", filepath.Join(dir, "postgres.log"), "-o", "-p "+port+" -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", "-w", "start")
	start.Env = append(os.Environ(), "LC_ALL=C")
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("pg_ctl start: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		stop := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-m", "immediate", "-w", "stop")
		stop.Env = append(os.Environ(), "LC_ALL=C")
		if out, err := stop.CombinedOutput(); err != nil {
			t.Logf("pg_ctl stop: %v\n%s", err, out)
		}
	})
	return "postgres://nova@127.0.0.1:" + port + "/postgres?sslmode=disable"
}

// TestFunctionalColdread5PG mirrors walks (no shadow) on a real PostgreSQL:
// every op the Fake takes, PG takes with the same id, and both give the
// same accepted flag and seq; after every refused op, and every 25 ops, PG's
// cards, rows and streams equal the Fake's, gens included (a refused op
// leaves nothing half-applied on either).
func TestFunctionalColdread5PG(t *testing.T) {
	t.Parallel()
	const seeds, steps = 4, 800
	runs := make([]*cold5Run, seeds)
	t.Run("seeds", func(t *testing.T) {
		for seed := int64(1); seed <= seeds; seed++ {
			t.Run(fmt.Sprint(seed), func(t *testing.T) {
				t.Parallel()
				dsn := cold5StartPG(t)
				ctx := context.Background()
				var pg *store.PG
				require.Eventually(t, func() bool {
					p, err := store.Open(ctx, dsn, "")
					if err == nil {
						pg = p
					}
					return err == nil
				}, 30*time.Second, 50*time.Millisecond)
				t.Cleanup(pg.Close)
				require.NoError(t, pg.Migrate(ctx))
				r := newCold5Run(t, 1000+seed, pg, false)
				for i := 0; i < steps; i++ {
					r.step()
				}
				r.pgSame("end")
				runs[seed-1] = r
			})
		}
	})
	cold5Report(t, runs, seeds, steps, nil, "PG")
}
