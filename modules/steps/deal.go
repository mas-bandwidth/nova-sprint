package steps

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
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
// DealStep; NeverDealtTwice, NeverBelowTier, guarantee 8).
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
				if c := t.v.Cards[p]; c.State == "ready" && c.Kind == "producer" && t.admitted(p) {
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
	serving := t.servingKind(c, "work")
	if len(serving) == 0 {
		q := "no open row serves tier " + c.Tier + t.needsClause(c) + "; ready " + c.ID + " waits"
		var resting []string
		for _, f := range t.candidates(c, "work") {
			resting = append(resting, f.Name)
		}
		if len(resting) > 0 {
			q += "; resting after faults, dealt to again when the rest ends: " + strings.Join(resting, ", ")
		}
		t.judge("unserved", t.unservedAbout(c), q)
		return
	}
	named := t.named(c, serving)
	if len(named) == 0 {
		t.noModel(c, serving)
		return
	}
	base, ok := t.base(c.Stream)
	if !ok || (t.overBudget() && rung(c) != "blocker") {
		return
	}
	workNeeds := t.consumerNeeds("work", c.Brief)
	row := t.pickRow(c, t.named(c, answered), t.withRoomFor(t.dealable(c, named), workNeeds))
	if row == "" {
		return
	}
	t.recordDeal(row, workNeeds)
	t.provTally[provider(t.model(c, row))]++
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
	serving := t.servingKind(c, "read")
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
	readNeeds := card.DefaultReadNeeds()
	var rows, models []string
	for _, r := range t.pickRows(c, t.named(c, answered), t.withRoomFor(t.dealable(c, named), readNeeds), t.owed(c)) {
		if m := t.model(c, r); len(t.dealable(c, []string{r})) > 0 {
			t.recordDeal(r, readNeeds)
			t.provTally[provider(m)]++
			rows, models = append(rows, r), append(models, m)
		}
	}
	if len(rows) == 0 {
		return
	}
	t.group("read", c.ID, t.producerMove(c.ID, card.AskReads{Rows: rows, Models: models}))
}

// dealable drops the rows whose model for c rests after a provider's rate
// limit or whose provider is at its concurrency budget.
func (t *tick) dealable(c store.Card, rows []string) []string {
	var out []string
	for _, r := range rows {
		m := t.model(c, r)
		if at, ok := t.v.RateLimited[m]; ok && t.in.Now.Before(at.Add(seconds(t.tmo.RouteRestFor))) {
			continue
		}
		if budget, ok := t.o.a.Concurrency[provider(m)]; ok && t.providerUsed(provider(m)) >= budget {
			continue
		}
		out = append(out, r)
	}
	return out
}

// providerUsed is the consumers live on provider p's models, fleet-wide,
// plus this tick's deals.
func (t *tick) providerUsed(p string) int {
	n := t.provTally[p]
	for _, ids := range t.v.liveOnRow {
		for id := range ids {
			if provider(t.v.Cards[id].Model) == p {
				n++
			}
		}
	}
	return n
}

func provider(model string) string {
	p, _, _ := strings.Cut(model, "/")
	return p
}

// recordDeal updates in-tick consumer and bench resource tallies for row.
func (t *tick) recordDeal(row string, needs card.Needs) {
	t.tally[row]++
	if t.kind(row) == roster.KindWorker {
		t.dealtNeeds[row] = append(t.dealtNeeds[row], needs)
	}
}

// sprintUsedExcept computes sprint_used(B, r) per R50 excluding drop: the
// consumer-ID union C_sprint(B) of working sprint consumers on B and live
// sprint leases in Reserve[B].SprintHolding (taking max(declared, live_held)
// per consumer and resource), plus earlier deals made to B in this tick.
func (t *tick) sprintUsedExcept(row string, drop set) map[string]int {
	res := t.in.Reserve[row]
	heldByID := map[string]map[string]int{}
	active := set{}
	for id := range t.v.liveOnRow[row] {
		if _, skip := drop[id]; !skip && t.v.Cards[id].State == "working" {
			active[id] = struct{}{}
		}
	}
	for raw, m := range res.SprintHolding {
		id := canonicalConsumerID(raw, t.v.Epoch)
		if _, skip := drop[id]; id == "" || skip {
			continue
		}
		active[id] = struct{}{}
		if heldByID[id] == nil {
			heldByID[id] = map[string]int{}
		}
		for k, v := range m {
			heldByID[id][k] += v
		}
	}
	out := map[string]int{}
	for id := range active {
		decl, live := t.consumerNeedsByID(id).Amounts, heldByID[id]
		for k, dv := range decl {
			out[k] += max(dv, live[k])
		}
		for k, lv := range live {
			if _, ok := decl[k]; !ok {
				out[k] += lv
			}
		}
	}
	for _, needs := range t.dealtNeeds[row] {
		for k, v := range needs.Amounts {
			out[k] += v
		}
	}
	return out
}

