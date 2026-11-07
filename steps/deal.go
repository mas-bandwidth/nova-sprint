package steps

import (
	"cmp"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
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
// candidate is dealt to a row with room (live under work plus ahead), the
// row from policy's rows answer, else the engine's ranking; a candidate no
// row has room for stays ready, queued. Escalation is row choice: a work
// consumer at the card's own rank, one up at escalate_after bounces, the
// top rank at frontier_after; a read at the rank above the writer's. No
// tier is ever written, and a row below the card's tier is never offered.
func (t *tick) deal() {
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
	serving := t.serving(c)
	if len(serving) == 0 {
		t.judge("unserved", c.Tier, "no open row serves tier "+c.Tier+"; ready "+c.ID+" waits")
		return
	}
	base, ok := t.base(c.Stream)
	if !ok {
		return
	}
	row := t.pickRow(c, "work", t.withRoom(serving))
	if row == "" {
		return
	}
	g := t.group("start", c.ID, t.producerMove(c.ID, card.Start{Row: row, Branch: branch(c), Base: base}))
	if g != nil {
		g.blocker = rung(c) == "blocker"
		t.tally[row]++
	}
}

func (t *tick) dealRead(c store.Card) {
	if t.v.Streams[c.Stream].Held {
		return
	}
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
	rows := t.pickRows(c, t.withRoom(serving), t.owed(c))
	if len(rows) == 0 {
		return
	}
	if t.group("read", c.ID, t.producerMove(c.ID, card.AskReads{Rows: rows})) != nil {
		for _, r := range rows {
			t.tally[r]++
		}
	}
}

// withRoom keeps the rows with room: live consumers plus this tick's deals
// under work plus ahead (policy's width answer; width caps working only).
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

func (t *tick) room(r string) int {
	w := t.o.width(t.rowFacts(t.v.Rows[r]))
	return w.Work + w.Ahead
}

// pickRow is one row for c among rows: policy's first answered row with
// room, else the engine's best.
func (t *tick) pickRow(c store.Card, kind string, rows []string) string {
	picks := t.pickRows(c, rows, 1)
	if len(picks) == 0 {
		return ""
	}
	return picks[0]
}

// pickRows is up to n distinct rows for c among rows (every one eligible,
// with room): policy's answered rows in its order, the engine's ranking for
// the rest. An answer naming no row with room is a miss, as an answer
// naming a row not offered is.
func (t *tick) pickRows(c store.Card, rows []string, n int) []string {
	if len(rows) == 0 || n <= 0 {
		return nil
	}
	var out []string
	answered := t.o.rows(RowsQuestion{Card: t.facts(c), Kind: kindOf(c), Candidates: t.candidates(c, kindOf(c))})
	for _, r := range answered {
		if slices.Contains(rows, r) && !slices.Contains(out, r) && len(out) < n {
			out = append(out, r)
		}
	}
	if answered != nil && len(out) == 0 {
		t.o.refuse("rows")
	}
	for _, r := range t.ranked(c, kindOf(c), rows) {
		if len(out) >= n {
			break
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
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
func (t *tick) ranked(c store.Card, kind string, rows []string) []string {
	want := t.desired(c, kind)
	score := func(r string) int {
		rk := tierIndex(t.rank(r))
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
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(score(b), score(a)), cmp.Compare(a, b)) })
	return out
}

// desired is the rank wanted for c (LAYER-3 "Escalation"): a read at the
// rank above the writer's row (the top stays the top); work at the card's
// tier, one up at escalate_after bounces, the top at frontier_after.
func (t *tick) desired(c store.Card, kind string) int {
	top := len(store.Tiers) - 1
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

// withdrawnFrom says whether a consumer of c was withdrawn from row.
func (t *tick) withdrawnFrom(c store.Card, row string) bool {
	for id := range t.v.consOf[c.ID] {
		if x := t.v.Cards[id]; x.State == "fail" && x.Holder == row && x.Kind == "work" {
			return true
		}
	}
	return false
}
