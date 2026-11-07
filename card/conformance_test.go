package card

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Conformance: every plan the machine makes, applied to store.Fake as a real
// op (epoch from the snapshot, At passed in), is accepted, and the rows it
// leaves are the ones its writes name. This is the proof the machine emits
// only writes layer 1 accepts. No clock: At is t0 plus the op's number.

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

type world struct {
	t     *testing.T
	f     *store.Fake
	n     int
	ev    Event          // the walk's last event, for its trace (trace_test.go)
	width map[string]int // the roster's widths
}

// worldTiers is every row's tiers in the roster.
var worldTiers = []string{"pro"}

// worldWidth is every row's width in the roster; no event changes it.
var worldWidth = map[string]int{"r1": 2, "r2": 1, "r3": 1}

// newWorld is stream s1 with rows r1 (width 2), r2 and r3 (width 1) in its
// roster, all up and serving pro, and the given producers (id, needs) of
// tier pro.
func newWorld(t *testing.T, cards ...store.PutCard) *world {
	w := &world{t: t, f: store.NewFake(), width: maps.Clone(worldWidth)}
	seed := []store.Write{store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1}}
	for _, r := range []string{"r1", "r2", "r3"} {
		seed = append(seed, store.PutRow{Name: r})
	}
	for _, c := range cards {
		c.Stream, c.Tier = "s1", "pro"
		if c.Kind == "" {
			c.Kind, c.Brief = "producer", "do "+c.ID
		}
		seed = append(seed, c)
	}
	w.raw("seed", seed...)
	for _, r := range []string{"r1", "r2", "r3"} {
		w.row(r, Seen{})
	}
	return w
}

func producer(id string, needs ...string) store.PutCard { return store.PutCard{ID: id, Needs: needs} }

func (w *world) snap() store.Snapshot {
	s, err := w.f.Snapshot(context.Background())
	require.NoError(w.t, err)
	return s
}

// op applies plan as one op, requires the store to accept it, and checks
// the rows it names.
func (w *world) op(kind string, plan Plan) {
	w.t.Helper()
	r, err := w.apply(kind, plan)
	require.NoError(w.t, err, "%s: %+v", kind, plan)
	require.True(w.t, r.Accepted, "%s refused: %s; %+v", kind, r.Reason, plan)
	w.landed(plan)
}

// raw applies writes the machine does not make (the seed, a stream hold as
// layer 3's raw op) and requires them accepted.
func (w *world) raw(kind string, writes ...store.Write) {
	w.t.Helper()
	r, err := w.apply(kind, Plan{Writes: writes})
	require.NoError(w.t, err)
	require.True(w.t, r.Accepted, "%s refused: %s", kind, r.Reason)
}

// apply applies plan as one op: epoch from the snapshot, At passed in; the
// receipt and error unchecked. A take past the roster's width is refused
// here, as layer 3's dealer refuses it (layer 1 does not count
// width), with a reason naming width, and never reaches the store.
func (w *world) apply(kind string, plan Plan) (store.Receipt, error) {
	if reason := w.overWidth(plan); reason != "" {
		return store.Receipt{Reason: reason}, nil
	}
	w.n++
	op := store.Op{ID: fmt.Sprintf("op%d", w.n), Epoch: w.snap().Epoch, Actor: "card", Kind: kind, At: t0.Add(time.Duration(w.n) * time.Second),
		Expect: plan.Expect, Writes: plan.Writes}
	return w.f.Apply(context.Background(), op)
}

