package sprint

import (
	"fmt"
	"strings"
	"time"
)

// A fleet member's hold carries when it releases itself (the owner, 2026-10-14:
// "We should add an expiry time for fleet member holds."): an RFC3339 UTC timestamp in
// FieldHeldUntil on the member's control card (ctl-<member>), or empty when the hold is
// explicitly indefinite (--indefinite). The tick's holdCauseClear ends a timed hold in
// the first step whose snapshot clock is at or past FieldHeldUntil (tla/HoldUntil.tla
// NoTimedHoldOutlivesItsTick), never ends an indefinite hold by time
// (IndefiniteHoldNeverReleasedByTick), and --extend <duration> pushes an active timed
// hold's FieldHeldUntil out by adding to the existing timestamp (ExtendNeverShortens).

// FieldHeldUntil is a held fleet member's control card's RFC3339 UTC timestamp when a
// timed hold releases itself; absent when the hold is indefinite.
const FieldHeldUntil = "held_until"

// holdUntilWhy validates a hold request's release-time flags (--until, --for,
// --indefinite, --extend) against the resolved targets and snapshot clock, returning the
// canonical RFC3339 UTC timestamp for FieldHeldUntil ("" when indefinite or unset) and
// a refusal reason when the flags are invalid.
func holdUntilWhy(s *Snapshot, r HoldReq, targets []HoldTarget) (string, string) {
	if r.Release {
		return "", ""
	}
	anyMember := false
	for _, t := range targets {
		if t.Kind == HoldMember {
			anyMember = true
		}
	}
	hasEndFlag := r.Until != "" || r.For != 0 || r.Indefinite || r.Extend != 0 || r.Flag != ""
	if hasEndFlag && (!anyMember || r.Extend != 0 || r.Flag == "--extend") {
		for _, t := range targets {
			if t.Kind != HoldMember {
				flag := r.Flag
				if flag == "" {
					switch {
					case r.Extend != 0:
						flag = "--extend"
					case r.For != 0:
						flag = "--for"
					case r.Until != "":
						flag = "--until"
					case r.Indefinite:
						flag = "--indefinite"
					}
				}
				return "", fmt.Sprintf("%s names a fleet member's hold: %s is a %s", flag, t.Name, holdKindWords[t.Kind])
			}
		}
	}
	if r.Extend != 0 || r.Flag == "--extend" {
		if r.Extend <= 0 {
			return "", "--extend wants a duration above 0 (e.g. 30m, 2h)"
		}
		if r.Until != "" || r.For != 0 || r.Indefinite || r.Cause != "" || r.Return {
			return "", "--extend cannot be combined with --until, --for, --indefinite, --cause or --return"
		}
		for _, t := range targets {
			ctl := s.MemberCtl(t.Name)
			if ctl == nil || ctl.F("held") == "" {
				return "", t.Name + " is not held: --extend pushes out an active timed hold"
			}
			if ctl.F(FieldHeldUntil) == "" {
				return "", t.Name + " has an indefinite hold: --extend wants an active timed hold"
			}
			if _, err := time.Parse(time.RFC3339, ctl.F(FieldHeldUntil)); err != nil {
				return "", t.Name + " has an invalid held_until " + ctl.F(FieldHeldUntil)
			}
		}
		return "", ""
	}
	if !anyMember {
		return "", ""
	}
	n := 0
	if r.Until != "" {
		n++
	}
	if r.For != 0 || r.Flag == "--for" {
		n++
	}
	if r.Indefinite {
		n++
	}
	if n > 1 {
		return "", "choose one of --until <RFC3339>, --for <duration> or --indefinite"
	}
	if n == 0 {
		if r.RequireEnd {
			return "", "holding a fleet member wants one of --until <RFC3339>, --for <duration> or --indefinite"
		}
		return "", ""
	}
	if r.Indefinite {
		return "", ""
	}
	if r.For != 0 || r.Flag == "--for" {
		if r.For <= 0 {
			return "", "--for wants a duration above 0 (e.g. 30m, 2h)"
		}
		return s.Now.UTC().Truncate(time.Second).Add(r.For).Format(time.RFC3339), ""
	}
	u, err := time.Parse(time.RFC3339, r.Until)
	if err != nil {
		return "", fmt.Sprintf("--until %q wants an RFC3339 timestamp (e.g. 2030-01-02T05:00:00Z)", r.Until)
	}
	now := s.Now.UTC().Truncate(time.Second)
	u = u.UTC().Truncate(time.Second)
	if !u.After(now) {
		return "", fmt.Sprintf("--until %s must be in the future (after %s)", u.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	return u.Format(time.RFC3339), ""
}

// holdExtendPlan pushes each target fleet member's FieldHeldUntil out by r.Extend,
// leaving all other hold fields unchanged (tla/HoldUntil.tla Extend, ExtendNeverShortens).
func holdExtendPlan(s *Snapshot, r HoldReq, targets []HoldTarget) Plan {
	var p Plan
	now := s.Now.UTC().Truncate(time.Second)
	for _, t := range targets {
		ctl := s.MemberCtl(t.Name)
		cur, _ := time.Parse(time.RFC3339, ctl.F(FieldHeldUntil))
		base := cur.UTC().Truncate(time.Second)
		if now.After(base) {
			base = now
		}
		next := base.Add(r.Extend).Format(time.RFC3339)
		moved := fmt.Sprintf("%s hold extended by %s until %s", t.Name, HoldDurationWords(r.Extend), next)
		p.Units = append(p.Units, Unit{
			Key:     ctl.ID,
			Changes: []Change{change(Fleet, setEntry(ctl, map[string]string{FieldHeldUntil: next}))},
			Moved:   moved,
		})
		n := happened(NHeld, "", s.Now)
		n.Who = r.Who
		n.What = "member " + moved
		if r.Reason != "" {
			n.What += ": " + r.Reason
		}
		p.Notes = append(p.Notes, n)
	}
	return Lawful(p)
}

// holdUntilExpired says a held member's timed hold has reached or passed its release
// time at s.Now; an indefinite hold (empty FieldHeldUntil) is never expired.
func holdUntilExpired(s *Snapshot, ctl *Card) (string, bool) {
	if ctl == nil || ctl.F("held") == "" {
		return "", false
	}
	until := ctl.F(FieldHeldUntil)
	if until == "" {
		return "", false
	}
	u, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return "", false
	}
	return until, !s.Now.Before(u)
}

// HoldUntilWords formats when a member hold releases for where and the dashboard:
// "releases at <RFC3339> (in <d>)" while in the future, "releases at <RFC3339> (overdue
// by <d>)" once reached or past before the next tick releases it, and "indefinite" when
// untilText is empty.
func HoldUntilWords(untilText string, now time.Time) string {
	if untilText == "" {
		return "indefinite"
	}
	u, err := time.Parse(time.RFC3339, untilText)
	if err != nil || now.IsZero() {
		return "releases at " + untilText
	}
	rem := u.Sub(now.UTC().Truncate(time.Second))
	if rem > 0 {
		return fmt.Sprintf("releases at %s (in %s)", untilText, HoldDurationWords(rem))
	}
	return fmt.Sprintf("releases at %s (overdue by %s)", untilText, HoldDurationWords(-rem))
}

// HoldDurationWords formats a positive duration without trailing zero units (e.g. "1h",
// "30m", "1h15m", "45s").
func HoldDurationWords(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	sec := int((d % time.Minute) / time.Second)
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if sec > 0 || b.Len() == 0 {
		fmt.Fprintf(&b, "%ds", sec)
	}
	return b.String()
}
