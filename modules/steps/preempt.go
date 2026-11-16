package steps

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// preempt is the preemption step (LAYER-3 "Preemption"; R53). A ready blocker
// with no free room on any open serving row preempts minimal lowest-priority
// working sprint victims in v2's order (never behind AnyPaused or insufficient
// non-sprint headroom on a bench) and starts in the same op.
func (t *tick) preempt() {
	if t.o.a.NoDeal != "" {
		return
	}
	used := set{}
	for _, b := range t.v.Producers("ready") {
		c := t.v.Cards[b]
		if c.Kind != "producer" || rung(c) != "blocker" || !t.free(b) || t.v.Streams[c.Stream].Held || !t.admitted(b) {
			continue
		}
		serving := t.named(c, t.serving(c))
		if len(serving) == 0 {
			continue
		}
		needs := t.consumerNeeds("work", c.Brief)
		if !t.noFreeRoom(c, serving, needs) {
			continue
		}
		var rows []string
		for _, r := range serving {
			if _, u := used[r]; !u && len(t.victims(r, b, needs)) > 0 {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		row := t.preferred(c, rows)
		t.preemptOn(c, row, t.victims(row, b, needs), needs)
		used[row] = struct{}{}
	}
}

func (t *tick) noFreeRoom(c store.Card, rows []string, needs card.Needs) bool {
	for _, r := range t.dealable(c, rows) {
		if t.kind(r) == roster.KindKin {
			if t.v.Working(r) < t.room(r) {
				return false
			}
		} else if t.hasRoom(r, needs) {
			return false
		}
	}
	return true
}

// preemptOn plans the multi-victim preemption of victims on row for blocker b.
func (t *tick) preemptOn(b store.Card, row string, victims []string, needs card.Needs) {
	base, ok := t.base(b.Stream)
	if !ok || len(victims) == 0 {
		return
	}
	moves := make([]move, 0, len(victims)+1)
	for _, v := range victims {
		moves = append(moves, t.preemptWithdraw(v, "preempted by blocker "+b.ID))
	}
	start := card.Start{Row: row, Branch: branch(b), Base: base, Model: t.model(b, row)}
	moves = append(moves, t.producerMove(b.ID, start))
	g := t.group("pre", b.ID, moves...)
	if g == nil {
		return
	}
	t.recordDeal(row, needs)
	g.blocker = true
	for _, v := range victims {
		vc := t.v.Cards[v]
		g.edges = append(g.edges, EdgeRequest{ID: "stop." + v, Kind: "stop", Stream: vc.Stream, Cards: []string{v}, Row: row, Consumer: v})
	}
}

// serving is the open (up, not held), unrested rows serving c's work tier and
// resources, by name: the candidates without the rows resting after faults.
func (t *tick) serving(c store.Card) []string {
	return t.servingKind(c, "work")
}

// servingKind is the open, unrested, unclaimed candidate rows for kind ("work" or "read").
func (t *tick) servingKind(c store.Card, kind string) []string {
	var out []string
	for _, f := range t.candidates(c, kind) {
		if _, resting := t.rested[f.Name]; !resting && t.unclaimed(f.Name) {
			out = append(out, f.Name)
		}
	}
	return out
}

func (t *tick) consumerPrio(c store.Card) int {
	if c.Kind == "read" {
		return 3
	}
	switch rung(t.v.Cards[c.Of]) {
	case "low":
		return 0
	case "normal":
		return 1
	case "high":
		return 2
	case "critical":
		return 4
	case "blocker":
		return 5
	}
	return 1
}

func (t *tick) eligibleVictims(row, b string) []string {
	var elig []string
	seenProd := set{}
	for _, id := range t.v.Live(row) {
		c := t.v.Cards[id]
		if c.State != "working" || c.Of == b || !t.unclaimed(id) || !t.free(c.Of) || t.consumerPrio(c) >= 5 {
			continue
		}
		if _, dup := seenProd[c.Of]; dup {
			continue
		}
		seenProd[c.Of] = struct{}{}
		elig = append(elig, id)
	}
	slices.SortFunc(elig, func(a, b string) int {
		if cp := cmp.Compare(t.consumerPrio(t.v.Cards[a]), t.consumerPrio(t.v.Cards[b])); cp != 0 {
			return cp
		}
		if !t.v.TakenAt[a].Equal(t.v.TakenAt[b]) {
			return t.v.TakenAt[b].Compare(t.v.TakenAt[a])
		}
		return cmp.Compare(b, a)
	})
	return elig
}

func (t *tick) fitsAfterFreeing(row string, needs card.Needs, freed []string) bool {
	if t.kind(row) == roster.KindKin {
		return len(freed) >= max(1, t.v.Working(row)-t.width(row)+1)
	}
	rr := t.rowOf(row)
	sr := t.v.Rows[row]
	drop := set{}
	for _, id := range freed {
		drop[id] = struct{}{}
	}
	active := set{}
	for id := range t.v.liveOnRow[row] {
		if _, dropped := drop[id]; !dropped && t.v.Cards[id].State == "working" {
			active[id] = struct{}{}
		}
	}
	for _, raw := range sr.Resources.SprintConsumers {
		id := canonicalConsumerID(raw, t.v.Epoch)
		if _, dropped := drop[id]; id != "" && !dropped {
			active[id] = struct{}{}
		}
	}
	used := map[string]int{}
	for k, v := range sr.Resources.HeldOther {
		used[k] += v
	}
	for id := range active {
		for k, v := range t.consumerNeedsByID(id).Amounts {
			used[k] += v
		}
	}
	for _, d := range t.dealtNeeds[row] {
		for k, v := range d.Amounts {
			used[k] += v
		}
	}
	for k, need := range needs.Amounts {
		if rr.Resources[k]-used[k] < need {
			return false
		}
	}
	return true
}

// victims returns the minimal lowest-priority victim set on row whose
// preemption frees room for needs, or nil when preemption cannot fit needs.
func (t *tick) victims(row, b string, needs card.Needs) []string {
	if t.kind(row) == roster.KindWorker {
		rr := t.rowOf(row)
		sr := t.v.Rows[row]
		if rr.Held != "" || sr.Resources.AnyPaused || !t.rowFitsMax(row, needs) {
			return nil
		}
		for k, need := range needs.Amounts {
			if rr.Resources[k]-sr.Resources.HeldOther[k] < need {
				return nil
			}
		}
	}
	elig := t.eligibleVictims(row, b)
	var chosen []string
	fit := false
	for _, id := range elig {
		chosen = append(chosen, id)
		if t.fitsAfterFreeing(row, needs, chosen) {
			fit = true
			break
		}
	}
	if !fit {
		return nil
	}
	for j := len(chosen) - 2; j >= 0; j-- {
		without := slices.Delete(slices.Clone(chosen), j, j+1)
		if t.fitsAfterFreeing(row, needs, without) {
			chosen = without
		}
	}
	return chosen
}

// preferred is the first of rows policy's answer for c names, else the first
// by name; an answer naming none of them is a miss.
func (t *tick) preferred(c store.Card, rows []string) string {
	answered := t.rowsAnswer(c, "work")
	for _, r := range answered {
		if slices.Contains(rows, r) {
			return r
		}
	}
	if answered != nil {
		t.o.refuse("rows", fmt.Sprintf("rows(%s) names %s, none of the rows that could take it (%s)", c.ID, firstIDs(answered), firstIDs(rows)))
	}
	return rows[0]
}

// base is the stream's head, the base a Start records; a stream with none
// opens an engine judgement, since nothing can be dealt in it.
func (t *tick) base(stream string) (string, bool) {
	b, ok := t.in.Heads[stream]
	if !ok || b == "" {
		t.engine(stream, noHead(stream)+"; layer 6 reads the stream branch and passes Inputs.Heads")
		return "", false
	}
	return b, true
}

func noHead(stream string) string { return "stream " + stream + " has no head in the tick's inputs" }

func branch(c store.Card) string { return "sprint/" + c.Stream + "/" + c.ID }