// overWidth is the dealer's width check on plan: the first consumer it
// moves into working, a cut one included (the deal is the take),
// on a row that already holds the row's roster width of working consumers,
// or "".
func (w *world) overWidth(plan Plan) string {
	s := w.snap()
	working := map[string]int{} // per row, counted at each write as the store's old rule did
	for _, o := range s.Cards {
		if o.Holder != "" && o.State == Working {
			working[o.Holder]++
		}
	}
	cut := map[string]store.Card{}
	for _, x := range plan.Writes {
		if put, ok := x.(store.PutCard); ok {
			cut[put.ID] = store.Card{ID: put.ID, Holder: put.Holder}
		}
	}
	for _, x := range plan.Writes {
		v, ok := x.(store.SetCardState)
		c, held := s.Cards[v.Card]
		if n, isCut := cut[v.Card]; isCut {
			c, held = n, true
		}
		if !ok || !held || c.Holder == "" || (v.State == Working) == (c.State == Working) {
			continue
		}
		if v.State != Working {
			working[c.Holder]--
			continue
		}
		if n := working[c.Holder]; n >= w.width[c.Holder] {
			return fmt.Sprintf("width: row %s holds %d working, its roster width is %d", c.Holder, n, w.width[c.Holder])
		}
		working[c.Holder]++
	}
	return ""
}

func kind(e Event) string { return strings.ToLower(name(e)) }

func (w *world) card(id string) Card {
	w.t.Helper()
	c, ok := w.snap().Cards[id]
	require.True(w.t, ok, "card %s", id)
	return FromStore(c)
}

// fromStore is FromStore, the short name tests use.
func fromStore(c store.Card) Card { return FromStore(c) }

func (w *world) rows() map[string]Row {
	rs := map[string]Row{}
	for n, r := range w.snap().Rows {
		rs[n] = RowFromStore(r, w.width[n], worldTiers)
	}
	return rs
}

// plan is Move of producer id on e, read from the store as a caller would.
func (w *world) plan(id string, e Event) (Plan, *Refusal) {
	s := w.snap()
	var cons []Card
	all := map[string]Card{}
	for _, c := range s.Cards {
		all[c.ID] = fromStore(c)
		if c.Of == id {
			cons = append(cons, fromStore(c))
		}
	}
	p := all[id]
	slices.SortFunc(cons, func(a, b Card) int { return strings.Compare(a.ID, b.ID) })
	return Move(p, cons, w.rows(), StreamFromStore(s.Streams[p.Stream]), all, e)
}

func (w *world) move(id string, e Event) {
	w.t.Helper()
	plan, r := w.plan(id, e)
	require.Nil(w.t, r, "%s %s", name(e), id)
	w.op(kind(e), plan)
}

func (w *world) refused(id string, e Event, want string) {
	w.t.Helper()
	_, r := w.plan(id, e)
	refusal{r, want}.check(w.t)
}

func (w *world) consumerPlan(id string, e Event) Plan {
	w.t.Helper()
	c := w.card(id)
	p := w.card(c.Of)
	if v, ok := e.(Withdraw); ok {
		v.Stream = StreamFromStore(w.snap().Streams[p.Stream])
		e = v
	}
	plan, r := MoveConsumer(c, p, w.rows()[c.Holder], e)
	require.Nil(w.t, r, "%s %s", name(e), id)
	return plan
}

func (w *world) consume(id string, e Event) { w.t.Helper(); w.op(kind(e), w.consumerPlan(id, e)) }

func mustMerge(t *testing.T, plans ...Plan) Plan {
	t.Helper()
	p, r := Merge(plans...)
	require.Nil(t, r)
	return p
}

func (w *world) row(name string, e Event) {
	w.t.Helper()
	plan, r := MoveRow(w.rows()[name], e)
	require.Nil(w.t, r)
	w.op(kind(e), plan)
}

// landed checks the store holds what each write of plan names, for every
// write type the machine makes (an unchecked type hides an omission).
func (w *world) landed(plan Plan) {
	w.t.Helper()
	s := w.snap()
	for _, x := range plan.Writes {
		if !w.landedCard(s, x) && !w.landedRow(s, x) {
			w.t.Errorf("landed does not check %T", x)
		}
	}
}

