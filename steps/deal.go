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
// row has room for stays ready, queued. A blocker's start is one per row per
// tick (tick step 3). Escalation is row choice: a work consumer at the
// card's own rank, one rank up per bounce from escalate_after, the top rank
// at frontier_after; a read at the rank above the writer's, on a row of
// another model than the author's when one is open (LESSONS 3.8). No tier is
// ever written, and a row below the card's tier is never offered.
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
	answered := t.rowsAnswer(c, "work")
	serving := t.serving(c)
	if len(serving) == 0 {
		t.judge("unserved", c.Tier, "no open row serves tier "+c.Tier+"; ready "+c.ID+" waits: hook a row that serves it (nova-sprint hook --as <row> --role worker)")
		return
	}
	blocker := rung(c) == "blocker"
	base, ok := t.base(c.Stream)
	if !ok || (t.overBudget() && !blocker) {
		return // past the budget nothing more is dealt; the judgements above are still opened
	}
	rows := t.withRoom(serving)
	if blocker {
		rows = slices.DeleteFunc(rows, func(r string) bool { _, used := t.blockers[r]; return used })
	}
	row := t.pickRow(c, answered, rows)
	if row == "" {
		return
	}
	t.tally[row]++ // the in-tick tally counts the deal whether or not the budget admits it
	if g := t.group("start", c.ID, t.producerMove(c.ID, card.Start{Row: row, Branch: branch(c), Base: base})); g != nil {
		g.blocker = blocker
	}
	if blocker {
		t.blockers[row] = struct{}{}
	}
}

// dealRead asks the reads c is owed, one row each. Fewer rows than reads is a
// cannot-read judgement (round 2 ruling 9): what can be read is asked, and
// the review never merges short of the count.
func (t *tick) dealRead(c store.Card) {
	if t.v.Streams[c.Stream].Held {
		return
	}
	answered := t.rowsAnswer(c, "read")
	serving := t.readers(c)
	owed := t.owed(c)
	if len(serving) < owed {
		t.judge("cannot-read", c.ID, "fewer open rows may read "+c.ID+" than the "+itoa(t.readsWanted(c))+" reads policy asks (author "+c.Author+"); hook another row or lower reads(card)")
	}
	if len(serving) == 0 || t.overBudget() {
		return
	}
	rows := t.pickRows(c, answered, t.withRoom(serving), owed)
	if len(rows) == 0 {
		return
	}
	for _, r := range rows {
		t.tally[r]++
	}
	t.group("read", c.ID, t.producerMove(c.ID, card.AskReads{Rows: rows}))
}

// readers is the open serving rows that may read c: not its author, not
// holding a live read of its head.
func (t *tick) readers(c store.Card) []string {
	var out []string
	for _, f := range t.candidates(c, "read") {
		out = append(out, f.Name)
	}
	return out
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

// room is policy's work plus ahead for row r, read once per tick.
func (t *tick) room(r string) int {
	if n, ok := t.roomCache[r]; ok {
		return n
	}
	w := t.o.width(t.rowFacts(t.v.Rows[r]))
	t.roomCache[r] = w.Work + w.Ahead
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

// pickRow is one row for c among rows: policy's first answered row with
// room, else the engine's best.
func (t *tick) pickRow(c store.Card, answered, rows []string) string {
	picks := t.pickRows(c, answered, rows, 1)
	if len(picks) == 0 {
		return ""
	}
	return picks[0]
}

// pickRows is up to n distinct rows for c among rows (every one eligible,
// with room): policy's answered rows in its order, the engine's ranking for
// the rest. An answer naming no row with room is a miss, as an answer
// naming a row not offered is.
func (t *tick) pickRows(c store.Card, answered, rows []string, n int) []string {
	if answered != nil && !slices.ContainsFunc(answered, func(r string) bool { return slices.Contains(rows, r) }) {
		t.o.refuse("rows")
	}
	if len(rows) == 0 || n <= 0 {
		return nil
	}
	var out []string
	for _, r := range answered {
		if slices.Contains(rows, r) && !slices.Contains(out, r) && len(out) < n {
			out = append(out, r)
		}
	}
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

// ranked is rows in the engine's order (the model's DefaultRow), an
// ordering with no weights: the desired rank exactly, then above it; a row
// of another model than the author's for a read; an idle lane; most room; a
// row the card was withdrawn from last (LPM D22); then name. rows come in
// name order, so a stable sort keeps the name tie-break; a single
// candidate's best row is one pass (the deal's common case).
func (t *tick) ranked(c store.Card, kind string, rows []string, n int) []string {
	want := t.desired(c, kind)
	author := t.rowOf(c.Author).Model
	better := func(a, b string) int {
		rk := func(r string) int {
			switch i := t.rankIndex(r); {
			case i == want:
				return 2
			case i > want:
				return 1
			}
			return 0
		}
		other := func(r string) int { return b2i(kind == "read" && author != "" && t.rowOf(r).Model != author) }
		idle := func(r string) int { return b2i(t.used(r) == 0) }
		kept := func(r string) int { return b2i(!t.withdrawnFrom(c, r)) }
		return cmp.Or(cmp.Compare(rk(b), rk(a)), cmp.Compare(other(b), other(a)), cmp.Compare(idle(b), idle(a)),
			cmp.Compare(t.room(b)-t.used(b), t.room(a)-t.used(a)), cmp.Compare(kept(b), kept(a)))
	}
	if n == 1 {
		best := ""
		for _, r := range rows {
			if best == "" || better(r, best) < 0 {
				best = r
			}
		}
		return []string{best}
	}
	out := slices.Clone(rows)
	slices.SortStableFunc(out, better)
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// desired is the rank wanted for c (LAYER-3 "Escalation"): a read at the
// rank above the writer's row (the top stays the top); work at the card's
// tier, one rank up per bounce from escalate_after, the top at
// frontier_after.
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
	case c.Bounces >= t.lim.FrontierAfter:
		want = top
	case c.Bounces >= t.lim.EscalateAfter:
		want += c.Bounces - t.lim.EscalateAfter + 1
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
