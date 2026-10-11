package sprint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/typedrec"
)

// rework --carry (docs/SPEC-SPRINT.md, the rework verb): the one verb that answers a bound
// judgment and a coordination brief-defect judgment. For each card selected it keeps the
// brief, records the card's latest pushed head as its carry (the same CARRY: line --widen
// writes, member.CarryLine's shape), writes the given fix as the next attempt's fix, resets
// the brief's bound (FieldBriefAttempt) and opens the next attempt staged from that head, in
// place, as a brief edit does (Brief, briefInPlace). A card none of whose attempts pushed a
// full head cannot be carried: it refuses the whole step, by name, and nothing is written.
//
// A card a worker held on a coordination brief defect (its report said it was dealt before
// its DEPENDS-ON or ORDER prerequisites landed) is carried once those needs have landed; a
// card whose needs are still unlanded is refused, naming them. Today plain rework refuses
// such a card ("a rework deals the same brief again; re-cut the brief"), which left the seat
// dumping and re-briefing each card by hand (2026-10-11).
func reworkCarried(s *Snapshot, r ReworkReq) Plan {
	var p Plan
	p.on(s)
	ids := r.IDs
	if r.Only != nil {
		ids = r.Only
	}
	var chosen []*Card
	if len(ids) > 0 {
		chosen = pick(&p, r.Sel, nil, rowOf, func(*Card) string { return "" }, s.primaryCard)
	} else {
		all := slices.Clone(s.Work.Column(Review))
		for _, c := range s.Work.Column(Ready) {
			if AtRedealBound(s, c) != nil {
				all = append(all, c)
			}
		}
		chosen = pick(&p, r.Sel, all, rowOf, func(*Card) string { return "" }, s.primaryCard)
	}
	// hold nothing back: every card that cannot be carried is refused by name before any
	// brief is edited, so the step is all or none and nothing is written
	heads := map[string]string{}
	for _, c := range chosen {
		head := carriedHead(s, c)
		if head == "" {
			p.refuse(c.ID, c.ID+" has no pushed head: --carry stages its next attempt from the last head it pushed, and no attempt of it pushed one; nothing was written")
			continue
		}
		heads[c.ID] = head
		if c.F(FieldBriefDefect) == BriefDefectCoordination {
			if un := unlandedNeeds(s, c); len(un) > 0 {
				p.refuse(c.ID, c.ID+" names a coordination brief defect and its needs have not landed: "+strings.Join(un, ", ")+"; nothing was written")
			}
		}
	}
	if len(p.Refused) > 0 {
		return p
	}
	for _, c := range chosen {
		carry := fmt.Sprintf("CARRY: %s attempt %d head=%s", c.ID, c.Int("attempt"), heads[c.ID])
		q := Brief(s, BriefReq{ID: c.ID, Brief: briefCarry(c.F("brief"), carry), Rules: c.F(FieldRules), Who: r.Who})
		if len(q.Refused) > 0 {
			p.Refused = append(p.Refused, q.Refused...)
			continue
		}
		for i := range q.Units {
			u := &q.Units[i]
			if u.Key != c.ID {
				continue
			}
			for j := range u.Changes {
				ch := &u.Changes[j]
				if ch.Table != Work || ch.Entry.ID != c.ID || ch.Entry.Set == nil {
					continue
				}
				if r.Fix != "" {
					ch.Entry.Set["fix"] = cutText(r.Fix, MaxCardTextBytes)
				}
			}
			// a coordination brief defect is answered as well: Brief closes the bound's
			// judgments (ReworkResolves) but not NBriefDefect
			for _, o := range closesFor(s.Open, []string{NBriefDefect}, c.ID) {
				if !slices.ContainsFunc(u.Closes, func(x Open) bool { return x.Key == o.Key }) {
					u.Closes = append(u.Closes, o)
				}
			}
		}
		p.Units = append(p.Units, q.Units...)
		p.Rows = append(p.Rows, q.Rows...)
		p.Places = append(p.Places, q.Places...)
		p.Notes = append(p.Notes, q.Notes...)
		p.Updates = append(p.Updates, q.Updates...)
		p.Props = append(p.Props, q.Props...)
	}
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}

// carriedHead is the head a carry starts a card's next attempt from: the latest attempt's
// pushed head (BaseOf, the one place that decides where a later attempt starts), else the
// primary's own recorded head. "" when no attempt pushed a full head.
func carriedHead(s *Snapshot, c *Card) string {
	if s.Fleet != nil {
		if b := BaseOf(s.Fleet.Of(c.ID)); b.Head != "" {
			return b.Head
		}
	}
	if h := c.F("head"); typedrec.IsFullSha(h) {
		return h
	}
	return ""
}

// unlandedNeeds is the card's needs (its DEPENDS-ON: line) that have not landed.
func unlandedNeeds(s *Snapshot, c *Card) []string {
	var out []string
	for _, n := range Split(c.F("needs")) {
		if s.StateOf(n) != Landed {
			out = append(out, n)
		}
	}
	return out
}

// briefCarry is brief with carry as its CARRY: line: an existing CARRY: line in the header is
// replaced, else the line is inserted after line 1. Every other line is kept byte for byte, so
// a carry edit changes the brief only by its carry.
func briefCarry(brief, carry string) string {
	lines := strings.Split(brief, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "CARRY:") {
			lines[i] = carry
			return strings.Join(lines, "\n")
		}
	}
	return strings.Join(slices.Insert(lines, min(1, len(lines)), carry), "\n")
}