func (w *world) landedCard(s store.Snapshot, x store.Write) bool {
	switch v := x.(type) {
	case store.SetCardReason:
		assert.Equal(w.t, v.Reason, s.Cards[v.Card].Reason, "reason of %s", v.Card)
	case store.SetCardFinding:
		assert.Equal(w.t, v.Finding, s.Cards[v.Card].Finding, "finding of %s", v.Card)
	case store.SetCardBranch:
		assert.Equal(w.t, v.Branch, s.Cards[v.Card].Branch, "branch of %s", v.Card)
	case store.SetCardBaseSHA:
		assert.Equal(w.t, v.SHA, s.Cards[v.Card].BaseSHA, "base of %s", v.Card)
	case store.SetCardScore:
		assert.Equal(w.t, v.Score, s.Cards[v.Card].Score, "score of %s", v.Card)
	case store.SetCardPriority:
		assert.Equal(w.t, cmp.Or(v.Priority, "normal"), s.Cards[v.Card].Priority, "priority of %s", v.Card)
	case store.SetCardProse:
		assert.Equal(w.t, v.Prose, s.Cards[v.Card].Prose, "prose of %s", v.Card)
	case store.SetCardPaths:
		assert.Equal(w.t, v.Paths, s.Cards[v.Card].Paths, "paths of %s", v.Card)
	case store.SetCardDoneWhen:
		assert.Equal(w.t, v.DoneWhen, s.Cards[v.Card].DoneWhen, "done-when of %s", v.Card)
	case store.SetCardResult:
		assert.Equal(w.t, v.Result, s.Cards[v.Card].Result, "result of %s", v.Card)
	case store.SetCardCost:
		assert.Equal(w.t, []any{v.Cost, v.Priced}, []any{s.Cards[v.Card].Cost, s.Cards[v.Card].Priced}, "cost of %s", v.Card)
	default:
		return w.landedCard2(s, x)
	}
	return true
}

func (w *world) landedCard2(s store.Snapshot, x store.Write) bool {
	switch v := x.(type) {
	case store.SetCardState:
		assert.Equal(w.t, v.State, s.Cards[v.Card].State, "state of %s", v.Card)
	case store.SetCardHead:
		assert.Equal(w.t, v.Head, s.Cards[v.Card].Head, "head of %s", v.Card)
	case store.SetCardAuthor:
		assert.Equal(w.t, v.Author, s.Cards[v.Card].Author, "author of %s", v.Card)
	case store.SetCardAttempt:
		assert.Equal(w.t, v.Attempt, s.Cards[v.Card].Attempt, "attempt of %s", v.Card)
	case store.SetCardBounces:
		assert.Equal(w.t, v.Bounces, s.Cards[v.Card].Bounces, "bounces of %s", v.Card)
	case store.SetCardNeeds:
		assert.ElementsMatch(w.t, v.Needs, s.Cards[v.Card].Needs, "needs of %s", v.Card)
	case store.SetCardReview:
		assert.Positive(w.t, s.Cards[v.Card].Review, "review of %s", v.Card)
	case store.SetCardRed:
		assert.Equal(w.t, v.Red, s.Cards[v.Card].Red, "red of %s", v.Card)
	case store.SetCardPriorHead:
		assert.Equal(w.t, v.Head, s.Cards[v.Card].Prior, "prior head of %s", v.Card)
	case store.PutCard:
		c := s.Cards[v.ID]
		assert.Equal(w.t, []any{v.Kind, v.Of, v.Holder, v.Tier, v.Attempt, v.Review, v.Finding, v.Brief, v.Prose, v.Stream},
			[]any{c.Kind, c.Of, c.Holder, c.Tier, c.Attempt, c.Review, c.Finding, c.Brief, c.Prose, c.Stream}, "card %s", v.ID)
	default:
		return false
	}
	return true
}

