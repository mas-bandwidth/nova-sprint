package steps

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Deal is take (defect 52, intent rule 33; LAYER-3 "In hand"): a consumer is
// cut working, in its row's hand at the deal, and the deal's push tells the
// row what it holds. The row's acknowledgement is its `take`, which writes
// AckCursor once; until then the steps nudge the row each tick from
// NudgeAfter, and open an unacked judgement for the coordinator at
// UnackedAfter, naming the row and the card.

// AckCursor is the cursor a row's take writes on consumer c of epoch: the
// row's acknowledgement that it holds c. A cursor, not a card column, so
// the card's gen (the row's handle on it) does not move; it is set once and
// never read for its seq. The epoch is in the name because a new epoch
// reuses consumer ids and the store keeps cursors across epochs. A name
// longer than the store's MaxName is hashed, as a push cursor's is.
func AckCursor(epoch int, c string) string {
	if n := fmt.Sprintf("took:%d:%s", epoch, c); len(n) <= store.MaxName {
		return n
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%d:%s", epoch, c))
	return fmt.Sprintf("took-hash:%x", sum[:16])
}

// Nudge is one push run sends to a row this tick, outside the event log: a
// dealt card the row has not acknowledged, and the command that does.
type Nudge struct{ Row, Card, Line string }

// dealtAt is when consumer c was dealt (went working): the fold's witness,
// else the view's start.
func (t *tick) dealtAt(c string) time.Time {
	if at, ok := t.v.TakenAt[c]; ok {
		return at
	}
	return t.v.Since
}

// unacked is the nudges and the unacked judgements (LAYER-3 "In hand"): a
// working consumer its row has not acknowledged, NudgeAfter after the deal
// a nudge each tick, UnackedAfter after it one judgement for the
// coordinator naming the row and the card.
func (t *tick) unacked() []Nudge {
	var out []Nudge
	for _, n := range t.rowNames {
		for _, id := range t.v.Live(n) {
			c := t.v.Cards[id]
			if c.State != "working" || t.v.Acked(id) {
				continue
			}
			since := t.in.Now.Sub(t.dealtAt(id))
			if since < seconds(Engine.NudgeAfter) {
				continue
			}
			cmd := fmt.Sprintf("nova-sprint take %s@%d --as %s --epoch %d", id, c.Gen, n, t.v.Epoch)
			out = append(out, Nudge{Row: n, Card: id, Line: fmt.Sprintf("NUDGE card=%s dealt %s ago is in your hand, not acknowledged; run: %s", id, since.Truncate(time.Second), cmd)})
			if since >= seconds(Engine.UnackedAfter) {
				t.judge("unacked", id, fmt.Sprintf("row %s has not acknowledged card %s, dealt %s ago and nudged each tick since %ds: is the row's session alive? (nova-sprint release %s@%d --reason <why> deals it again)",
					n, id, since.Truncate(time.Second), Engine.NudgeAfter, id, c.Gen))
			}
		}
	}
	return out
}

// readyAt is when producer p went ready: the fold's witness, else the view's
// start.
func (t *tick) readyAt(p string) time.Time {
	if at, ok := t.v.ReadyAt[p]; ok {
		return at
	}
	return t.v.Since
}

// undealt is the stall of a single card (defect 52's twin, directive 65):
// a ready producer still ready UndealtAfter after it went ready (more than
// one one-second tick) while an open row serving its tier has room opens
// one judgement naming the row and the card, whatever kept the deal from it.
// Run after the deal, so a producer this tick dealt is claimed and skipped.
// Room is counted only on rows the deal can name a model for, and not at all
// while the policy gate deals nothing: the no-model and no-policy judgements
// already say why those cards wait.
func (t *tick) undealt() {
	if t.o.a.NoDeal != "" {
		return
	}
	for _, p := range t.v.Producers("ready") {
		c := t.v.Cards[p]
		if c.Kind != "producer" || t.v.Streams[c.Stream].Held || !t.free(p) {
			continue
		}
		since := t.in.Now.Sub(t.readyAt(p))
		if since < seconds(Engine.UndealtAfter) {
			continue
		}
		rows := t.withRoom(t.named(c, t.serving(c)))
		if len(rows) == 0 {
			continue
		}
		why := "the deal skipped it: an engine defect, report it with this line"
		if t.overBudget() {
			why = fmt.Sprintf("the tick's budget of %d cards was spent before it", t.lim.Budget)
		}
		t.judge("undealt", p, fmt.Sprintf("producer %s has been ready %s with row %s open, serving tier %s with room, and is not dealt: %s",
			p, since.Truncate(time.Second), rows[0], c.Tier, why))
	}
}

// routeBlocked opens the blocked judgement of a producer a missing-route
// fault blocked (card.RouteFault; defect 48): the report is the row's own
// op, so the judgement is the next tick's. It names the row, the card and
// what is missing; the producer waits blocked until the coordinator answers.
func (t *tick) routeBlocked() {
	for _, p := range t.v.Producers("blocked") {
		c := t.v.Cards[p]
		what, ok := card.RouteFault(c.Reason)
		if !ok {
			continue
		}
		row := "its row"
		if w, found := t.v.Cards[workID(c)]; found {
			row = "row " + w.Holder
		}
		t.judge("blocked", p, fmt.Sprintf("producer %s faulted on %s for a missing route or device: %s; it waits blocked, never dealt again by itself: %s", p, row, what, routeFix(c)))
	}
}

// routeFix is what the coordinator does about a route fault, in the one
// vocabulary of routes (card.MissingRoute): a brief that declares the route
// is dealt only to rows serving it.
func routeFix(c store.Card) string {
	route := card.RouteName(c.Reason)
	switch {
	case route == "":
		return "unblock to deal it again, or drop it"
	case slices.Contains(card.NeedsRoutes(c.Brief), route):
		return "it declares " + card.RouteField + ": " + route + ", so the row that took it serves route:" + route + " without having it; fix that row's route, then unblock, or drop it"
	}
	return "replace it (nova-sprint replace " + c.ID + " --by <card>) with a brief that declares " + card.RouteField + ": " + route + ", so the deal sends it only to a row serving route:" + route + "; or unblock to deal it again, or drop it"
}
