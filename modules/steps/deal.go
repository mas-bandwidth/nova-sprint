package steps

import (
	"cmp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The ladder, highest first (Glenn, 2026-11-06: "low, normal, high, read,
// critical, blocker in specific order"); read is the rung every read
// consumer occupies.
var ladder = []string{"blocker", "critical", "read", "high", "normal", "low"}

// cand is one deal candidate: a ready producer (work) or a review owed reads
// (read), with policy's order key.
type cand struct {
	c    store.Card
	kind string
	key  []any
}

// deal is the deal and reads step (LAYER-3 "Deal and reads"; the model's
// DealStep; NeverDealtTwice, NeverBelowTier, guarantee 8). One candidate
// list down the ladder, within a rung in policy's order then id; each
// candidate is dealt to a row with room (working under policy's work, at
// most the width: the deal is the take, defect 52), the
// row from policy's rows answer, else the engine's ranking; a candidate no
// row has room for stays ready, queued. Escalation is row choice: a work
// consumer at the card's own rank, one up at escalate_after bounces, the
// top rank at frontier_after; a read at the rank above the writer's. No
// tier is ever written, and a row below the card's tier is never offered.
func (t *tick) deal() {
	if t.gated() {
		return
	}
	for _, x := range t.cands() {
		if !t.free(x.c.ID) {
			continue
		}
		if x.kind == "work" {
			t.dealWork(x.c)
		} else {
			t.dealRead(x.c)
		}
	}
}

// cands is the candidate list in ladder order.
func (t *tick) cands() []cand {
	var out []cand
	for _, level := range ladder {
		var rung []cand
		if level == "read" {
			for _, p := range t.v.Producers("review") {
				if c := t.v.Cards[p]; t.owed(c) > 0 {
					rung = append(rung, cand{c: c, kind: "read", key: t.o.order(t.facts(c))})
				}
			}
		} else {
			for p := range t.v.byRung[level] {
				if c := t.v.Cards[p]; c.State == "ready" && c.Kind == "producer" {
					rung = append(rung, cand{c: c, kind: "work", key: t.o.order(t.facts(c))})
				}
			}
		}
		slices.SortFunc(rung, func(a, b cand) int { return cmp.Or(cmpKeys(a.key, b.key), cmp.Compare(a.c.ID, b.c.ID)) })
		out = append(out, rung...)
	}
	return out
}

// cmpKeys orders two order tuples position by position: ints by value,
// strings by text, an int before a string, a shorter tuple first.
func cmpKeys(a, b []any) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := cmpKey(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func cmpKey(a, b any) int {
	ai, aok := a.(int)
	bi, bok := b.(int)
	switch {
	case aok && bok:
		return cmp.Compare(ai, bi)
	case aok:
		return -1
	case bok:
		return 1
	}
	as, _ := a.(string)
	bs, _ := b.(string)
	return cmp.Compare(as, bs)
}

func (t *tick) dealWork(c store.Card) {
	if t.v.Streams[c.Stream].Held {
		return
	}
	answered := t.rowsAnswer(c, "work")
	d := t.dealable(c)
	if len(d.serving) == 0 {
		t.judge("unserved", unservedAbout(c), "no open row serves tier "+c.Tier+routesClause(c)+"; ready "+c.ID+" waits")
		return
	}
	if len(d.named) == 0 {
		t.noModel(c, d.serving)
		return
	}
	base, ok := t.base(c.Stream)
	if !ok || (t.overBudget() && rung(c) != "blocker") {
		return // past the budget nothing more is dealt; the judgements above are still opened
	}
	row := t.pickRow(c, t.named(c, answered), t.roomy(d))
	if row == "" {
		return
	}
	t.tally[row]++ // the in-tick tally counts the deal whether or not the budget admits it
	start := card.Start{Row: row, Branch: branch(c), Base: base, Model: t.model(c, row)}
	if g := t.group("start", c.ID, t.producerMove(c.ID, start)); g != nil {
		g.blocker = rung(c) == "blocker"
	}
}

func (t *tick) dealRead(c store.Card) {
	if t.v.Streams[c.Stream].Held {
		return
	}
	answered := t.rowsAnswer(c, "read")
	var serving []string
	for _, r := range t.serving(c) {
		if r != c.Author && !t.readingOn(c, r) {
			serving = append(serving, r)
		}
	}
	if len(serving) == 0 {
		t.judge("cannot-read", c.ID, "no open row may read "+c.ID+" (author "+c.Author+"); the review waits on nothing")
		return
	}
	named := t.named(c, serving)
	if len(named) == 0 {
		t.noModel(c, serving)
		return
	}
	if t.overBudget() {
		return
	}
	rows := t.pickRows(c, t.named(c, answered), t.withRoom(named), t.owed(c))
	if len(rows) == 0 {
		return
	}
	models := make([]string, len(rows))
	for i, r := range rows {
		t.tally[r]++
		models[i] = t.model(c, r)
	}
	t.group("read", c.ID, t.producerMove(c.ID, card.AskReads{Rows: rows, Models: models}))
}

// dealable is the rows a work card of one tier and routes may be dealt to
// this tick: those serving it, of them those it can be named a model on,
// and of those the ones with room when last asked. Serving is the tick's
// candidates for the tier and routes (built once), so the first two are
// built once per tier and routes, never per card.
type dealable struct{ serving, named, room []string }

func (t *tick) dealable(c store.Card) *dealable {
	key := candKey(c)
	d, ok := t.dealCache[key]
	if !ok {
		d = &dealable{serving: t.serving(c)}
		d.named = t.named(c, d.serving)
		d.room = d.named
		t.dealCache[key] = d
	}
	return d
}

// roomy is withRoom(d.named) now: a row's room only shrinks within a tick
// (a deal adds to its tally, nothing takes from it), so the rows that had
// room are filtered again, never all of them. With the fleet full a card
// costs nothing here, where it cost the fleet.
func (t *tick) roomy(d *dealable) []string {
	d.room = t.withRoom(d.room)
	return d.room
}

// withRoom keeps the rows with room: live consumers plus this tick's deals
// under policy's work, at most the width. Every live consumer is in hand
// (working; a ready one is a pre-deal-is-take leftover the holds step
// withdraws), so this is the width check at the deal.
func (t *tick) withRoom(rows []string) []string {
	var out []string
	for _, r := range rows {
		if t.used(r) < t.room(r) {
			out = append(out, r)
		}
	}
	return out
}

func (t *tick) used(r string) int { return len(t.v.liveOnRow[r]) + t.tally[r] }

// room is policy's work for row r, read once per tick.
func (t *tick) room(r string) int {
	if n, ok := t.roomCache[r]; ok {
		return n
	}
	w := t.o.width(t.rowFacts(t.v.Rows[r]))
	t.roomCache[r] = w.Work
	return t.roomCache[r]
}

// rankIndex is row r's rank as an index into store.Tiers, once per tick.
func (t *tick) rankIndex(r string) int {
	if n, ok := t.rankCache[r]; ok {
		return n
	}
	t.rankCache[r] = tierIndex(t.rank(r))
	return t.rankCache[r]
}

// rowsAnswer is policy's rows answer for c, read once per candidate (the
// oracle records a miss per question whatever the candidate's fate); nil
// when the engine's ranking decides.
func (t *tick) rowsAnswer(c store.Card, kind string) []string {
	return t.o.rows(RowsQuestion{Card: t.facts(c), Kind: kind, Candidates: t.candidates(c, kind)})
}

// pickRow is one row for c among rows: the engine's best of the rows policy
// answered with room, else of them all.
func (t *tick) pickRow(c store.Card, answered, rows []string) string {
	picks := t.pickRows(c, answered, rows, 1)
	if len(picks) == 0 {
		return ""
	}
	return picks[0]
}

// pickRows is up to n distinct rows for c among rows (every one eligible,
// with room): policy's answered rows are the set a card may go to, its order
// no preference; the engine's ranking (which counts this tick's deals) picks
// among them, then among the rest. An answer naming no row with room is a
// miss, as an answer naming a row not offered is.
func (t *tick) pickRows(c store.Card, answered, rows []string, n int) []string {
	if len(rows) == 0 { // no answered row has room: a miss when there is an answer
		if answered != nil {
			t.o.refuse("rows")
		}
		return nil
	}
	named := make(set, len(answered)) // the answer as a set: the checks below are linear, never answer times rows
	for _, r := range answered {
		named[r] = struct{}{}
	}
	isNamed := func(r string) bool { _, ok := named[r]; return ok }
	if answered != nil && !slices.ContainsFunc(rows, isNamed) {
		t.o.refuse("rows")
	}
	if n <= 0 {
		return nil
	}
	allowed := slices.DeleteFunc(slices.Clone(rows), func(r string) bool { return !isNamed(r) })
	out := t.ranked(c, kindOf(c), allowed, n)[:min(n, len(allowed))]
	if len(out) >= n {
		return out
	}
	rest := slices.DeleteFunc(slices.Clone(rows), func(r string) bool { return slices.Contains(out, r) })
	out = append(out, t.ranked(c, kindOf(c), rest, n-len(out))[:min(n-len(out), len(rest))]...)
	return out
}

func kindOf(c store.Card) string {
	if c.State == "review" {
		return "read"
	}
	return "work"
}

// ranked is rows in the engine's order (the model's DefaultRow): the desired
// rank exactly, then above it, then an idle lane, then most room, then name;
// a row the card was withdrawn from last, when another remains (LPM D22).
// rows come in name order, so a stable sort keeps the name tie-break; a
// single candidate's best row is one pass (the deal's common case).
func (t *tick) ranked(c store.Card, kind string, rows []string, n int) []string {
	want := t.desired(c, kind)
	score := func(r string) int {
		rk := t.rankIndex(r)
		s := 0
		switch {
		case rk == want:
			s = 100
		case rk > want:
			s = 50
		}
		if t.used(r) == 0 {
			s += 10
		}
		if t.withdrawnFrom(c, r) {
			s -= 1000
		}
		return s + t.room(r) - t.used(r)
	}
	if n == 1 {
		best, bestScore := "", 0
		for _, r := range rows {
			if s := score(r); best == "" || s > bestScore {
				best, bestScore = r, s
			}
		}
		return []string{best}
	}
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(score(b), score(a)) })
	return out
}