func (w *world) landedRow(s store.Snapshot, x store.Write) bool {
	switch v := x.(type) {
	case store.SetStreamHeld:
		assert.Equal(w.t, v.Held, s.Streams[v.Stream].Held, "held of %s", v.Stream)
	case store.SetRowFlaps:
		assert.Equal(w.t, v.Flaps, s.Rows[v.Row].Flaps, "flaps of %s", v.Row)
	case store.SetRowState:
		assert.Equal(w.t, v.State, s.Rows[v.Row].State, "state of %s", v.Row)
	case store.SetRowHeld:
		assert.Equal(w.t, []any{v.Held, v.Reason}, []any{s.Rows[v.Row].Held, s.Rows[v.Row].Reason}, "held of %s", v.Row)
	default:
		return false
	}
	return true
}

func (w *world) is(id, state string) { w.t.Helper(); assert.Equal(w.t, state, w.card(id).State, id) }

// work starts id on row, takes it and reports result with head.
func (w *world) work(id, row, result, head, reason string) string {
	w.t.Helper()
	w.move(id, Start{Row: row, Branch: "b-" + id, Base: base})
	c := workID(id, w.card(id).Attempt)
	w.consume(c, Report{Row: row, Result: result, Head: head, Reason: reason, Cost: 7, Priced: true})
	return c
}

// read asks row for a read of id's head, takes it and reports result.
func (w *world) read(id, row, result, reason string) string {
	w.t.Helper()
	before := w.snap().Cards
	w.move(id, AskRead{Row: row})
	var c string
	for k := range w.snap().Cards {
		if _, ok := before[k]; !ok {
			c = k
		}
	}
	w.consume(c, Report{Row: row, Result: result, Head: w.card(id).Head, Reason: reason})
	return c
}

// land takes a resolved producer to landed through the happy path.
func (w *world) land(id string, head string) {
	w.t.Helper()
	w.move(id, Resolve{})
	w.move(id, Finished{Consumer: w.work(id, "r1", "ok", head, "")})
	w.read(id, "r2", "ok", "")
	w.move(id, ReadYes{})
	w.move(id, Landed{Head: head})
}

func TestConformanceHappy(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.is("p1", Ready)
	c := w.work("p1", "r1", "ok", h1, "")
	assert.Equal(t, "p1.w1", c)
	assert.Equal(t, []any{"b-p1", base, ""}, []any{w.card(c).Branch, w.card(c).BaseSHA, w.card(c).PriorHead})
	w.move("p1", Finished{Consumer: c})
	w.is("p1", Review)
	assert.Equal(t, []any{h1, "r1"}, []any{w.card("p1").Head, w.card("p1").Author})
	w.refused("p1", AskRead{Row: "r1"}, "r1 wrote producer p1's head and may not read it")
	r := w.read("p1", "r2", "ok", "")
	assert.Equal(t, "p1.r1.1", r)
	assert.Equal(t, []any{h1, "r1"}, []any{w.card(r).Head, w.card(r).Author})
	w.move("p1", ReadYes{})
	w.is("p1", Merging)
	w.refused("p1", Landed{Head: h2}, "producer p1 is at head "+h1+", Landed names "+h2)
	w.move("p1", Landed{Head: h1})
	w.is("p1", LandedState)
	w.refused("p1", Rescore{Score: 1}, "producer p1 is landed; nothing moves a landed card")
}

