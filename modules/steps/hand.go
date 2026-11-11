package steps

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Deal is take (defect 52, intent rule 33; LAYER-3 "In hand"): a consumer is
// cut working, in its row's hand at the deal, and the deal's push tells the
// row what it holds. The row's acknowledgement is its `take`, which writes
// AckCursor once; until then the steps nudge the row each tick from
// timeouts ready_nag, and at ready_take withdraw the card and deal it to
// another row with room, or, when no other row can take it, withdraw it
// back to the queue, its branch
// kept, to be dealt again (item 2, 2026-11-08: never left in a row's hand).

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

// ShellWord is s as one word of a printed command a row runs in a shell:
// as it is when it holds only safe characters, else single-quoted.
func ShellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:@/-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// PongOpID is the id of row's pong to the challenge that carried nonce in
// epoch: the pong verb writes an op by this id, and run counts it only for
// a nonce it issued. A repeat applies once.
func PongOpID(epoch int, row, nonce string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d:%s:%s", epoch, row, nonce))
	return fmt.Sprintf("pong.%x", sum[:16])
}

// Nudge is one push run sends to a row this tick, outside the event log: a
// dealt card the row has not acknowledged, and the command that does, or
// run's challenge (Subject CHALLENGE). Subject "" is NUDGE.
type Nudge struct {
	Row, Card, Line, Subject string
	Every                    time.Duration
}

// dealtAt is when consumer c was dealt (went working): the fold's witness,
// else the view's start.
func (t *tick) dealtAt(c string) time.Time {
	if at, ok := t.v.TakenAt[c]; ok {
		return at
	}
	return t.v.Since
}

// unacked is the nudges and the ready timeout (LAYER-3 "In hand"; the
// human, 2026-11-07: "nag every second until taken", "ready_take withdraws
// and re-deals"): a working consumer its row has not acknowledged is nudged
// each tick from timeouts ready_nag after the deal while the row is on the
// bus (present), with the row's count of
// such cards, the cards it has taken and its width; at ready_take a work
// card is withdrawn and its producer dealt in the same group, by the
// engine's ranking, to another row with room, never the slow one, and a
// card no other row can take (a read, or no room elsewhere) is withdrawn
// back to the queue, its branch kept and the reason recorded: the deal
// gives it to another row first (ranked) when one has room.
func (t *tick) unacked() []Nudge {
	var out []Nudge
	for _, n := range t.rowNames {
		live := t.v.Live(n)
		untaken := 0
		for _, id := range live {
			if t.v.Cards[id].State == "working" && !t.v.Acked(id) {
				untaken++
			}
		}
		for _, id := range live {
			c := t.v.Cards[id]
			if c.State != "working" || t.v.Acked(id) {
				continue
			}
			since := t.in.Now.Sub(t.dealtAt(id))
			if since < seconds(t.tmo.ReadyNag) {
				continue
			}
			if since >= seconds(t.tmo.ReadyTake) && (!t.unclaimed(id) || !t.free(c.Of) || t.redeal(n, c, since) ||
				t.group("withdraw", id, t.withdraw(id, fmt.Sprintf("in %s's hand %d s, not taken; back to the queue, branch %s kept", n, int(since/time.Second), branch(t.v.Cards[c.Of])))) != nil) {
				continue // another step moves it this tick, or it is dealt again or back in the queue
			}
			if t.present(n) { // the bus only queues a nudge to an absent row
				cmd := fmt.Sprintf("nova-sprint take %s --as %s --epoch %d", ShellWord(fmt.Sprintf("%s@%d", id, c.Gen)), n, t.v.Epoch)
				out = append(out, Nudge{Row: n, Card: id, Every: seconds(t.tmo.ReadyNag), Line: fmt.Sprintf("NUDGE you have a new ready card. you have %d ready cards, you are currently working on %d, and your width is %d. Please work to width! card=%s dealt %s ago is in your hand, not acknowledged; run: %s",
					untaken, len(live)-untaken, t.room(n), id, since.Truncate(time.Second), cmd)})
			}
		}
	}
	return out
}

// redeal withdraws work consumer c, unacknowledged on row n past
// ready_take, and plans its producer's Start to another row with room in
// the same group; false when no other serving row has room, the producer
// is claimed this tick, the base is unknown, the stream is held or the
// group plans nothing (the budget is spent, or the card machine refused
// it), and the card stays where it is.
func (t *tick) redeal(n string, c store.Card, since time.Duration) bool {
	p, base := t.v.Cards[c.Of], t.in.Heads[c.Stream]
	if c.Kind != "work" || !t.unclaimed(c.ID) || !t.free(c.Of) || base == "" || t.v.Streams[c.Stream].Held {
		return false
	}
	row := t.pickRow(p, nil, t.withRoom(slices.DeleteFunc(t.named(p, t.serving(p)), func(r string) bool { return r == n })))
	if row == "" {
		return false
	}
	t.tally[row]++
	return t.group("withdraw", c.ID, t.withdraw(c.ID, fmt.Sprintf("in %s's hand %d s, not taken; dealt again", n, int(since/time.Second))),
		t.releasedStart(p, c.ID, card.Start{Row: row, Branch: branch(p), Base: base, Model: t.model(p, row)})) != nil
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
		if c.Kind != "producer" || t.v.Streams[c.Stream].Held || !t.free(p) || !t.admitted(p) {
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

// routeHeld opens the held judgement of a producer a missing-route fault
// left waiting (card.RouteFault; defect 48): the report is the row's own
// op, so the judgement is the next tick's. It names the row, the card and
// what is missing; the producer waits, never dealt, until the coordinator
// answers.
func (t *tick) routeHeld() {
	for _, p := range t.v.Producers("waiting") {
		c := t.v.Cards[p]
		what, ok := card.RouteFault(c.Reason)
		if !ok {
			continue
		}
		row := "its row"
		if w, found := t.v.Cards[workID(c)]; found {
			row = "row " + w.Holder
		}
		t.judge("held", p, fmt.Sprintf("producer %s faulted on %s for a missing route or device: %s; it waits, never dealt again by itself: %s", p, row, what, routeFix(c)))
	}
}

// routeFix is what the coordinator does about a route fault, in the one
// vocabulary of routes (card.MissingRoute): a brief that declares the route
// is dealt only to rows serving it.
func routeFix(c store.Card) string {
	route := card.RouteName(c.Reason)
	switch {
	case route == "":
		return "unhold to deal it again, or drop it"
	case slices.Contains(card.NeedsRoutes(c.Brief), route):
		return "it declares " + card.RouteField + ": " + route + ", so the row that took it serves route:" + route + " without having it; fix that row's route, then unhold, or drop it"
	}
	return "replace it (nova-sprint replace " + c.ID + " --by <card>) with a brief that declares " + card.RouteField + ": " + route + ", so the deal sends it only to a row serving route:" + route + "; or unhold to deal it again, or drop it"
}
