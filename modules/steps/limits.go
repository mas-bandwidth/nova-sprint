package steps

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// limits is the retire and fail limits step (LAYER-3 "Retire and fail
// limits"): HoldCard with a held judgement when bounces since the last
// unhold reach the limit (a fault never counts, LESSONS 2.24; an unhold is a
// fresh budget), or when reads of its review and head have faulted policy's
// faults limit running (readFaults; unheld it is dealt again as work, so its
// next review starts the count afresh); Withdraw of a working consumer whose
// row is silent past the bound, or that is past its tier's deadline with no
// report, branch kept; Gone for a row unseen past its
// bound, with its hand returned in the same op (holds), never for one missed challenge (LESSONS 4.7: the bound is policy's
// and long); a flap judgement at the flap limit.
func (t *tick) limits() {
	for _, state := range []string{"ready", "review"} {
		for _, p := range t.v.Producers(state) {
			c := t.v.Cards[p]
			spent := c.Bounces - c.BounceBase // since its last unhold
			faults, last := t.readFaults(c)
			// The no-progress run, since the last unhold, counting only its
			// attempts dealt at the top rank: each fail of the run spent one of
			// the last bounces, and those dealt below frontier_after bounces
			// were escalation's room (escalate_after, frontier_after).
			stuck := max(0, min(t.noProgress(c), spent, c.Bounces-t.lim.FrontierAfter))
			if c.Kind != "producer" || spent < t.lim.Bounces && faults < t.lim.Faults && stuck < t.lim.SameFinding || !t.free(p) || t.hasLive(p) {
				continue
			}
			why := fmt.Sprintf("bounced %d times, the limit is %d", spent, t.lim.Bounces)
			switch {
			case spent < t.lim.Bounces && faults >= t.lim.Faults:
				why = fmt.Sprintf("reads faulted %d times running at review %d, the limit is %d: %s", faults, c.Review, t.lim.Faults, last)
			case spent < t.lim.Bounces:
				why = fmt.Sprintf("failed %d top-rank attempts running with no new head on the same brief, the limit is %d", stuck, t.lim.SameFinding)
			case c.BounceBase > 0:
				why = fmt.Sprintf("bounced %d times since its last unhold, the limit is %d", spent, t.lim.Bounces)
			}
			if g := t.group("hold", p, t.producerMove(p, card.HoldCard{Reason: card.HeldPrefix + why})); g != nil {
				g.judge = t.riding("held", p, "producer "+p+" "+why+"; it waits held until you review it")
			}
		}
	}
	t.routeHeld()
	t.refutedHeld()
	for _, n := range t.rowNames {
		t.rowLimits(n)
	}
}

// noProgress is how many of c's latest work attempts, running, failed with
// no head pushed on the brief as it is now: once the top rank has failed it
// twice so, the same brief dealt again fails again (policy's same_finding,
// LESSONS 5.9's "the same thing twice means the brief is wrong"; live
// 2026-11-09). A fault or a withdraw that pushed nothing is no attempt and
// is passed over; an ok, a pushed head or an edited brief (prose, paths or
// done-when) ends the run. Every fail spent a bounce, so the caller caps the
// run at the bounces since the last unhold.
func (t *tick) noProgress(c store.Card) int {
	if c.State != "ready" {
		return 0
	}
	n := 0
	for a := c.Attempt; a >= 1; a-- {
		w, ok := t.v.Cards[c.ID+".w"+itoa(a)]
		switch {
		case !ok || w.Head != "" || w.Prose != c.Prose || !slices.Equal(w.Paths, c.Paths) || !slices.Equal(w.DoneWhen, c.DoneWhen) || w.State == "done" && w.Result == "ok":
			return n
		case w.State == "done" && w.Result == "fail":
			n++
		}
	}
	return n
}

