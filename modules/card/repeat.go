package card

import (
	"fmt"
	"strings"
)

// A work fail that repeats the card's previous bounce (two audits,
// 2026-11-15: 17-20% of failed attempts were a card failing again with the
// identical reason, one merge conflict four times running, one GOPROXY off
// six times running) leaves the producer waiting held, as a refuted fail
// does: the same brief dealt again fails the same way, and escalation would
// spend frontier time on it. tla/CardMachine.tla, Broken and
// RepeatNeverRedealt.

// FailLine is a fail reason's first line, trimmed: what two bounces compare.
func FailLine(reason string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(reason), "\n")
	return strings.TrimSpace(line)
}

// failClass says whether work consumer c is a plain fail: reported fail,
// not refuting its brief (Refuted holds those on its own). Its bounce is
// one of the class a repeat compares.
func failClass(c Card) bool {
	return c.Kind == "work" && c.State == Done && c.Result == "fail" && !Refuted(c.Reason)
}

// lastReported is producer p's work consumer with the greatest attempt
// below below that reported ok or fail: a fault, a withdraw or a release
// pushed no verdict on the brief and is passed over, as noProgress passes
// it over (modules/steps/limits.go).
func lastReported(p string, cons []Card, below int) (Card, bool) {
	var out Card
	found := false
	for _, c := range cons {
		if c.Kind != "work" || c.Of != p || c.Attempt >= below || c.State != Done || c.Result != "ok" && c.Result != "fail" {
			continue
		}
		if !found || c.Attempt > out.Attempt {
			out, found = c, true
		}
	}
	return out, found
}

// RepeatedFail is the earlier attempt whose fail cur repeats, and whether
// there is one: cur, p's work consumer, is a plain fail (failClass) with a
// first reason line, p has bounced since its last unhold (an unhold is a
// fresh budget: what it was held for never counts again), and the latest
// earlier attempt that reported is a plain fail with the same first line.
// An ok between them ends the run (the bounce after it was a read's fix, or
// none), and so does an unhold. A pushed head, the rank it was dealt at and
// an edited brief do not: unlike same_finding (steps' noProgress), the same
// first line twice is the stop, whatever the attempts pushed (the audits'
// repeats pushed heads, so a run that restarts on a head never held them).
func RepeatedFail(p Card, cons []Card, cur Card) (Card, bool) {
	line := FailLine(cur.Reason)
	if !failClass(cur) || cur.Of != p.ID || line == "" || p.Bounces-p.BounceBase < 1 {
		return Card{}, false
	}
	prev, ok := lastReported(p.ID, cons, cur.Attempt)
	if !ok || !failClass(prev) || FailLine(prev.Reason) != line {
		return Card{}, false
	}
	return prev, true
}

// RepeatReason is the held reason Broken writes when cur repeats prev's
// fail: it names both attempts and the repeated line.
func RepeatReason(prev, cur Card) string {
	return HeldPrefix + fmt.Sprintf("failed the same way twice running, %s and %s: %s", prev.ID, cur.ID, FailLine(cur.Reason))
}

// LastBounce is what last sent producer p back, as policy's card input
// names it: "fail" when its latest reported work attempt (lastReported)
// failed, "fix" when that attempt finished ok and a read of p's current
// review said fix (the ReadFix bounce), "" otherwise (no attempt reported,
// or none bounced since its latest ok; a card in review has not been sent
// back since; after a Red, whose review rose, ""). "fix" reads the done fix
// read, not the ReadFix move: a card held from review over a fix read and
// unheld is "fix" too. Only the result: no reason or finding text.
func LastBounce(p Card, cons []Card) string {
	w, ok := lastReported(p.ID, cons, p.Attempt+1)
	switch {
	case !ok:
		return ""
	case w.Result == "fail":
		return "fail"
	case p.State == Review || p.State == Merging || p.State == LandedState:
		return ""
	}
	for _, c := range cons {
		if c.Kind == "read" && c.Of == p.ID && c.State == Done && c.Result == "fix" && c.Review == p.Review && c.Attempt == w.Attempt {
			return "fix"
		}
	}
	return ""
}

// RepeatHeld is the two attempts a waiting producer p is held on by a
// repeated fail (Broken's RepeatReason, never a later hold of the
// coordinator's, whose reason differs), and whether it is.
func RepeatHeld(p Card, cons []Card) (prev, cur Card, ok bool) {
	if p.State != Waiting {
		return Card{}, Card{}, false
	}
	for _, c := range cons {
		if c.Kind == "work" && c.Of == p.ID && c.Attempt == p.Attempt {
			cur, ok = c, failClass(c)
		}
	}
	if !ok {
		return Card{}, Card{}, false
	}
	prev, ok = lastReported(p.ID, cons, cur.Attempt)
	if !ok || !failClass(prev) || FailLine(prev.Reason) != FailLine(cur.Reason) || p.Reason != RepeatReason(prev, cur) {
		return Card{}, Card{}, false
	}
	return prev, cur, true
}
