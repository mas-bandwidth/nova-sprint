package steps

import (
	"fmt"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// limits is the retire and fail limits step (LAYER-3 "Retire and fail
// limits"): HoldCard with a held judgement when bounces since the last
// unhold reach the limit (a fault never counts, LESSONS 2.24; an unhold is a
// fresh budget), or when a ready producer's faults running reach a multiple
// of policy's faults limit while it is ready on the newest of them (the
// model's FaultDue: the hold changes its reason, so an unhold is a fresh
// budget here too); Withdraw of a working consumer whose
// row is silent past the bound, branch kept; Gone for a row unseen past its
// bound, never for one missed challenge (LESSONS 4.7: the bound is policy's
// and long); a flap judgement at the flap limit.
func (t *tick) limits() {
	for _, state := range []string{"ready", "review"} {
		for _, p := range t.v.Producers(state) {
			c := t.v.Cards[p]
			spent := c.Bounces - c.BounceBase // since its last unhold
			faults, onFault := t.v.faultRun(c)
			faultDue := state == "ready" && onFault && faults > 0 && faults%t.lim.Faults == 0
			if c.Kind != "producer" || spent < t.lim.Bounces && !faultDue || !t.free(p) || t.hasLive(p) {
				continue
			}
			why := fmt.Sprintf("bounced %d times, the limit is %d", spent, t.lim.Bounces)
			switch {
			case spent < t.lim.Bounces:
				why = fmt.Sprintf("faulted %d times running, the limit is %d", faults, t.lim.Faults)
			case c.BounceBase > 0:
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

// faultRun is producer p's faults running (the model's FaultRun): its done
// work consumers' faults, newest attempt first, until one that did not
// fault (a consumer not done, or a missing-route fault, which holds by
// itself, is passed over); onFault is whether p is still ready on the
// newest (its newest work consumer is that fault and p's reason is the
// fault's: no deal, hold, unhold or park since; the model's fr).
func (v *View) faultRun(p store.Card) (n int, onFault bool) {
	var ws []store.Card
	for id := range v.consOf[p.ID] {
		if c := v.Cards[id]; c.Kind == "work" {
			ws = append(ws, c)
		}
	}
	slices.SortFunc(ws, func(a, b store.Card) int { return b.Attempt - a.Attempt })
	for i, c := range ws {
		_, route := card.RouteFault(c.Reason)
		switch {
		case c.State != "done" || route && c.Result == "fault":
			continue
		case c.Result != "fault":
			return n, onFault
		}
		if i == 0 {
			onFault = p.Reason == c.Reason
		}
		n++
	}
	return n, onFault
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
// Seen (not seen since the run's start) is silent since the view's start,
// so a row whose session died before a restart still goes Gone and has its
// working consumers withdrawn (tla/Steps.tla SilenceActs, witness
// StepsSilentUnseen).
func (t *tick) rowLimits(n string) {
	r := t.v.Rows[n]
	if r.Flaps >= Engine.FlapLimit {
		t.judgeAt("flap", n, r.Flaps, fmt.Sprintf("row %s has flapped %d times (down and up); the limit is %d", n, r.Flaps, Engine.FlapLimit))
	}
	seen, known := t.in.Seen[n]
	if !known {
		seen = t.v.Since
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

// rest marks the rows resting after faults (LPM R34, LESSONS 4.24): the
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