// refutedHeld opens the held judgement of a producer whose attempt refuted
// its brief (card.Refuted): Broken left it waiting held with the worker's
// evidence, never dealt again by itself.
func (t *tick) refutedHeld() {
	for _, p := range t.v.Producers("waiting") {
		c := t.v.Cards[p]
		w, ok := t.v.Cards[workID(c)]
		if !ok || w.State != "done" || w.Result != "fail" || !card.Refuted(w.Reason) || c.Reason != card.HeldPrefix+strings.TrimSpace(w.Reason) {
			continue // held by the refutation itself, never by a later hold of the coordinator's
		}
		t.judge("held", p, fmt.Sprintf("producer %s's attempt on row %s refuted its brief: %s; it waits, never dealt again by itself: drop it, replace it with a corrected brief (nova-sprint replace %s --by <card> --reason <why>), or unhold to deal it again", p, w.Holder, w.Reason, p))
	}
}

// readFaults is how many reads of producer c's current review and head are
// done with a fault, and the newest one's reason: the faults running, since
// a done read that answered ends the review at the verdict. 0 when one
// answered or c is not in review.
func (t *tick) readFaults(c store.Card) (n int, last string) {
	if c.State != "review" {
		return 0, ""
	}
	for _, r := range t.v.Consumers(c.ID) { // in birth order
		if r.Kind != "read" || r.Review != c.Review || r.Head != c.Head || r.State != "done" {
			continue
		}
		if r.Result != "fault" {
			return 0, ""
		}
		n, last = n+1, r.Reason
	}
	return n, last
}

func (t *tick) hasLive(p string) bool {
	for id := range t.v.consOf[p] {
		if isLive(t.v.Cards[id]) {
			return true
		}
	}
	return false
}

// rowLimits is the silence, deadline and flap rules of one row. A row
// absent from Seen has not been seen on the bus since the run started, so
// its silence counts from the view's start (item 2, 2026-11-08: a friend
// off the bus across a run restart held its cards until a manual release).
// A working consumer past its tier's deadline (timeouts consumer, counted
// from the deal plus the take window, so never before the take receipt's
// deadline) with no report is withdrawn.
func (t *tick) rowLimits(n string) {
	r := t.v.Rows[n]
	if r.Flaps >= t.lim.FlapLimit {
		t.judgeAt("flap", n, r.Flaps, fmt.Sprintf("row %s has flapped %d times (down and up); the limit is %d", n, r.Flaps, t.lim.FlapLimit))
	}
	seen, known := t.in.Seen[n]
	if !known {
		seen = t.v.Since
	}
	silent := t.in.Now.Sub(seen)
	for _, id := range t.v.Live(n) {
		c := t.v.Cards[id]
		if c.State != "working" || !t.unclaimed(id) || !t.free(c.Of) {
			continue
		}
		deadline := t.tmo.Consumer[c.Tier]
		if since := t.in.Now.Sub(t.dealtAt(id)); deadline > 0 && since >= seconds(t.tmo.ReadyTake+deadline) {
			t.group("withdraw", id, t.withdraw(id, fmt.Sprintf("working %d s with no report, past tier %s's deadline of %d s after the take window; branch %s is kept", int(since/time.Second), c.Tier, deadline, c.Branch)))
		} else if silent >= seconds(t.tmo.RowSilentWithdraw) {
			t.group("withdraw", id, t.withdraw(id, fmt.Sprintf("row %s silent for %s; branch %s is kept", n, silent.Truncate(time.Second), c.Branch)))
		}
	}
	if r.State == "up" && silent >= seconds(t.tmo.RowSilentGone) && t.unclaimed(n) {
		t.group("gone", "", t.handMoves(n, "down", card.Gone{})...)
	}
}

// rest marks the rows resting after faults (LPM R34, LESSONS 4.24): the
// limit's count of faults among the row's last route_rest_window finishes,
// within the rest bound, rests the row for the rest bound after the last; a
// deal filter, never a stored hold (R37).
func (t *tick) rest() {
	for n, ends := range t.v.Ends {
		window := seconds(t.tmo.RouteRestFor)
		recent := 0
		var last time.Time
		for _, e := range ends[max(0, len(ends)-t.lim.RouteRestWindow):] {
			if at := e.At; e.Fault && !at.Before(t.in.Now.Add(-window)) {
				recent++
				if at.After(last) {
					last = at
				}
			}
		}
		if recent >= t.lim.RouteRestAfter && t.in.Now.Before(last.Add(window)) {
			t.rested[n] = struct{}{}
		}
	}
}
