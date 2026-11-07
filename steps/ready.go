package steps

import (
	"fmt"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// readyTimeout is the ready timeout (Glenn 2026-11-07: "By leaving cards in
// ready we are violating this promise"): a work consumer ready on its row
// longer than timeouts() ready_take is withdrawn and its producer dealt in
// the same group, by the engine's ranking, to another row with room, never
// the slow one. With no such row it stays where it is.
func (t *tick) readyTimeout() {
	for _, n := range t.rowNames {
		for _, id := range t.v.Live(n) {
			c := t.v.Cards[id]
			since, ok := t.v.ReadyAt[id]
			if !ok {
				since = t.v.Since
			}
			ready, p, base := t.in.Now.Sub(since), t.v.Cards[c.Of], t.in.Heads[c.Stream]
			if c.Kind != "work" || c.State != "ready" || ready < seconds(t.tmo.ReadyTake) || !t.unclaimed(id) || !t.free(c.Of) || base == "" || t.v.Streams[c.Stream].Held {
				continue
			}
			row := t.pickRow(p, nil, t.withRoom(slices.DeleteFunc(t.serving(p), func(r string) bool { return r == n })))
			if row == "" {
				continue
			}
			t.tally[row]++
			t.group("withdraw", id, t.withdraw(id, fmt.Sprintf("ready %d s on %s, not taken", int(ready/time.Second), n)),
				t.releasedStart(p, id, card.Start{Row: row, Branch: branch(p), Base: base}))
		}
	}
}

// releasedStart is producer p's Start planned on the records the withdraw
// of its consumer c leaves (p ready, c fail); the group's Merge plans it
// again on the withdraw's writes.
func (t *tick) releasedStart(p store.Card, c string, e card.Start) move {
	return move{label: "Start " + p.ID, plan: func() (card.Plan, *card.Refusal) {
		pc := card.FromStore(p)
		pc.State = card.Ready
		cons := t.consOf(p.ID)
		for i := range cons {
			if cons[i].ID == c {
				cons[i].State = card.Fail
			}
		}
		return card.Move(pc, cons, t.rows(), t.streamOf(p.Stream), t.needsOf(p.ID), e)
	}}
}
