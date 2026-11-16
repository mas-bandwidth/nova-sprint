package steps

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// AckCursor is the cursor a row's take writes on consumer c of epoch: the
// row's acknowledgement that it holds c.
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

// Nudge is one push run sends to a row this tick, outside the event log: a
// dealt card the row has not acknowledged, and the command that does.
type Nudge struct {
	Row, Card, Line string
	Every           time.Duration
}

// dealtAt is when consumer c was dealt (went working): the fold's witness,
// else the view's start.
func (t *tick) dealtAt(c string) time.Time {
	if at, ok := t.v.TakenAt[c]; ok {
		return at
	}
	return t.v.Since
}

// unacked is the nudges and the ready timeout (LAYER-3 "In hand").
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
				continue
			}
			if t.present(n) {
				cmd := fmt.Sprintf("nova-sprint take %s --as %s --epoch %d", ShellWord(fmt.Sprintf("%s@%d", id, c.Gen)), n, t.v.Epoch)
				out = append(out, Nudge{Row: n, Card: id, Every: seconds(t.tmo.ReadyNag), Line: t.nudgeLine(n, id, untaken, len(live)-untaken, since, cmd)})
			}
		}
	}
	return out
}

func (t *tick) nudgeLine(n, id string, untaken, taken int, since time.Duration, cmd string) string {
	if t.kind(n) == roster.KindWorker {
		rr := t.rowOf(n)
		free := t.benchFree(n)
		return fmt.Sprintf("NUDGE you have a new ready card. you have %d ready cards, you are currently working on %d (cpu %d/%d free, memory %d/%d GiB free). card=%s dealt %s ago is in your hand, not acknowledged; run: %s",
			untaken, taken, free["cpu"], rr.Resources["cpu"], free["memory"], rr.Resources["memory"], id, since.Truncate(time.Second), cmd)
	}
	return fmt.Sprintf("NUDGE you have a new ready card. you have %d ready cards, you are currently working on %d, and your width is %d. Please work to width! card=%s dealt %s ago is in your hand, not acknowledged; run: %s",
		untaken, taken, t.room(n), id, since.Truncate(time.Second), cmd)
}

// redeal withdraws work consumer c, unacknowledged on row n past ready_take,
// and plans its producer's Start to another row with room in the same group.
func (t *tick) redeal(n string, c store.Card, since time.Duration) bool {
	p, base := t.v.Cards[c.Of], t.in.Heads[c.Stream]
	if c.Kind != "work" || !t.unclaimed(c.ID) || !t.free(c.Of) || base == "" || t.v.Streams[c.Stream].Held {
		return false
	}
	workNeeds := t.consumerNeeds("work", p.Brief)
	others := slices.DeleteFunc(t.named(p, t.serving(p)), func(r string) bool { return r == n })
	row := t.pickRow(p, nil, t.withRoomFor(others, workNeeds))
	if row == "" {
		return false
	}
	t.recordDeal(row, workNeeds)
	return t.group("withdraw", c.ID, t.withdraw(c.ID, fmt.Sprintf("in %s's hand %d s, not taken; dealt again", n, int(since/time.Second))),
		t.releasedStart(p, c.ID, card.Start{Row: row, Branch: branch(p), Base: base, Model: t.model(p, row)})) != nil
}

// releasedStart is producer p's Start planned on the records the withdraw
// of its consumer c leaves (p ready, c fail).
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

// undealt opens one undealt judgement when a ready producer stays ready past
// UndealtAfter while an open serving row has room.
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
		rows := t.withRoomFor(t.named(c, t.serving(c)), t.consumerNeeds("work", c.Brief))
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

// routeHeld opens the held judgement of a producer a missing-route fault left waiting.
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
		t.judge("held", p, fmt.Sprintf("producer %s faulted on %s for a missing route or device: %s; it waits, never dealt again by itself: %s", p, row, what, t.routeFix(c)))
	}
}

func (t *tick) routeFix(c store.Card) string {
	route := card.RouteName(c.Reason)
	if route == "" {
		return "unhold to deal it again, or drop it"
	}
	needs := t.consumerNeeds("work", c.Brief)
	if slices.Contains(needs.Tags, route) || needs.Amounts[route] >= 1 {
		return "it declares " + card.ResourcesField + ": " + route + ", so the row that took it advertises " + route + " without having it; fix that row's config, then unhold, or drop it"
	}
	return "replace it (nova-sprint replace " + c.ID + " --by <card>) with a brief that declares " + card.ResourcesField + ": " + route + ", so the deal sends it only to a row with " + route + "; or unhold to deal it again, or drop it"
}
