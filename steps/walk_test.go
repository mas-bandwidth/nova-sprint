package steps

import (
	"fmt"
	"math/rand"
	"slices"
	"sync"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWalk is the random walk of the steps on store.Fake: 100 seeds of 2,000
// steps over hand-built rows and cards; each step a tick or an outside move
// (a take, a report, an edge result, a hold, a row going or coming, a verb).
// After every step the model's state invariants are checked on the store;
// after every tick its tick invariants on the plan. The sim folds and
// applies at once, so no race is possible and every op must be accepted
// (RefusedOnlyByRace). At least 40% of the picked moves must be legal.
func TestWalk(t *testing.T) {
	t.Parallel()
	steps := 2000
	if testing.Short() {
		steps = 300
	}
	var mu sync.Mutex
	picked, legal := 0, 0
	kinds := map[string]int{}
	for seed := int64(1); seed <= 100; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			w := newWalk(t, seed)
			p, l := w.run(steps)
			mu.Lock()
			defer mu.Unlock()
			picked += p
			legal += l
			for k, n := range w.kinds {
				kinds[k] += n
			}
		})
	}
	t.Cleanup(func() {
		t.Logf("walk: %d picked, %d legal (%.0f%%); groups by kind %v", picked, legal, 100*float64(legal)/float64(max(picked, 1)), kinds)
		assert.GreaterOrEqual(t, float64(legal), 0.4*float64(picked), "depth: at least 40%% of picked moves legal")
		// park is a defence (the hold's own op parks; tla/README.md "Not
		// modelled"), held by TestTickHolds alone.
		for _, k := range []string{"resolve", "start", "read", "yes", "fix", "land", "pre", "withdraw", "drop", "reached", "hold"} {
			assert.Positive(t, kinds[k], "the walk never planned a %s group", k)
		}
	})
}

// walk is one seed's world: the sim, a rng and the counts.
type walk struct {
	*sim
	rng   *rand.Rand
	kinds map[string]int
	heads int
}

