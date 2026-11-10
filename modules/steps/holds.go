package steps

import (
	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// holds is the holds step (LAYER-3 "Holds"): Park for a ready producer of a
// held stream that the hold's op missed; Withdraw of every ready consumer,
// releasing its producer to be dealt again in hand (a ready consumer is one
// dealt before the deal became the take, defect 52: nothing cuts one now);
// and the whole hand of a row that is held, down or not hooked in, taken
// and not taken, returned to the queue with its branch and head kept
// (Glenn, 2026-11-10: "The cards should be redistributed when anybody goes
// to held or down automatically" ... "Aurora is down, but she still has
// cards. This is forbidden."). The hold, the Gone and the unhook return
// the hand in their own op; this is what a hand left on such a row by an
// op of an older release, or by a race, needs: it goes back on the next
// tick. A hold never touches landing (LANDING step 9).
func (t *tick) holds() {
	for _, p := range t.v.Producers("ready") {
		if c := t.v.Cards[p]; t.v.Streams[c.Stream].Held && t.free(p) {
			t.group("park", p, t.producerMove(p, card.Park{}))
		}
	}
	for _, n := range t.rowNames {
		why := closed(t.v.Rows[n])
		for _, id := range t.v.Live(n) {
			c := t.v.Cards[id]
			if !t.unclaimed(id) || !t.free(c.Of) {
				continue
			}
			switch {
			case why != "":
				t.group("withdraw", id, t.withdraw(id, card.ReleasedPrefix+card.Returned(n, why)))
			case c.State == "ready":
				t.group("withdraw", id, t.withdraw(id, "dealt to "+n+" before the deal became the take; dealt again, in hand"))
			}
		}
	}
}

// closed is why row r may hold no card (held, down, unhooked), or "".
func closed(r store.Row) string {
	switch {
	case r.Held:
		return "held"
	case r.State != "up":
		return "down"
	case !Hooked(r):
		return "not hooked in"
	}
	return ""
}

// handMoves is the Withdraw of every live consumer of row n that no earlier
// step of the tick claimed, each released (no blame, no result) with the
// reason that the row is why, its branch and pushed head kept, then e's
// move of row n: the row's hold or Gone with its hand returned, in one op.
// A consumer a step claimed goes back by holds on the next tick.
func (t *tick) handMoves(n, why string, e card.Event) []move {
	var moves []move
	for _, id := range t.v.Live(n) {
		if t.unclaimed(id) && t.free(t.v.Cards[id].Of) {
			moves = append(moves, t.withdraw(id, card.ReleasedPrefix+card.Returned(n, why)))
		}
	}
	return append(moves, t.rowMove(n, e))
}
