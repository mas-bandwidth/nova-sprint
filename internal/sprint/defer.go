package sprint

import (
	"fmt"
	"strings"
)

// DeferReq is the coordinator deferring waiting cards to the roadmap: the verb
// writes each card's whole brief into the roadmap data, and this step drops
// them from the store. Only a waiting primary defers; a card in any other
// state is refused.
type DeferReq struct {
	Sel
	Reason  string
	Answers []string
	Who     string
}

// pickedForDrop chooses the cards a step that takes primaries off the table
// (drop, defer) acts on: the cards of the columns passed, each refused when it
// is not on the table, and the ids of the chosen, the dropping set both callers
// build before they differ.
func pickedForDrop(p *Plan, sel Sel, cols []*Card, refuse func(*Card) string, byID func(string) *Card) (chosen []*Card, dropping map[string]bool) {
	chosen = pick(p, sel, cols, rowOf, func(c *Card) string {
		if !c.Placed() {
			return "not on the table (" + orDash(c.F("outcome")) + ")"
		}
		return refuse(c)
	}, byID)
	dropping = make(map[string]bool, len(chosen))
	for _, c := range chosen {
		dropping[c.ID] = true
	}
	return chosen, dropping
}

// Defer takes waiting primaries off the table with the reason, as Drop does,
// but only from the waiting column: a card that is no longer waiting when the
// step runs (it moved ready) is refused and nothing of it changes. Its record,
// outcome and reason are kept, and the roadmap write is the verb's own (the
// store step only drops).
//
// tla/NeedsBlocked.tla NoStrandingDrop
func Defer(s *Snapshot, r DeferReq) Plan {
	var p Plan
	chosen, dropping := pickedForDrop(&p, r.Sel, s.Work.Column(Waiting), func(c *Card) string {
		if c.Col != Waiting {
			return "not waiting (it is " + c.Col + ")"
		}
		if IsSentinel(c) {
			return "a sentinel is released, not deferred"
		}
		return ""
	}, s.primaryCard)
	for _, c := range chosen {
		if deps := unlandedNeeding(s, c.ID, dropping); len(deps) > 0 {
			p.refuse(c.ID, fmt.Sprintf("%s is needed by %s; defer them too, or drop %s with --cascade, --relink <card> or --waive", c.ID, strings.Join(deps, ", "), c.ID))
		}
	}
	if len(p.Refused) > 0 {
		return Lawful(p)
	}
	for _, c := range chosen {
		u := Unit{Key: c.ID, Stream: c.Row}
		for _, fc := range s.Fleet.Of(c.ID) {
			if fc.Col == Ready || fc.Col == Working || fc.Col == Withdrawn {
				u.Changes = append(u.Changes, change(Fleet, removeEntry(fc, map[string]string{"dropped": stamp(s.Now)})))
			}
		}
		for _, rc := range s.Readers.Of(c.ID) {
			if rc.Col == Asked || rc.Col == Reading {
				u.Changes = append(u.Changes, change(Readers, removeEntry(rc, map[string]string{"dropped": stamp(s.Now)})))
			}
		}
		if m := s.Merge.Placed(c.ID); m != nil {
			u.Changes = append(u.Changes, change(Merge, removeEntry(m, map[string]string{"dropped": stamp(s.Now)})))
		}
		u.Changes = append(u.Changes, change(Work, removeEntry(c, map[string]string{
			"outcome": "dropped", "reason": r.Reason, "dropped_from": c.Col, "dropped_at": stamp(s.Now)})))
		u.Closes = closesFor(s.Open, nil, c.ID)
		answerListed(&u, s.Open, r.Answers, "defer", c.Row, "deferred "+c.ID+" to the roadmap; "+r.Reason, r.Who, s.Now, c.ID)
		u.Moved = fmt.Sprintf("%s %s -> off the table (%s)", c.ID, c.Col, r.Reason)
		p.Units = append(p.Units, u)
	}
	p.Units = append(p.Units, weighUnits(s, nil, dropping)...)
	settle(&p, s, r.Who, dropping, dropping)
	for _, st := range unitStreams(p) {
		k := 0
		for _, u := range p.Units {
			if u.Stream == st && dropping[u.Key] {
				k++
			}
		}
		setStream(&p, s, st, map[string]string{"dropped": itoa(s.StreamCtl(st).Int("dropped") + k)})
	}
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}
