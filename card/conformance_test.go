package card

import (
	"context"
	"fmt"
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
	t *testing.T
	f *store.Fake
	n int
}

// newWorld is stream s1 with rows r1 (width 2), r2 and r3 (width 1), all up
// and serving pro, and the given producers (id, needs) of tier pro.
func newWorld(t *testing.T, cards ...store.PutCard) *world {
	w := &world{t: t, f: store.NewFake()}
	seed := []store.Write{store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1}}
	for _, r := range []store.PutRow{{Name: "r1", Kind: "worker", Width: 2}, {Name: "r2", Kind: "worker", Width: 1}, {Name: "r3", Kind: "worker", Width: 1}} {
		r.Tiers = []string{"pro"}
		seed = append(seed, r)
	}
	for _, c := range cards {
		c.Stream, c.Tier = "s1", "pro"
		if c.Kind == "" {
			c.Kind, c.Brief = "producer", "do "+c.ID
		}
		seed = append(seed, c)
	}
	w.op("seed", Plan{Writes: seed})
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

// op applies plan as one op and checks the rows it names.
func (w *world) op(kind string, plan Plan) {
	w.t.Helper()
	w.n++
	op := store.Op{ID: fmt.Sprintf("op%d", w.n), Epoch: w.snap().Epoch, Actor: "card", Kind: kind, At: t0.Add(time.Duration(w.n) * time.Second),
		Expect: plan.Expect, Writes: plan.Writes}
	_, err := w.f.Apply(context.Background(), op)
	require.NoError(w.t, err, "%s: %+v", kind, plan)
	w.landed(plan)
}

func kind(e Event) string { return strings.ToLower(name(e)) }

func (w *world) card(id string) Card {
	w.t.Helper()
	c, ok := w.snap().Cards[id]
	require.True(w.t, ok, "card %s", id)
	return fromStore(c)
}

func fromStore(c store.Card) Card {
	return Card{ID: c.ID, Kind: c.Kind, Of: c.Of, Stream: c.Stream, State: c.State, Gen: c.Gen, Attempt: c.Attempt, Priority: c.Priority,
		Tier: c.Tier, Score: c.Score, Needs: c.Needs, After: c.After, Holder: c.Holder, Author: c.Author, Branch: c.Branch,
		BaseSHA: c.BaseSHA, PriorHead: c.Prior, Head: c.Head, PR: c.PR, Result: c.Result, Reason: c.Reason, Bounces: c.Bounces,
		Cost: c.Cost, Priced: c.Priced, Prose: c.Prose, Paths: c.Paths, DoneWhen: c.DoneWhen, Review: c.Review}
}

func (w *world) rows() map[string]Row {
	rs := map[string]Row{}
	for n, r := range w.snap().Rows {
		rs[n] = Row{Name: r.Name, State: r.State, Width: r.Width, Gen: r.Gen, Flaps: r.Flaps, Held: r.Held, Reason: r.Reason, Tiers: r.Tiers}
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
	st := s.Streams[p.Stream]
	return Move(p, cons, w.rows(), Stream{Name: st.Name, Held: st.Held}, all, e)
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
	plan, r := MoveConsumer(c, w.card(c.Of), w.rows()[c.Holder], e)
	require.Nil(w.t, r, "%s %s", name(e), id)
	return plan
}

func (w *world) consume(id string, e Event) { w.t.Helper(); w.op(kind(e), w.consumerPlan(id, e)) }

func (w *world) row(name string, e Event) {
	w.t.Helper()
	plan, r := MoveRow(w.rows()[name], e)
	require.Nil(w.t, r)
	w.op(kind(e), plan)
}

// landed checks the store holds what each write of plan names.
func (w *world) landed(plan Plan) {
	w.t.Helper()
	s := w.snap()
	for _, x := range plan.Writes {
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
		case store.SetCardPriorHead:
			assert.Equal(w.t, v.Head, s.Cards[v.Card].Prior, "prior head of %s", v.Card)
		case store.PutCard:
			c := s.Cards[v.ID]
			assert.Equal(w.t, []any{v.Kind, v.Of, v.Holder, v.Tier, v.Attempt}, []any{c.Kind, c.Of, c.Holder, c.Tier, c.Attempt}, "card %s", v.ID)
		case store.SetRowState:
			assert.Equal(w.t, v.State, s.Rows[v.Row].State, "state of %s", v.Row)
		case store.SetRowHeld:
			assert.Equal(w.t, v.Held, s.Rows[v.Row].Held, "held of %s", v.Row)
		case store.SetRowWidth:
			assert.Equal(w.t, v.Width, s.Rows[v.Row].Width, "width of %s", v.Row)
		}
	}
}