func TestConformanceFix(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.read("p1", "r2", "fix", "rename x")
	w.move("p1", ReadFix{Finding: "rename x"})
	p := w.card("p1")
	assert.Equal(t, []any{Ready, 1, "rename x"}, []any{p.State, p.Bounces, p.Finding})
	w.move("p1", SetPriority{Level: "high", Reason: "raised for the release"}) // a status reason, not the finding
	w.move("p1", Start{Row: "r3", Branch: "b-p1", Base: base})
	c := w.card("p1.w2")
	assert.Equal(t, []any{2, h1, "rename x", ""}, []any{c.Attempt, c.PriorHead, c.Finding, c.Reason}, "the fix continues from the head, with the finding")
	assert.Equal(t, "", w.card("p1").Author, "a new attempt clears the author")
	w.consume("p1.w2", Report{Row: "r3", Result: "ok", Head: h2})
	w.move("p1", Finished{Consumer: "p1.w2"})
	assert.Equal(t, []any{h2, "r3"}, []any{w.card("p1").Head, w.card("p1").Author})
	w.refused("p1", ReadYes{}, "producer p1 has no read of review 2 that said ok; ReadYes needs one")
	assert.Equal(t, "p1.r2.1", w.read("p1", "r1", "ok", ""))
	w.move("p1", ReadYes{})
	w.move("p1", Landed{Head: h2})
	w.is("p1", LandedState)
}

func TestConformanceRed(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	w.read("p1", "r2", "ok", "")
	w.move("p1", ReadYes{})
	w.move("p1", Red{Head: h1, Reason: "ci red"})
	w.is("p1", Ready) // a red goes to fix with the CI finding
	assert.Equal(t, 2, w.card("p1").Review, "Finished and Red each raise the review")
	assert.Equal(t, "ci red", w.card("p1").Finding)
	w.refused("p1", ReadYes{}, "producer p1 is in ready, ReadYes needs review")
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h2, "")})
	assert.Equal(t, 3, w.card("p1").Review)
	w.read("p1", "r3", "ok", "")
	w.move("p1", ReadYes{})
	w.move("p1", Landed{Head: h2})
}

func TestConformanceFaultAndBroken(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Faulted{Consumer: w.work("p1", "r1", "fault", "", "tool lost")})
	p := w.card("p1")
	assert.Equal(t, []any{Ready, 0, "tool lost"}, []any{p.State, p.Bounces, p.Reason})
	w.move("p1", Broken{Consumer: w.work("p1", "r1", "fail", h2, "tests red")})
	p = w.card("p1")
	assert.Equal(t, []any{Ready, 1, "tests red", h2}, []any{p.State, p.Bounces, p.Reason, p.Head})
	w.move("p1", Finished{Consumer: w.work("p1", "r1", "ok", h1, "")})
	assert.Equal(t, "p1.w3", workID("p1", w.card("p1").Attempt))
}

func TestConformanceReleaseAndWithdraw(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.refused("p1", Released{}, "consumer p1.w1 is working; Released needs it withdrawn (fail)")
	w.refused("p1", Cancel{}, "producer p1 has live consumer p1.w1 on row r1; withdraw it first")
	w.consume("p1.w1", Withdraw{Reason: "row r1 went silent", Head: h2}) // releases p1 in the same plan
	p := w.card("p1")
	assert.Equal(t, []any{Ready, "row r1 went silent", 0, h2}, []any{p.State, p.Reason, p.Bounces, p.Head})
	assert.Equal(t, []any{Fail, h2}, []any{w.card("p1.w1").State, w.card("p1.w1").Head})
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
	assert.Equal(t, h2, w.card("p1.w2").PriorHead, "the next attempt continues from the withdrawn head")
	w.consume("p1.w2", Withdraw{Reason: "cancelling"}) // a ready consumer withdraws too
	w.move("p1", Cancel{Reason: "not wanted"})
	w.is("p1", Fail)
}

