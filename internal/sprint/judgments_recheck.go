package sprint

import "fmt"

// RecheckJudgments closes only existing obligations whose current causes ended.
// It never applies the deal planner's placements, new judgments or updates.
// tla/JudgmentRecheck.tla ClosedOnlyProven and OpenMatchesHistory.
func RecheckJudgments(s *Snapshot, who string) Plan {
	// Preserve the existing stream-specific retirement reason before generic
	// card retirement can consume a removed stream's judgment.
	p, _ := TickRetireGone(s, TickReq{Who: who})
	p.Said = nil // one consistent line per judgment below
	RetireJudgments(s, &p, who)
	closing := map[string]bool{}
	for _, o := range p.Closes {
		closing[o.Key] = true
	}
	close := func(o Open, why string) {
		if closing[o.Key] {
			return
		}
		closing[o.Key] = true
		p.Closes = append(p.Closes, o)
		p.Notes = append(p.Notes, decided(o, why, who, s.Now))
	}
	for _, o := range s.Open {
		if o.Note.Kind != Judgment {
			continue
		}
		if (o.Note.Type == NWorkLate || o.Note.Type == NReadLate) && !LateStands(s, o.Note) {
			close(o, "rechecked: the deadline's take or read is no longer live")
		}
	}
	deal, _ := TickDeal(s, TickReq{Who: who})
	for _, o := range deal.Closes {
		if o.Note.Kind == Judgment && o.Note.Type == NNoRoute {
			close(o, "rechecked: the tier's no-route condition no longer holds")
		}
	}
	// The obsolete judgment takes its own overdue marker with it, never a
	// different subject of a grouped judgment that still holds.
	closedSubjects := map[string]bool{}
	for _, o := range p.Closes {
		if o.Note.Kind == Judgment {
			closedSubjects[OpenKey(o.Note.ID, o.Subject())] = true
		}
	}
	for _, o := range s.Acked {
		if o.Note.Type == NOverdue && closedSubjects[OpenKey(o.Note.What, o.Subject())] {
			close(o, "rechecked: the underlying judgment no longer holds")
		}
	}
	seen := map[string]bool{}
	for _, o := range p.Closes {
		if o.Note.Kind == Judgment && !seen[o.Key] {
			seen[o.Key] = true
			p.Said = append(p.Said, fmt.Sprintf("closed judgment %s on %s: %s", o.Note.ID, o.Subject(), o.Note.Type))
		}
	}
	return p
}