func (w *world) is(id, state string) { w.t.Helper(); assert.Equal(w.t, state, w.card(id).State, id) }

// work starts id on row, takes it and reports result with head.
func (w *world) work(id, row, result, head, reason string) string {
	w.t.Helper()
	w.move(id, Start{Row: row, Branch: "b-" + id, Base: base})
	c := workID(id, w.card(id).Attempt)
	w.consume(c, Take{Row: row})
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
	w.consume(c, Take{Row: row})
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
	w.move("p1", ReadFix{Reason: "rename x"})
	p := w.card("p1")
	assert.Equal(t, []any{Ready, 1, "rename x"}, []any{p.State, p.Bounces, p.Reason})
	w.move("p1", Start{Row: "r3", Branch: "b-p1", Base: base})
	c := w.card("p1.w2")
	assert.Equal(t, []any{2, h1, "rename x"}, []any{c.Attempt, c.PriorHead, c.Reason}, "the fix continues from the head")
	assert.Equal(t, "", w.card("p1").Author, "a new attempt clears the author")
	w.consume("p1.w2", Take{Row: "r3"})
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
	w.is("p1", Review)
	assert.Equal(t, 2, w.card("p1").Review, "Finished and Red each raise the review")
	w.refused("p1", ReadYes{}, "producer p1 has no read of review 2 that said ok; ReadYes needs one")
	assert.Equal(t, "p1.r2.1", w.read("p1", "r3", "ok", ""), "a new read, cut at the new review")
	assert.Equal(t, 2, w.card("p1.r2.1").Review)
	assert.Equal(t, 1, w.card("p1.r1.1").Review, "the old read keeps the review it was cut at")
	w.move("p1", ReadYes{})
	w.move("p1", Landed{Head: h1})
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
	w.consume("p1.w1", Take{Row: "r1"})
	w.refused("p1", Released{}, "consumer p1.w1 is working; Released needs it withdrawn (fail)")
	w.refused("p1", Cancel{}, "producer p1 has live consumer p1.w1 on row r1; withdraw it first")
	w.consume("p1.w1", Withdraw{Reason: "row r1 went silent"})
	w.move("p1", Released{})
	p := w.card("p1")
	assert.Equal(t, []any{Ready, "row r1 went silent", 0}, []any{p.State, p.Reason, p.Bounces})
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
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
	w.move("p1", Released{})
	w.move("p1", Edit{Prose: "do it smaller", Paths: []string{"card/"}, DoneWhen: []string{"tests pass"}})
	w.move("p1", Rescore{Score: 4})
	w.move("p1", SetPriority{Level: "high"})
	assert.Equal(t, []any{"do it smaller", 4, "high"}, []any{w.card("p1").Prose, w.card("p1").Score, w.card("p1").Priority})
	w.op("hold", Plan{Writes: []store.Write{store.SetStreamHeld{Stream: "s1", Held: true}}})
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
// and the start of the blocker are one op; Released returns the preempted.
func TestConformancePreemption(t *testing.T) {
	w := newWorld(t, producer("p1"), store.PutCard{ID: "p2", Priority: "blocker"})
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r2", Branch: "b", Base: base})
	w.consume("p1.w1", Take{Row: "r2"})
	w.move("p2", Resolve{})
	start, r := w.plan("p2", Start{Row: "r2", Branch: "b", Base: base})
	require.Nil(t, r)
	w.op("preempt", Merge(w.consumerPlan("p1.w1", Withdraw{Reason: "preempted by blocker p2"}), start))
	w.consume("p2.w1", Take{Row: "r2"})
	w.is("p2.w1", Working)
	w.move("p1", Released{})
	assert.Equal(t, []any{Ready, "preempted by blocker p2"}, []any{w.card("p1").State, w.card("p1").Reason})
}

func TestConformanceRows(t *testing.T) {
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.row("r1", Hold{Reason: "maintenance"})
	assert.Equal(t, "maintenance", w.rows()["r1"].Reason)
	w.refused("p1", Start{Row: "r1", Branch: "b", Base: base}, "row r1 is held; nothing is dealt to a held row")
	w.row("r1", Unhold{Reason: "back"})
	w.row("r1", Width{N: 3})
	w.row("r1", Gone{})
	assert.Equal(t, 1, w.rows()["r1"].Flaps)
	w.refused("p1", Start{Row: "r1", Branch: "b", Base: base}, "row r1 is down; a consumer is dealt only to a row that is up")
	w.row("r1", Seen{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
}