func TestConformanceBlockAndPark(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Block{Reason: "bounced too often"})
	w.refused("p1", Resolve{}, "producer p1 is in blocked, Resolve needs waiting")
	w.move("p1", Unblock{Reason: "brief rewritten"})
	w.is("p1", Waiting)
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.refused("p1", Block{}, "producer p1 has live consumer p1.w1 on row r1; withdraw it first")
	w.refused("p1", Edit{Prose: "x"}, "producer p1 is held by r1 in consumer p1.w1; message r1 instead")
	w.consume("p1.w1", Withdraw{Reason: "x"})
	w.move("p1", Edit{Prose: "do it smaller", Paths: []string{"card/"}, DoneWhen: []string{"tests pass"}})
	w.move("p1", Rescore{Score: 4})
	w.move("p1", SetPriority{Level: "high"})
	assert.Equal(t, []any{"do it smaller", 4, "high"}, []any{w.card("p1").Prose, w.card("p1").Score, w.card("p1").Priority})
	w.raw("hold", store.SetStreamHeld{Stream: "s1", Held: true})
	w.refused("p1", Start{Row: "r1", Branch: "b", Base: base}, "stream s1 is held; no new consumers start in a held stream")
	w.move("p1", Park{})
	p := w.card("p1")
	assert.Equal(t, []any{Waiting, "stream s1 is held"}, []any{p.State, p.Reason})
}

func TestConformanceNeeds(t *testing.T) {
	w := newWorld(t, producer("n1"), producer("n2"), producer("p2", "n1"), producer("p3", "n1"), producer("p4", "n1"))
	w.refused("p2", Resolve{}, "need n1 of producer p2 is waiting, not landed")
	w.move("n1", Cancel{Reason: "dropped"})
	w.move("p2", NeedFailed{Need: "n1"})
	assert.Equal(t, []any{Fail, "need n1 failed"}, []any{w.card("p2").State, w.card("p2").Reason})
	w.move("p3", Waive{Need: "n1", Reason: "not needed"})
	assert.Empty(t, w.card("p3").Needs)
	w.move("p3", Resolve{})
	w.move("p4", Relink{Need: "n1", To: "n2", Reason: "n2 does it"})
	assert.Equal(t, []string{"n2"}, w.card("p4").Needs)
	w.land("n2", h1)
	w.move("p4", Resolve{})
	w.is("p4", Ready)
	w.move("p4", Replace{By: "p3", Reason: "same work"})
	assert.Equal(t, "replaced by p3: same work", w.card("p4").Reason)
}

func TestConformanceSentinel(t *testing.T) {
	w := newWorld(t, producer("p1"), store.PutCard{ID: "g1", Kind: "sentinel", Needs: []string{"p1"}})
	w.refused("g1", Reached{}, "need p1 of sentinel g1 is waiting, not landed")
	w.refused("g1", Resolve{}, "sentinel g1 is landed by Reached, never resolved")
	w.land("p1", h1)
	w.move("g1", Reached{})
	w.is("g1", LandedState)
}

// Blocker preemption: the withdraw of the youngest consumer on the full row
// and the start of the blocker are one op, and the withdraw releases the
// preempted producer in it.
func TestConformancePreemption(t *testing.T) {
	w := newWorld(t, producer("p1"), store.PutCard{ID: "p2", Priority: "blocker"})
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
	w.move("p2", Resolve{})
	start, r := w.plan("p2", Start{Row: "r2", Branch: "b", Base: base})
	require.Nil(t, r)
	w.op("preempt", mustMerge(t, w.consumerPlan("p1.w1", Withdraw{Reason: "preempted by blocker p2", Preempt: true}), start))
	w.is("p2.w1", Working)
	assert.Equal(t, []any{Ready, "preempted by blocker p2"}, []any{w.card("p1").State, w.card("p1").Reason})
}