// desired is the rank wanted for c (LAYER-3 "Escalation"): a read at the
// rank above the writer's row (the top stays the top); work at the card's
// tier, one up at escalate_after bounces, the top at frontier_after.
func (t *tick) desired(c store.Card, kind string) int {
	top := t.topRank()
	if kind == "read" {
		if rk := t.rank(c.Author); rk != "" {
			return min(tierIndex(rk)+1, top)
		}
		return tierIndex(c.Tier)
	}
	want := tierIndex(c.Tier)
	switch {
	case c.Bounces >= Engine.FrontierAfter:
		want = top
	case c.Bounces >= Engine.EscalateAfter:
		want++
	}
	return min(want, top)
}

// topRank is the highest rank any listed row serves: the top escalation
// reaches (the top stays the top); the top tier when no row is listed.
func (t *tick) topRank() int {
	top := -1
	for n := range t.v.Rows {
		if t.listed(n) {
			top = max(top, tierIndex(t.rank(n)))
		}
	}
	if top < 0 {
		return len(store.Tiers) - 1
	}
	return top
}

// withdrawnFrom says whether a consumer of c was withdrawn from row.
func (t *tick) withdrawnFrom(c store.Card, row string) bool {
	for id := range t.v.consOf[c.ID] {
		if x := t.v.Cards[id]; x.State == "fail" && x.Holder == row && x.Kind == "work" {
			return true
		}
	}
	return false
}

// routesClause is " with route(s) <a, b>" for a card that declares routes
// (card.NeedsRoutes), else "".
func routesClause(c store.Card) string {
	if rs := card.NeedsRoutes(c.Brief); len(rs) > 0 {
		return " with route(s) " + strings.Join(rs, ", ")
	}
	return ""
}

// unservedAbout is what an unserved judgement is about: the card's tier,
// then "+<route>" for each route it declares (cleared reads it back), so a
// tier served without the route does not clear it.
func unservedAbout(c store.Card) string {
	return strings.Join(append([]string{c.Tier}, card.NeedsRoutes(c.Brief)...), "+")
}
