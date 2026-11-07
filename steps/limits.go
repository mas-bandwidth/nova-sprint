package steps

import (
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// limits is the retire and fail limits step (AGENTS.md, Steps): Block with a blocked judgement when bounces reach the limit (a
// fault never counts); Withdraw of a working consumer whose
// row is silent past the bound, branch kept; Gone for a row unseen past its
// bound, never for one missed challenge (the bound is policy's
// and long); a flap judgement at the flap limit.
func (t *tick) limits() {
	for _, state := range []string{"ready", "review"} {
		for _, p := range t.v.Producers(state) {
			c := t.v.Cards[p]
			if c.Kind != "producer" || c.Bounces < t.lim.Bounces || !t.free(p) || t.hasLive(p) {
				continue
			}
			why := fmt.Sprintf("bounced %d times, the limit is %d", c.Bounces, t.lim.Bounces)
			if g := t.group("block", p, t.producerMove(p, card.Block{Reason: why})); g != nil {
				g.judge = t.riding("blocked", p, "producer "+p+" "+why+"; it sits here until you review it")
			}
		}
	}
	t.routeBlocked()
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

// rowLimits is the silence and flap rules of one row. A row absent from
// Seen is unknown, and nothing fires on it.
func (t *tick) rowLimits(n string) {
	r := t.v.Rows[n]
	if r.Flaps >= Engine.FlapLimit {
		t.judgeAt("flap", n, r.Flaps, fmt.Sprintf("row %s has flapped %d times (down and up); the limit is %d", n, r.Flaps, Engine.FlapLimit))
	}
	seen, known := t.in.Seen[n]
	if !known {
		return
	}
	silent := t.in.Now.Sub(seen)
	if silent >= seconds(t.tmo.RowSilentWithdraw) {
		for _, id := range t.v.Live(n) {
			if c := t.v.Cards[id]; c.State == "working" && t.unclaimed(id) && t.free(c.Of) {
				t.group("withdraw", id, t.withdraw(id, fmt.Sprintf("row %s silent for %s; branch %s is kept", n, silent.Truncate(time.Second), c.Branch)))
			}
		}
	}
	if r.State == "up" && silent >= seconds(t.tmo.RowSilentGone) && t.unclaimed(n) {
		t.group("gone", "", t.rowMove(n, card.Gone{}))
	}
}

// rest marks the rows resting after faults: the
// limit's count of faults within the rest bound rests the row for the rest
// bound after the last; a deal filter, never a stored hold (R37).
func (t *tick) rest() {
	for n, faults := range t.v.Faults {
		window := seconds(t.tmo.RouteRestFor)
		recent := 0
		var last time.Time
		for _, at := range faults {
			if !at.Before(t.in.Now.Add(-window)) {
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
