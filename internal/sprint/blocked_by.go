package sprint

import (
	"fmt"
	"maps"
	"strings"
)

// A finish whose report names the cards it waits for has said what the card waits
// for, and the machine reads it (docs/SPEC-CARD-CONTRACT.md, the BLOCKED-BY line;
// docs/SPEC-SPRINT.md section 8; tla/WaitFor.tla FinishHoldGateFix,
// FinishHoldHiddenCause and Split).
//
// A named card that is open, is not the card itself and does not need it (over
// needs and LAND-AFTER of open cards) becomes a need: the card waits, its head
// kept for the next attempt's carry, and the attempt counts toward no bound and
// no attempt cap (FieldDecidedAttempt), since it is not the brief's failure.
// A named card that needs the card (over needs), or whose need would close a
// cycle through LAND-AFTER, writes no need: it is one NGateFixCycle judgment
// naming both cards, the shared DONE-WHEN command when one is found, and the
// split. A name that is not an open card is ignored: the finish goes the way it
// does today.

// blockedByLine is the BLOCKED-BY line of a finished report: the ids it names and
// the line itself, "" when the report names none.
func blockedByLine(report string) (ids []string, line string) {
	for _, raw := range strings.Split(report, "\n") {
		l := strings.TrimSpace(raw)
		const key = "BLOCKED-BY:"
		if len(l) < len(key) || !strings.EqualFold(l[:len(key)], key) {
			continue
		}
		line = l
		for _, part := range strings.Split(l[len(key):], ",") {
			if id := strings.TrimSpace(part); id != "" {
				ids = append(ids, id)
			}
		}
		return ids, line
	}
	return nil, ""
}

// blockedOpen is the named card when it is an open card on the work table: not the
// card itself, not landed, and not a control or sentinel.
func blockedOpen(s *Snapshot, self, id string) *Card {
	if id == "" || id == self {
		return nil
	}
	c := s.Work.Placed(id)
	if c == nil || c.Col == Landed || c.F("kind") == "control" || IsSentinel(c) {
		return nil
	}
	return c
}

// blockedReach says to is reachable from from over the open cards' edges: their
// needs, and with landAfter their LAND-AFTER too.
func blockedReach(s *Snapshot, from, to string, landAfter bool) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if id == to {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		c := blockedOpen(s, "", id)
		if c == nil {
			return false
		}
		waived := Split(c.F("waived"))
		var edges []string
		for _, n := range Split(c.F("needs")) {
			if !contains(waived, n) {
				edges = append(edges, n)
			}
		}
		if landAfter {
			edges = append(edges, SoftDepsOf(s, c)...)
		}
		for _, n := range edges {
			if walk(n) {
				return true
			}
		}
		return false
	}
	return walk(from)
}

// blockedDoneWhen is the plain command of a brief's DONE-WHEN line, whitespace
// folded to single spaces: "" when it names none.
func blockedDoneWhen(brief string) string {
	for _, raw := range strings.Split(brief, "\n") {
		l := strings.TrimSpace(raw)
		const key = "DONE-WHEN:"
		if len(l) < len(key) || !strings.EqualFold(l[:len(key)], key) {
			continue
		}
		return strings.Join(strings.Fields(l[len(key):]), " ")
	}
	return ""
}

// blockedPref is a snapshot with the card's needs as the finish will write them,
// the pre-state the lifecycle judges a working -> waiting move by (lifecycle.go,
// namedNeedsOpen): the need is written in the same plan, so the move reads it.
func blockedPref(s *Snapshot, id string, needs []string) *Snapshot {
	pre := *s
	pre.Work = s.Work.Clone()
	c := pre.Work.Card(id)
	if c == nil {
		return &pre
	}
	cp := *c
	cp.Fields = maps.Clone(c.Fields)
	if cp.Fields == nil {
		cp.Fields = map[string]string{}
	}
	cp.Fields["needs"] = strings.Join(needs, ",")
	pre.Work.Put(&cp)
	return &pre
}