// benchSprintUsed computes sprint_used(B, r) per R50.
func (t *tick) benchSprintUsed(row string) map[string]int {
	return t.sprintUsedExcept(row, nil)
}

// benchUsed computes used(B, r) = NonSprintUsed[r] + sprint_used(B, r) per R50.
func (t *tick) benchUsed(row string) map[string]int {
	used := t.benchSprintUsed(row)
	for k, v := range t.in.Reserve[row].NonSprintUsed {
		used[k] += v
	}
	return used
}

// benchFree returns available bench resources per R50/R52 (zero for all keys
// when held, offline, or AnyPaused).
func (t *tick) benchFree(row string) map[string]int {
	rr := t.rowOf(row)
	res := t.in.Reserve[row]
	out := make(map[string]int, len(rr.Resources))
	if rr.Held != "" || res.Offline || res.AnyPaused {
		for k := range rr.Resources {
			out[k] = 0
		}
		return out
	}
	used := t.benchUsed(row)
	for k, total := range rr.Resources {
		out[k] = max(0, total-used[k])
	}
	return out
}

// hasRoom reports whether row has room for needs right now (width on kin,
// R50/R52 multi-resource room on benches).
func (t *tick) hasRoom(row string, needs card.Needs) bool {
	switch t.kind(row) {
	case roster.KindKin:
		return t.used(row) < t.room(row)
	case roster.KindWorker:
		rr := t.rowOf(row)
		res := t.in.Reserve[row]
		if rr.Held != "" || res.Offline || res.AnyPaused || !t.rowFitsMax(row, needs) {
			return false
		}
		used := t.benchUsed(row)
		for k, need := range needs.Amounts {
			if rr.Resources[k]-used[k] < need {
				return false
			}
		}
		return true
	}
	return false
}

// withRoomFor filters rows to those that have room for needs right now.
func (t *tick) withRoomFor(rows []string, needs card.Needs) []string {
	var out []string
	for _, r := range rows {
		if t.hasRoom(r, needs) {
			out = append(out, r)
		}
	}
	return out
}

func (t *tick) used(r string) int { return len(t.v.liveOnRow[r]) + t.tally[r] }

// room is policy's work for kin row r, read once per tick.
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

func (t *tick) rowIdle(r string) bool {
	if t.used(r) > 0 {
		return false
	}
	if t.kind(r) == roster.KindWorker {
		for _, v := range t.benchUsed(r) {
			if v > 0 {
				return false
			}
		}
	}
	return true
}

func (t *tick) freeFraction(r string, needs card.Needs) float64 {
	if t.kind(r) == roster.KindKin {
		room := t.room(r)
		if room <= 0 {
			return 0
		}
		return float64(max(0, room-t.used(r))) / float64(room)
	}
	rr := t.rowOf(r)
	free := t.benchFree(r)
	best := 1.0
	has := false
	for _, k := range sortedKeys(needs.Amounts) {
		total := rr.Resources[k]
		if total <= 0 {
			return 0
		}
		frac := float64(free[k]) / float64(total)
		if !has || frac < best {
			best, has = frac, true
		}
	}
	return best
}