func TestConformanceRows(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.row("r1", Hold{Reason: "maintenance"})
	assert.Equal(t, "maintenance", w.rows()["r1"].Reason)
	w.refused("p1", Start{Row: "r1", Branch: "b", Base: base}, "row r1 is held; nothing is dealt to a held row")
	w.row("r1", Unhold{Reason: "back"})
	w.row("r1", Gone{})
	assert.Equal(t, 1, w.rows()["r1"].Flaps)
	w.refused("p1", Start{Row: "r1", Branch: "b", Base: base}, "row r1 is down; a consumer is dealt only to a row that is up")
	w.row("r1", Seen{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
}

// p relinks a to
// q and r relinks b to p, both planned from one snapshot. Applied in turn,
// the second is refused by its gen fence on r (its walk read q, r and b), so
// no cycle forms.
func TestConformanceRelinkStale(t *testing.T) {
	w := newWorld(t, producer("a"), producer("b"), producer("r", "b"), producer("q", "r"), producer("p", "a"))
	w.move("a", Cancel{Reason: "x"})
	w.move("b", Cancel{Reason: "x"})
	pp, r1 := w.plan("p", Relink{Need: "a", To: "q"})
	rp, r2 := w.plan("r", Relink{Need: "b", To: "p"})
	require.Nil(t, r1)
	require.Nil(t, r2)
	assert.Contains(t, pp.Expect, store.Expect(store.CardGen{Card: "r", Gen: w.card("r").Gen}), "p's walk read r")
	w.op("relink-r", rp)
	got, err := w.apply("relink-p", pp)
	require.NoError(t, err)
	assert.False(t, got.Accepted, "the stale relink is refused")
	assert.Equal(t, "card r is at gen 1, expected gen 0", got.Reason)
	assert.Equal(t, []string{"a"}, w.card("p").Needs, "the graph stays acyclic")
	w.refused("p", Relink{Need: "a", To: "q"}, "p needs q needs r needs p")
}

// For both cut kinds, a consumer
// carries its producer's brief, prose, paths, done_when, stream and tier.
func TestConformanceCutCopiesTask(t *testing.T) {
	w := newWorld(t, store.PutCard{ID: "p", Prose: "write the feature", Paths: []string{"src/"}, DoneWhen: []string{"go test ./..."}})
	w.move("p", Resolve{})
	work := w.work("p", "r1", "ok", h1, "")
	w.move("p", Finished{Consumer: work})
	w.move("p", AskRead{Row: "r2"})
	p := w.snap().Cards["p"]
	for _, id := range []string{work, "p.r1.1"} {
		c := w.snap().Cards[id]
		assert.Equal(t, []any{p.Brief, p.Prose, p.Paths, p.DoneWhen, p.Stream, p.Tier}, []any{c.Brief, c.Prose, c.Paths, c.DoneWhen, c.Stream, c.Tier}, id)
	}
	assert.Equal(t, "do p", w.snap().Cards[work].Brief)
}

// A work consumer cannot
// report fix, so no work card is left done with a result nothing handles.
func TestConformanceWorkFixRefused(t *testing.T) {
	w := newWorld(t, producer("p"))
	w.move("p", Resolve{})
	w.move("p", Start{Row: "r1", Branch: "b", Base: base})
	c := w.card("p.w1")
	_, r := MoveConsumer(c, w.card("p"), w.rows()["r1"], Report{Row: "r1", Result: "fix", Reason: "fix"})
	refusal{r, "a work consumer reports ok, fail or fault; fix is a read's result"}.check(t)
	w.is("p.w1", Working)
}

// The hold and the parks of the ready producers are one op the store accepts.
func TestConformanceHoldStream(t *testing.T) {
	w := newWorld(t, producer("p1"), producer("p2"), producer("p3"))
	w.move("p1", Resolve{})
	w.move("p2", Resolve{})
	plan, r := MoveStream(Stream{Name: "s1"}, HoldStream{Reason: "release freeze", Ready: []Card{w.card("p1"), w.card("p2")}, Seq: w.snap().Seq})
	require.Nil(t, r)
	w.op("hold", plan)
	assert.True(t, w.snap().Streams["s1"].Held)
	for _, id := range []string{"p1", "p2"} {
		assert.Equal(t, []any{Waiting, "release freeze"}, []any{w.card(id).State, w.card(id).Reason}, id)
	}
	w.refused("p3", Resolve{}, "stream s1 is held; no new consumers start in a held stream")
}

// A hold is fenced at the snapshot it was read from. A producer that becomes
// ready after that snapshot is not parked by the stale plan: the store
// refuses the op, and nothing in it lands.
func TestConformanceHoldStreamFencedAtSeq(t *testing.T) {
	w := newWorld(t, producer("p1"), producer("p2"))
	w.move("p1", Resolve{})
	snap := w.snap()
	stale, r := MoveStream(StreamFromStore(snap.Streams["s1"]), HoldStream{
		Reason: "release freeze", Ready: []Card{FromStore(snap.Cards["p1"])}, Seq: snap.Seq,
	})
	require.Nil(t, r)
	w.move("p2", Resolve{})
	rc, err := w.apply("stale-hold", stale)
	require.NoError(t, err)
	assert.False(t, rc.Accepted)
	assert.Contains(t, rc.Reason, "expected seq")
	assert.False(t, w.snap().Streams["s1"].Held)
	w.is("p1", Ready)
	w.is("p2", Ready)
	fresh := w.snap()
	plan, r := MoveStream(StreamFromStore(fresh.Streams["s1"]), HoldStream{
		Reason: "release freeze",
		Ready:  []Card{FromStore(fresh.Cards["p1"]), FromStore(fresh.Cards["p2"])},
		Seq:    fresh.Seq,
	})
	require.Nil(t, r)
	w.op("hold", plan)
	assert.True(t, w.snap().Streams["s1"].Held)
	w.is("p1", Waiting)
	w.is("p2", Waiting)
}

// A return to ready while the stream is held parks
// the producer in the same plan.
func TestConformanceReturnIntoHeldStreamParks(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	c := w.work("p1", "r1", "fail", h2, "tests red")
	w.raw("hold", store.SetStreamHeld{Stream: "s1", Held: true})
	w.move("p1", Broken{Consumer: c})
	assert.Equal(t, []any{Waiting, 1}, []any{w.card("p1").State, w.card("p1").Bounces})
	w.raw("unhold", store.SetStreamHeld{Stream: "s1", Held: false})
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	w.raw("hold", store.SetStreamHeld{Stream: "s1", Held: true})
	w.consume("p1.w2", Withdraw{Reason: "silent"})
	w.is("p1", Waiting)
}

// A drop and its dependants' moves are one op (NeedFailed and Waive with
// InThisOp), and the store accepts it.
func TestConformanceDropInOneOp(t *testing.T) {
	w := newWorld(t, producer("n1"), producer("p2", "n1"), producer("p3", "n1"))
	drop, r := w.plan("n1", Cancel{Reason: "dropped"})
	require.Nil(t, r)
	fail, r := w.plan("p2", NeedFailed{Need: "n1", InThisOp: true})
	require.Nil(t, r)
	waive, r := w.plan("p3", Waive{Need: "n1", Reason: "not needed", InThisOp: true})
	require.Nil(t, r)
	w.op("drop", mustMerge(t, drop, fail, waive))
	assert.Equal(t, []any{Fail, Fail, "need n1 failed"}, []any{w.card("n1").State, w.card("p2").State, w.card("p2").Reason})
	assert.Empty(t, w.card("p3").Needs)
}

// Add's plan is one the store accepts, and the card is as given.
func TestConformanceAdd(t *testing.T) {
	w := newWorld(t, producer("n1"))
	all, _ := w.all()
	plan, r := MoveAdd(Add{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Needs: []string{"n1"}, Brief: "do p1", Prose: "x"}, all)
	require.Nil(t, r)
	w.op("add", plan)
	assert.Equal(t, []any{"producer", Waiting, []string{"n1"}, "do p1"}, []any{w.card("p1").Kind, w.card("p1").State, w.card("p1").Needs, w.card("p1").Brief})
	all, _ = w.all()
	plan, r = MoveAdd(Add{ID: "g1", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: []string{"p1"}}, all)
	require.Nil(t, r)
	w.op("add", plan)
	_, r = MoveAdd(Add{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"}, all)
	refusal{r, "card p1 exists"}.check(t)
}
