package steps

import (
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// holds is the holds step (LAYER-3 "Holds"): Park for a ready producer of a
// held stream that the hold's op missed; Withdraw of a held row's untaken
// consumers at once and of a down row's after the grace (the gone bound),
// releasing their producers to be dealt elsewhere; working consumers stay.
// A hold never touches landing (LANDING step 9).
func (t *tick) holds() {
	for _, p := range t.v.Producers("ready") {
		if c := t.v.Cards[p]; t.v.Streams[c.Stream].Held && t.free(p) {
			t.group("park", p, t.producerMove(p, card.Park{}))
		}
	}
	for _, n := range sortedKeys(t.v.Rows) {
		r := t.v.Rows[n]
		why := ""
		switch {
		case r.Held:
			why = "row " + n + " is held; redealt"
		case r.State == "down" && !t.in.Now.Before(t.downAt(n).Add(seconds(t.tmo.RowSilentGone))):
			why = "row " + n + " is down; redealt"
		default:
			continue
		}
		for _, id := range t.v.Live(n) {
			if c := t.v.Cards[id]; c.State == "ready" && t.unclaimed(id) && t.free(c.Of) {
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