// rowsAnswer is policy's rows answer for c, dropping resting rows.
func (t *tick) rowsAnswer(c store.Card, kind string) []string {
	answered := t.o.rows(RowsQuestion{Card: t.facts(c), Kind: kind, Candidates: t.candidates(c, kind)})
	if answered == nil {
		return nil
	}
	out := slices.DeleteFunc(slices.Clone(answered), func(r string) bool { _, resting := t.rested[r]; return resting })
	if len(out) == 0 && len(answered) > 0 {
		return nil
	}
	return out
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
// with room): policy's answered rows first, then the rest.
func (t *tick) pickRows(c store.Card, answered, rows []string, n int) []string {
	if len(rows) == 0 || n <= 0 {
		return nil
	}
	if answered != nil && !slices.ContainsFunc(answered, func(r string) bool { return slices.Contains(rows, r) }) {
		t.o.refuse("rows", fmt.Sprintf("rows(%s) names %s, none with room (rows with room: %s)", c.ID, firstIDs(answered), cmp.Or(firstIDs(rows), "none")))
	}
	kind := kindOf(c)
	allowed := slices.DeleteFunc(slices.Clone(rows), func(r string) bool { return !slices.Contains(answered, r) })
	out := t.ranked(c, kind, answered, allowed, n)[:min(n, len(allowed))]
	if len(out) >= n {
		return out
	}
	rest := slices.DeleteFunc(slices.Clone(rows), func(r string) bool { return slices.Contains(out, r) })
	out = append(out, t.ranked(c, kind, nil, rest, n-len(out))[:min(n-len(out), len(rest))]...)
	return out
}

func kindOf(c store.Card) string {
	if c.State == "review" {
		return "read"
	}
	return "work"
}

func (t *tick) tierBand(r string, want int) int {
	rk := t.rankIndex(r)
	switch {
	case rk == want:
		return 2
	case rk > want:
		return 1
	default:
		return 0
	}
}

func (t *tick) compareRows(c store.Card, want int, needs card.Needs, answered []string, a, b string) int {
	if wa, wb := t.withdrawnFrom(c, a), t.withdrawnFrom(c, b); wa != wb {
		if !wa {
			return -1
		}
		return 1
	}
	if cmpBand := cmp.Compare(t.tierBand(b, want), t.tierBand(a, want)); cmpBand != 0 {
		return cmpBand
	}
	if ia, ib := t.rowIdle(a), t.rowIdle(b); ia != ib {
		if ia {
			return -1
		}
		return 1
	}
	if cmpFrac := cmp.Compare(t.freeFraction(b, needs), t.freeFraction(a, needs)); cmpFrac != 0 {
		return cmpFrac
	}
	if len(answered) > 0 {
		if pa, pb := slices.Index(answered, a), slices.Index(answered, b); pa >= 0 && pb >= 0 && pa != pb {
			return cmp.Compare(pa, pb)
		}
	}
	return cmp.Compare(a, b)
}

// ranked orders rows by !withdrawnFrom, desired tier rank, idle status,
// bottleneck free-resource fraction, policy answer order, then name.
func (t *tick) ranked(c store.Card, kind string, answered, rows []string, n int) []string {
	if len(rows) == 0 {
		return nil
	}
	want := t.desired(c, kind)
	needs := t.consumerNeeds(kind, c.Brief)
	if n == 1 {
		best := rows[0]
		for _, r := range rows[1:] {
			if t.compareRows(c, want, needs, answered, r, best) < 0 {
				best = r
			}
		}
		return []string{best}
	}
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b string) int { return t.compareRows(c, want, needs, answered, a, b) })
	return out
}

// desired is the rank wanted for c (LAYER-3 "Escalation").
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
		want++
	}
	return min(want, top)
}

// topRank is the highest rank any listed row serves.
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

func needsCacheKey(needs card.Needs) string {
	parts := slices.Clone(needs.Tags)
	for _, k := range sortedKeys(needs.Amounts) {
		parts = append(parts, k+"="+itoa(needs.Amounts[k]))
	}
	return strings.Join(parts, "+")
}

func extraNeedsTokens(needs card.Needs, sep string) []string {
	var out []string
	out = append(out, needs.Tags...)
	for _, k := range sortedKeys(needs.Amounts) {
		v := needs.Amounts[k]
		if (k == "cpu" || k == "memory") && v == 1 {
			continue
		}
		if v == 1 && k != "cpu" && k != "memory" {
			out = append(out, k)
		} else {
			out = append(out, k+sep+itoa(v))
		}
	}
	return out
}

func (t *tick) needsClause(c store.Card) string {
	words := extraNeedsTokens(t.consumerNeeds("work", c.Brief), " ")
	if len(words) > 0 {
		return " with resource(s) " + strings.Join(words, ", ")
	}
	return ""
}

func (t *tick) unservedAbout(c store.Card) string {
	parts := append([]string{c.Tier}, extraNeedsTokens(t.consumerNeeds("work", c.Brief), ":")...)
	s := strings.Join(parts, "+")
	if len(s) <= 96 {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%s+hash:%x", c.Tier, sum[:8])
}

func (t *tick) parseUnservedAbout(about string) (string, card.Needs) {
	parts := strings.Split(about, "+")
	needs := card.Needs{Amounts: map[string]int{"cpu": 1, "memory": 1}}
	for _, tok := range parts[1:] {
		if name, numStr, ok := strings.Cut(tok, ":"); ok {
			if name == "hash" {
				needs.Tags = append(needs.Tags, tok)
			} else if n, err := strconv.Atoi(numStr); err == nil && n >= 1 {
				needs.Amounts[name] = n
			}
		} else if t.knownResources[tok] {
			needs.Amounts[tok] = 1
		} else if tok != "" {
			needs.Tags = append(needs.Tags, tok)
		}
	}
	slices.Sort(needs.Tags)
	return parts[0], needs
}