func newWalk(t *testing.T, seed int64) *walk {
	w := &walk{sim: newSim(t), rng: rand.New(rand.NewSource(seed)), kinds: map[string]int{}}
	// A policy version with a low bounce limit, so Block is reached in the
	// walk's depth; the rest of the answers are the defaults, so the view's
	// limits path is exercised too.
	w.ask = scripted{Answers{Version: 1, Models: benchModels(), Limits: &Limits{Bounces: 3, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3, Budget: 250, Pack: 50}}}
	w.stream("s1")
	w.stream("s2")
	w.row("r1", 1, "pro")
	w.row("r2", 2, "pro", "heavy")
	w.row("r3", 1, "heavy", "frontier")
	w.producer("p1", "s1", "pro", "a/x")
	w.producer("p2", "s1", "pro", "a/y")
	w.producer("p3", "s1", "heavy", "a/x", "a/z")
	w.producer("p4", "s2", "pro", "b/x")
	w.producer("p5", "s2", "heavy", "b/y")
	w.must("add", store.PutCard{ID: "p6", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p6", Needs: []string{"p1"}, Paths: []string{"a/w"}})
	w.must("add", store.PutCard{ID: "p7", Kind: "producer", Stream: "s2", Tier: "frontier", Brief: "do p7", Needs: []string{"p4"}, After: []string{"p5"}, Paths: []string{"b/z"}})
	w.must("add", store.PutCard{ID: "p8", Kind: "producer", Stream: "s2", Tier: "pro", Brief: "do p8", After: []string{"p4"}, Paths: []string{"b/w"}})
	w.must("add", store.PutCard{ID: "g1", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: []string{"p6", "p2"}})
	w.must("add", store.PutCard{ID: "g2", Kind: "sentinel", Stream: "s2", Tier: "pro", Needs: []string{"p7"}})
	w.must("add", store.PutCard{ID: "g3", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: []string{"p2"}})
	return w
}

// run walks n steps and returns how many moves were picked and how many
// were legal.
func (w *walk) run(n int) (picked, legal int) {
	for i := 0; i < n; i++ {
		ok, counted := w.step()
		if counted {
			picked++
			if ok {
				legal++
			}
		}
		w.check(fmt.Sprintf("seed step %d", i))
	}
	return picked, legal
}

// step picks one move. A tick is not counted as a pick: it is the engine's.
// Most picks are of moves with a subject (the walk gets deep); some are
// blind, so refusals are exercised too.
func (w *walk) step() (ok, counted bool) {
	if w.rng.Intn(10) < 3 {
		w.ticked()
		return true, false
	}
	moves := []func() bool{w.takeOne, w.reportOne, w.edgeOne, w.envOne, w.verbOne}
	if w.rng.Intn(4) > 0 {
		moves = w.enabled()
	}
	if len(moves) == 0 {
		return false, false
	}
	return moves[w.rng.Intn(len(moves))](), true
}

// enabled is the moves with a subject now.
func (w *walk) enabled() []func() bool {
	var out []func() bool
	if len(w.unacked()) > 0 {
		out = append(out, w.takeOne)
	}
	if len(w.consumersIn("working")) > 0 {
		out = append(out, w.reportOne)
	}
	for _, r := range w.inflight {
		if _, sent := w.sent[r.ID]; !sent {
			out = append(out, w.edgeOne)
			break
		}
	}
	return append(out, w.envOne, w.verbOne)
}

// ticked runs one tick and checks the tick invariants on its plan.
func (w *walk) ticked() {
	w.t.Helper()
	plan := w.tick()
	w.settle()
	require.Empty(w.t, w.rejected, "an op refused with no race: %+v", w.rejected)
	seen := set{}
	touched, nonBlockers := 0, 0
	for _, op := range tickOnly(plan.Ops) {
		for _, g := range OpGroups(op) {
			for _, c := range g.Cards {
				_, dup := seen[c]
				assert.False(w.t, dup, "OneGroupPerCard: %s in two groups", c)
				seen[c] = struct{}{}
			}
			w.kinds[g.Kind]++
			if !isBlockerGroup(op, g) {
				nonBlockers++
				if nonBlockers > 1 {
					touched += len(g.Cards)
				}
			}
		}
	}
	assert.LessOrEqual(w.t, touched, 250, "BudgetKept")
	for _, e := range plan.Edges {
		if e.Kind == "stop" {
			assert.NotEmpty(w.t, e.After, "a stop waits on its op")
		}
	}
	w.carryFIFO(plan)
}

// carryFIFO: the carry is a subsequence of the inbox in arrival order.
func (w *walk) carryFIFO(plan Plan) {
	j := 0
	for _, it := range w.inbox {
		if j < len(plan.Carry) && plan.Carry[j].ID == it.ID {
			j++
		}
	}
	assert.Equal(w.t, len(plan.Carry), j, "CarryFIFO: the carry is not in arrival order")
}

func isBlockerGroup(op store.Op, g GroupKey) bool {
	if g.Kind == "pre" {
		return true
	}
	for _, wr := range op.Writes {
		if x, ok := wr.(store.PutCard); ok && slices.Contains(g.Cards, x.ID) && x.Kind == "work" {
			return false
		}
	}
	return false
}

// pick is a random element, "" when none.
func (w *walk) pick(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[w.rng.Intn(len(ids))]
}

func (w *walk) consumersIn(state string) []string {
	var out []string
	for _, c := range w.snap().Cards {
		if isConsumer(c) && c.State == state {
			out = append(out, c.ID)
		}
	}
	slices.Sort(out)
	return out
}

// takeOne acknowledges a dealt consumer, or any card (an illegal pick):
// the take verb refuses what is not working in a row's hand, and a second
// acknowledgement writes nothing.
func (w *walk) takeOne() bool {
	ids := w.unacked()
	if w.rng.Intn(4) == 0 {
		ids = sortedKeys(w.snap().Cards)
	}
	id := w.pick(ids)
	if id == "" {
		return false
	}
	if c := w.snap().Cards[id]; !isConsumer(c) || c.State != "working" {
		return false
	}
	return w.ack(id).Accepted
}

// reportOne reports a working consumer with a result its kind allows.
func (w *walk) reportOne() bool {
	id := w.pick(w.consumersIn("working"))
	if id == "" {
		return false
	}
	c := w.snap().Cards[id]
	results := []string{"ok", "ok", "fail", "fault"}
	head := ""
	if c.Kind == "read" {
		results = []string{"ok", "ok", "fix", "fault"}
		head = c.Head
	}
	res := results[w.rng.Intn(len(results))]
	if c.Kind == "work" && res == "ok" {
		w.heads++
		head = sha(100 + w.heads)
	}
	pl, r := w.reportPlan(id, res, head, res+" by the walk")
	if r != nil {
		return false
	}
	return w.apply("report", pl.Expect, pl.Writes...).Accepted
}

// edgeOne answers a request in flight, once or twice.
func (w *walk) edgeOne() bool {
	var open []EdgeRequest
	for _, r := range w.inflight {
		if _, sent := w.sent[r.ID]; !sent {
			open = append(open, r)
		}
	}
	if len(open) == 0 {
		return false
	}
	req := open[w.rng.Intn(len(open))]
	it := InboxItem{Kind: req.Kind, Request: req.ID, Outcome: "ack"}
	if req.Kind == "land" {
		switch w.rng.Intn(6) {
		case 0:
			it.Outcome = "ci_red"
		case 1:
			it.Outcome = "fault"
		case 2:
			it.Outcome = "conflict"
		default:
			it.Outcome, it.Landed = "landed", req.Heads
		}
	}
	w.arrive(it)
	if w.rng.Intn(5) == 0 {
		w.arrive(it)
	}
	return true
}

// envOne holds or lifts a stream, holds or lifts a row, or flips a row.
func (w *walk) envOne() bool {
	s := w.snap()
	switch w.rng.Intn(4) {
	case 0:
		name := w.pick(sortedKeys(s.Streams))
		st := card.StreamFromStore(s.Streams[name])
		var e card.Event = card.HoldStream{Reason: "walk", Ready: w.readyOf(name), Seq: s.Seq}
		if st.Held {
			e = card.UnholdStream{Reason: "walk"}
		}
		pl, r := card.MoveStream(st, e)
		return r == nil && w.apply("stream", pl.Expect, pl.Writes...).Accepted
	case 1:
		name := w.pick(sortedKeys(s.Rows))
		row := w.cardRow(s.Rows[name])
		var e card.Event = card.Hold{Reason: "walk"}
		if row.Held {
			e = card.Unhold{Reason: "walk"}
		}
		pl, r := card.MoveRow(row, e)
		return r == nil && w.apply("row", pl.Expect, pl.Writes...).Accepted
	default:
		name := w.pick(sortedKeys(s.Rows))
		row := w.cardRow(s.Rows[name])
		var e card.Event = card.Gone{}
		if row.State == "down" {
			e = card.Seen{}
		}
		pl, r := card.MoveRow(row, e)
		return r == nil && w.apply("row", pl.Expect, pl.Writes...).Accepted
	}
}

func (w *walk) readyOf(stream string) []card.Card {
	var out []card.Card
	for _, id := range sortedKeys(w.snap().Cards) {
		if c := w.snap().Cards[id]; c.Kind == "producer" && c.Stream == stream && c.State == "ready" {
			out = append(out, card.FromStore(c))
		}
	}
	return out
}

// verbOne is a coordinator's verb on a random producer.
func (w *walk) verbOne() bool {
	s := w.snap()
	var prods []string
	for id, c := range s.Cards {
		if !isConsumer(c) {
			prods = append(prods, id)
		}
	}
	slices.Sort(prods)
	id := w.pick(prods)
	if id == "" {
		return false
	}
	c := s.Cards[id]
	var e card.Event
	switch w.rng.Intn(8) {
	case 0:
		e = card.SetPriority{Level: "blocker", Reason: "walk"}
	case 1:
		e = card.SetPriority{Level: "normal", Reason: "walk"}
	case 2:
		if w.rng.Intn(4) > 0 {
			return false // a cancel is rare, or every card is gone before the walk is deep
		}
		e = card.Cancel{Reason: "walk"}
	case 3:
		e = card.Rescore{Score: w.rng.Intn(5)}
	case 4:
		e = card.UnholdCard{Reason: "walk"}
	case 5:
		e = card.WaiveRed{Reason: "walk"}
	default:
		if c.State == "waiting" && card.HeldReason(c.Reason) {
			e = card.UnholdCard{Reason: "walk"}
		} else if c.Red && c.State == "review" {
			e = card.WaiveRed{Reason: "walk"}
		} else {
			e = card.Rescore{Score: c.Score + 1}
		}
	}
	pl, r := w.plan(id, e)
	return r == nil && w.apply("verb", pl.Expect, pl.Writes...).Accepted
}

// check is the model's state invariants on the store.
func (w *walk) check(where string) {
	w.t.Helper()
	s := w.snap()
	working := map[string]int{}
	for id, c := range s.Cards {
		if !isConsumer(c) || !isLive(c) {
			continue
		}
		if c.State == "working" {
			working[c.Holder]++
		}
		_, _, tiers, ok := w.roster.Row(c.Holder)
		assert.True(w.t, ok && slices.Contains(tiers, c.Tier), "%s: NeverBelowTier: %s on %s", where, id, c.Holder)
		p := s.Cards[c.Of]
		if c.Kind == "work" {
			assert.Equal(w.t, "working", p.State, "%s: NeverDealtTwice: live work %s of %s in %s", where, id, p.ID, p.State)
			assert.Equal(w.t, p.Attempt, c.Attempt, "%s: NeverDealtTwice: live work %s of an old attempt", where, id)
		} else {
			assert.Equal(w.t, "review", p.State, "%s: a live read %s of %s in %s", where, id, p.ID, p.State)
		}
	}
	for row, n := range working {
		_, width, _, _ := w.roster.Row(row)
		assert.LessOrEqual(w.t, n, width, "%s: WidthKept on %s", where, row)
	}
	w.checkOverlap(s, where)
}

func (w *walk) checkOverlap(s store.Snapshot, where string) {
	w.t.Helper()
	for a, ca := range s.Cards {
		if isConsumer(ca) || !slices.Contains(started, ca.State) {
			continue
		}
		for b, cb := range s.Cards {
			if a < b && !isConsumer(cb) && cb.Stream == ca.Stream && slices.Contains(started, cb.State) {
				assert.False(w.t, shares(ca.Paths, cb.Paths), "%s: NeverOverlapStarted: %s and %s", where, a, b)
			}
		}
	}
}
