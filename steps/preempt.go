package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// preempt is the preemption step (LAYER-3 "Preemption"; the model's PreStep;
// BlockerPreemptsInOneTick, PreemptYoungest). A ready blocker with no free
// width on any open serving row (working at width) gets, on one such row,
// one group: Withdraw of the youngest working consumer there (releasing its
// producer, score and branch kept) merged with the blocker's Start, and a
// stop request for the runner that rides with the op. The row is policy's
// rows answer when it names one with a victim, else the first by name; at
// most one preemption per row per tick. No open serving row at all opens
// unserved for the tier and the blocker stays ready.
func (t *tick) preempt() {
	if t.o.a.NoDeal != "" {
		return // the gate: the deal step opens its judgement
	}
	used := set{}
	for _, b := range t.v.Producers("ready") {
		c := t.v.Cards[b]
		if c.Kind != "producer" || rung(c) != "blocker" || !t.free(b) || t.v.Streams[c.Stream].Held {
			continue
		}
		serving := t.named(c, t.serving(c))
		if len(serving) == 0 {
			continue // the deal opens unserved or no-model
		}
		if !t.noFreeWidth(serving) {
			continue // the deal starts it
		}
		var rows []string
		for _, r := range serving {
			if _, u := used[r]; !u && t.victim(r, b) != "" {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		row := t.preferred(c, rows)
		t.preemptOn(c, row, t.victim(row, b))
		used[row] = struct{}{}
	}
}

// preemptOn plans the preemption of victim on row for blocker b.
func (t *tick) preemptOn(b store.Card, row, victim string) {
	base, ok := t.base(b.Stream)
	if !ok {
		return
	}
	start := card.Start{Row: row, Branch: branch(b), Base: base, Model: t.model(b, row)}
	g := t.group("pre", b.ID, t.withdraw(victim, "preempted by blocker "+b.ID), t.producerMove(b.ID, start))
	if g == nil {
		return
	}
	g.blocker = true
	vc := t.v.Cards[victim]
	g.edges = []EdgeRequest{{ID: "stop." + victim, Kind: "stop", Stream: vc.Stream, Cards: []string{victim}, Row: row, Consumer: victim}}
}

// serving is the open (up, not held), unclaimed, unrested rows serving c's
// tier, by name (the candidates' names).
func (t *tick) serving(c store.Card) []string {
	cands := t.candidates(c, "work")
	out := make([]string, len(cands))
	for i, f := range cands {
		out[i] = f.Name
	}
	return out
}

// noFreeWidth says whether every row works at its width (ready consumers
// hold no width; a queue is not free width: the model's NoFreeWidth).
func (t *tick) noFreeWidth(rows []string) bool {
	for _, r := range rows {
		if t.v.Working(r) < t.width(r) {
			return false
		}
	}
	return true
}

// victim is the youngest working consumer on row that is not b's and is
// unclaimed: the latest taken, by id when the take was not witnessed.
func (t *tick) victim(row, b string) string {
	best := ""
	for _, id := range t.v.Live(row) {
		c := t.v.Cards[id]
		if c.State != "working" || c.Of == b || !t.unclaimed(id) || !t.free(c.Of) {
			continue
		}
		if best == "" || t.v.TakenAt[id].After(t.v.TakenAt[best]) || (t.v.TakenAt[id].Equal(t.v.TakenAt[best]) && id > best) {
			best = id
		}
	}
	return best
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
		t.o.refuse("rows")
	}
	return rows[0]
}

// base is the stream's head, the base a Start records; a stream with none
// opens an engine judgement, since nothing can be dealt in it.
func (t *tick) base(stream string) (string, bool) {
	b, ok := t.in.Heads[stream]
	if !ok || b == "" {
		t.engine(stream, "stream "+stream+" has no head in the tick's inputs; layer 6 reads the stream branch and passes Inputs.Heads")
		return "", false
	}
	return b, true
}

// branch is the card branch: sprint/<stream>/<producer> (the stream group
// branch is sprint/<stream>/group, never sprint/<stream>: git refuses a ref
// that is both a file and a directory).
func branch(c store.Card) string { return "sprint/" + c.Stream + "/" + c.ID }
