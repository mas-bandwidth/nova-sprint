package steps

import (
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// limits is the retire and fail limits step (LAYER-3 "Retire and fail
// limits"): Block with a blocked judgement when bounces reach the limit (a
// fault never counts, LESSONS 2.24; the candidates are the producers with a
// bounce, indexed); Withdraw of a working consumer whose row is silent past
// the bound, branch kept; Gone for a row unseen past its bound, never for
// one missed challenge (LESSONS 4.7: the bound is policy's and long); a flap
// judgement at the flap limit.
func (t *tick) limits() {
	for _, p := range sorted(t.v.bounced) {
		c := t.v.Cards[p]
		if c.Kind != "producer" || c.Bounces < t.lim.Bounces || !t.free(p) || t.hasLive(p) {
			continue
		}
		why := fmt.Sprintf("bounced %d times, the limit is %d", c.Bounces, t.lim.Bounces)
		if g := t.group("block", p, t.producerMove(p, card.Block{Reason: why})); g != nil {
			g.judge = t.riding("blocked", p, "producer "+p+" "+why+"; it sits here until you review it")
		}
	}
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
	if r.Flaps >= t.lim.FlapLimit {
		t.judgeAt("flap", n, r.Flaps, fmt.Sprintf("row %s has flapped %d times (down and up); the limit is %d", n, r.Flaps, t.lim.FlapLimit))
	}
	seen, known := t.in.Seen[n]
	if !known {
		return
	}
	if t.due(seen, t.tmo.RowSilentWithdraw) {
		for _, id := range t.v.Live(n) {
			if c := t.v.Cards[id]; c.State == "working" && t.unclaimed(id) && t.free(c.Of) {
				t.group("withdraw", id, t.withdraw(id, fmt.Sprintf("row %s silent for %s; branch %s is kept", n, t.in.Now.Sub(seen).Truncate(time.Second), c.Branch)))
			}
		}
	}
	if _, going := t.silent[n]; going && t.unclaimed(n) {
		t.group("gone", "", t.rowMove(n, card.Gone{}))
	}
}

// goingDown marks the rows up and unseen past the gone bound: the limits
// step takes them down this tick, so no step deals to them (round 2 ruling
// 5: the candidates are computed after limits and rest claim rows).
func (t *tick) goingDown() {
	for _, n := range t.rowNames {
		if seen, known := t.in.Seen[n]; known && t.v.Rows[n].State == "up" && t.due(seen, t.tmo.RowSilentGone) {
			t.silent[n] = struct{}{}
		}
	}
}

// rest marks the rows resting after faults (LPM R34, LESSONS 4.24): when
// limits' faults count of the row's last route_rest_window endings were
// faults and the last fault is within the rest bound, the row rests for the
// rest bound after it; a deal filter, never a stored hold (R37). The
// endings are the fold's witness (View.Ended).
func (t *tick) rest() {
	for n, ended := range t.v.Ended {
		if len(ended) > t.lim.RouteRestWindow {
			ended = ended[len(ended)-t.lim.RouteRestWindow:]
		}
		faults := 0
		var last time.Time
		for _, e := range ended {
			if e.Fault {
				faults++
				if e.At.After(last) {
					last = e.At
				}
			}
		}
		if faults >= t.lim.Faults && !t.due(last, t.tmo.RouteRestFor) {
			t.rested[n] = struct{}{}
		}
	}
}
