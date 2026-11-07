package steps

import (
	"cmp"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// preempt is the preemption step (LAYER-3 "Preemption"; guarantee 4 as round
// 2 reads it; the model's PreStep; BlockerPreemptsInOneTick,
// PreemptYoungest). A ready blocker with no room on any open serving row
// (every row's live consumers at its work plus ahead) gets, on one such row,
// one group: Withdraw of the youngest ready consumer there (it costs
// nothing; the row's queue is where the blocker goes), else of the youngest
// working one (releasing its producer, score and branch kept), merged with
// the blocker's Start, and a stop request for a working victim's runner that
// rides with the op. The row is policy's rows answer when it names one with
// a victim, else the first by name; at most one preemption per row per tick.
// A blocker with room on a row is dealt ahead of everything by the deal, and
// the row's take order is by priority (the take verb's, layer 6). No open
// serving row at all opens unserved for the tier and the blocker stays ready.
func (t *tick) preempt() {
	used := set{}
	for _, b := range sorted(t.v.byRung["blocker"]) {
		c := t.v.Cards[b]
		if c.Kind != "producer" || c.State != "ready" || !t.free(b) || t.v.Streams[c.Stream].Held {
			continue
		}
		serving := t.serving(c)
		if len(serving) == 0 {
			t.judge("unserved", c.Tier, "no open row serves tier "+c.Tier+"; ready "+b+" waits")
			continue
		}
		if !t.noRoom(serving) {
			continue // the deal starts it, ahead of everything
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
	start := card.Start{Row: row, Branch: branch(b), Base: base}
	g := t.group("pre", b.ID, t.withdraw(victim, "preempted by blocker "+b.ID), t.producerMove(b.ID, start))
	if g == nil {
		return
	}
	g.blocker = true
	if vc := t.v.Cards[victim]; vc.State == "working" {
		g.edges = []EdgeRequest{{ID: "stop." + victim, Kind: "stop", Stream: vc.Stream, Cards: []string{victim}, Row: row, Consumer: victim}}
	}
}

// serving is the open (up, not held, hooked), unclaimed, unrested rows
// serving c's tier, by name (the candidates' names).
func (t *tick) serving(c store.Card) []string {
	cands := t.candidates(c, "work")
	out := make([]string, len(cands))
	for i, f := range cands {
		out[i] = f.Name
	}
	return out
}

// noRoom says whether every row is full: its live consumers at its room
// (work plus ahead), with this tick's deals (guarantee 4: "no room").
func (t *tick) noRoom(rows []string) bool {
	for _, r := range rows {
		if t.used(r) < t.room(r) {
			return false
		}
	}
	return true
}

// victim is the consumer a blocker on row preempts: the youngest ready
// consumer there (by producer then slot, the last dealt), else the youngest
// working one (the latest taken, by id when the take was not witnessed);
// never b's own, never a claimed one or one of a claimed producer.
func (t *tick) victim(row, b string) string {
	best, bestReady := "", false
	for _, id := range t.v.Live(row) {
		c := t.v.Cards[id]
		if c.Of == b || !t.unclaimed(id) || !t.free(c.Of) {
			continue
		}
		ready := c.State == "ready"
		switch {
		case best == "" || (ready && !bestReady):
			best, bestReady = id, ready
		case ready == bestReady && ready && t.slotCmp(id, best) > 0:
			best = id
		case ready == bestReady && !ready && t.youngerTake(id, best):
			best = id
		}
	}
	return best
}

// slotCmp orders two consumers by producer then cut order (the model's slot).
func (t *tick) slotCmp(a, b string) int {
	ca, cb := t.v.Cards[a], t.v.Cards[b]
	return cmp.Or(cmp.Compare(ca.Of, cb.Of), cmp.Compare(t.slot(a), t.slot(b)))
}

// slot is a consumer's place in its producer's cut order, from 1.
func (t *tick) slot(id string) int {
	for i, k := range t.v.Consumers(t.v.Cards[id].Of) {
		if k.ID == id {
			return i + 1
		}
	}
	return 0
}

func (t *tick) youngerTake(a, b string) bool {
	ta, tb := t.v.TakenAt[a], t.v.TakenAt[b]
	return ta.After(tb) || (ta.Equal(tb) && a > b)
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