// blockedByFinish is the finish of a card whose report names BLOCKED-BY: the plan
// that reads it, and true when there is one. It returns false when the report
// names no such line, names no open card, or the finish is not a failed one.
func blockedByFinish(s *Snapshot, r FinishReq) (Plan, bool) {
	if !r.Failed {
		return Plan{}, false
	}
	ids, line := blockedByLine(r.Report)
	if len(ids) == 0 {
		return Plan{}, false
	}
	sel := r.Sel.IDs
	if r.Sel.Only != nil {
		sel = r.Sel.Only
	}
	var p Plan
	handled := false
	for _, id := range sel {
		c := s.Fleet.Placed(id)
		if c == nil {
			if pr := s.Work.Placed(id); pr != nil {
				c = s.Fleet.Placed(pr.F("work"))
			}
		}
		if c == nil || c.Col != Working || c.F("kind") == "read" {
			continue
		}
		pr := s.Work.Placed(c.F("primary"))
		if pr == nil || pr.Col != Working {
			continue
		}
		head := r.Head
		if head == "" {
			head = c.ID
		}
		var needs []string
		var judge string
		for _, want := range ids {
			other := blockedOpen(s, pr.ID, want)
			if other == nil {
				continue
			}
			if blockedReach(s, other.ID, pr.ID, false) {
				judge = other.ID
				break
			}
			merged := append(Split(pr.F("needs")), want)
			if blockedReach(s, other.ID, pr.ID, true) || needsCycleWith(s, pr.ID, map[string][]string{pr.ID: merged}) != "" {
				judge = other.ID
				break
			}
			if !contains(needs, want) && !contains(Split(pr.F("needs")), want) {
				needs = append(needs, want)
			}
		}
		if judge == "" && len(needs) == 0 {
			continue
		}
		handled = true
		cardSet := nextGen(c, "", s.Now)
		cardSet["withdrawn"] = stamp(s.Now)
		cardSet[FieldTakenBack] = "its report named what it waits for: " + line
		if judge != "" {
			// class A: no need; one judgment, and the attempt still counts toward no bound
			set := map[string]string{
				"head":              head,
				"result":            "failed",
				FieldDecidedAttempt: pr.F("attempt"),
				"why":               fmt.Sprintf("%s named %s (BLOCKED-BY), which needs it: the gate fix is split, never a need", pr.ID, judge),
			}
			cmd := blockedDoneWhen(pr.F("brief"))
			if o := s.Work.Card(judge); o != nil {
				if other := blockedDoneWhen(o.F("brief")); other != "" && other == cmd {
					// the shared DONE-WHEN, by plain word compare
				} else {
					cmd = ""
				}
			}
			instruction := "the shared DONE-WHEN command"
			if cmd != "" {
				instruction = "'" + cmd + " green'"
			}
			split := fmt.Sprintf("twin %s --needs %s --instruction %s", judge, pr.ID, instruction)
			n := judgment(NGateFixCycle, pr.Row, s.Now, 0, pr.ID, judge)
			n.Who, n.Attempt, n.Card, n.Other = r.Who, pr.Int("attempt"), pr.ID, judge
			n.Marked = true
			n.What = fmt.Sprintf("%s HOLDs on %s (BLOCKED-BY: %s), which needs it: %s. split it (%s), rework with a fix, or drop it; no need is written and the attempt counts toward no bound.",
				pr.ID, judge, judge, line, split)
			n.Decisions = []string{split, "rework with a fix", "drop"}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
				change(Fleet, removeEntry(c, cardSet)),
				change(Work, moveEntry(pr, pr.Row, Review, set, "result")),
			}, Notes: []Note{n}, Moved: fmt.Sprintf("%s working -> pool, %s working -> review, the gate-fix judgment", c.ID, pr.ID)})
			continue
		}
		// class I: the named open cards become its needs; it waits, its head kept
		all := append(Split(pr.F("needs")), needs...)
		all = without(all, Split(pr.F("waived")))
		set := map[string]string{
			"needs":             strings.Join(all, ","),
			"head":              head,
			FieldDecidedAttempt: pr.F("attempt"),
			"why":               fmt.Sprintf("%s named what it waits for (BLOCKED-BY): %s; the needs %s are written and it waits for them", pr.ID, line, strings.Join(needs, ",")),
		}
		note := happened(NBlockedBy, pr.Row, s.Now, pr.ID)
		note.Who, note.Attempt, note.Card = r.Who, pr.Int("attempt"), c.ID
		note.What = fmt.Sprintf("%s HOLDs on %s (BLOCKED-BY: %s): the needs %s are written; %s -> waiting, its head %s kept for its next attempt; the attempt counts toward no bound and no attempt cap",
			pr.ID, strings.Join(needs, ","), line, strings.Join(needs, ","), pr.ID, head)
		u := Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
			change(Fleet, removeEntry(c, cardSet)),
			change(Work, moveEntry(pr, pr.Row, Waiting, set, "result")),
		}, Notes: []Note{note}, Moved: fmt.Sprintf("%s working -> pool, %s working -> waiting (its needs %s)", c.ID, pr.ID, strings.Join(needs, ","))}
		p.Units = append(p.Units, u)
	}
	if !handled {
		return Plan{}, false
	}
	p.pre = blockedPref(s, firstUnitID(p, s), firstUnitNeeds(p))
	return p, true
}

func firstUnitID(p Plan, s *Snapshot) string {
	if len(p.Units) == 0 {
		return ""
	}
	for _, ch := range p.Units[0].Changes {
		if ch.Table == Work {
			return ch.Entry.ID
		}
	}
	return ""
}

func firstUnitNeeds(p Plan) []string {
	if len(p.Units) == 0 {
		return nil
	}
	for _, ch := range p.Units[0].Changes {
		if ch.Table == Work && ch.Entry.Set != nil {
			return Split(ch.Entry.Set["needs"])
		}
	}
	return nil
}
