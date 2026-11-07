package steps

import (
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// holds is the holds step (AGENTS.md, Steps): Park for a ready producer of a
// held stream that the hold's op missed; Withdraw of every ready consumer,
// releasing its producer to be dealt again in hand;
// and of a held row's unacknowledged consumers at once and a down row's
// after the grace (the gone bound): the row never acknowledged them, as the
// untaken ones these rules were for. A consumer the row acknowledged stays
// (a hold stops new deals, not work in hand; a silent row's are the limits
// step's). A hold never touches landing.
func (t *tick) holds() {
	for _, p := range t.v.Producers("ready") {
		if c := t.v.Cards[p]; t.v.Streams[c.Stream].Held && t.free(p) {
			t.group("park", p, t.producerMove(p, card.Park{}))
		}
	}
	for _, n := range t.rowNames {
		r, why := t.v.Rows[n], ""
		switch {
		case r.Held:
			why = "row " + n + " is held; redealt"
		case r.State == "down" && !t.in.Now.Before(t.downAt(n).Add(seconds(t.tmo.RowSilentGone))):
			why = "row " + n + " is down; redealt"
		}
		for _, id := range t.v.Live(n) {
			c := t.v.Cards[id]
			if !t.unclaimed(id) || !t.free(c.Of) {
				continue
			}
			switch {
			case c.State == "ready":
				t.group("withdraw", id, t.withdraw(id, "dealt to "+n+" before the deal became the take; dealt again, in hand"))
			case why != "" && !t.v.Acked(id):
				t.group("withdraw", id, t.withdraw(id, why))
			}
		}
	}
}

// downAt is when row r went down: the fold's witness, else the view's start.
func (t *tick) downAt(r string) time.Time {
	if d, ok := t.v.Down[r]; ok {
		return d
	}
	return t.v.Since
}
