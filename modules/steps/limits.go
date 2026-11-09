package steps

import (
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// limits is the retire and fail limits step (LAYER-3 "Retire and fail
// limits"): HoldCard with a held judgement when bounces since the last
// unhold reach the limit (a fault never counts, LESSONS 2.24; an unhold is a
// fresh budget); Withdraw of a working consumer whose
// row is silent past the bound, or that is past its tier's deadline with no
// report, branch kept; Gone for a row unseen past its
// bound, never for one missed challenge (LESSONS 4.7: the bound is policy's
// and long); a flap judgement at the flap limit.
func (t *tick) limits() {
	for _, state := range []string{"ready", "review"} {
		for _, p := range t.v.Producers(state) {
			c := t.v.Cards[p]
			spent := c.Bounces - c.BounceBase // since its last unhold
			if c.Kind != "producer" || spent < t.lim.Bounces || !t.free(p) || t.hasLive(p) {
				continue
			}
			why := fmt.Sprintf("bounced %d times, the limit is %d", spent, t.lim.Bounces)
			if c.BounceBase > 0 {
				why = fmt.Sprintf("bounced %d times since its last unhold, the limit is %d", spent, t.lim.Bounces)
			}
			if g := t.group("hold", p, t.producerMove(p, card.HoldCard{Reason: card.HeldPrefix + why})); g != nil {
				g.judge = t.riding("held", p, "producer "+p+" "+why+"; it waits held until you review it")
			}
		}
	}
	t.routeHeld()
	for _, n := range t.rowNames {
		t.rowLimits(n)
	}
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
		t.group("gone", "", t.rowMove(n, card.Gone{}))
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
