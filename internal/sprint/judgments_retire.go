package sprint

import (
	"fmt"
	"strings"
)

// A judgment retires with its card (docs/SPEC-SPRINT.md section 8, "A judgment
// retires with its card"): an open judgment whose every card has left the table
// (landed, dropped, or dropped replaced by its twin) is closed by the tick that
// sees it gone, answered by the machine with the card's event, so the inbox never
// shows a judgment about a card no longer on the table. A judgment on a primary
// retires on that primary; a tick-kept judgment of a stream-level condition that
// names cards (a tier no route serves, a starving fleet's sentinel) retires when
// every card it names has left, and the tick raises it again on the cards still
// waiting. A stream's own judgment (a stop, a red base) is its stream's, never a
// card's, and stays.
//
// A judgment is also outlived by its card's move (the 2026-10-12 j127: a brief
// defect still open after its card was re-briefed and dealt on to attempt 5):
// one raised at an attempt the card has since left, or on a brief the card has
// since replaced, closes the same way, answered by the card's move. The card's
// state, not the tick alone, is what closes it: one still at the attempt and
// brief it was raised for stays.

// RetiredPrefix opens the decided note of a judgment retired with its card.
const RetiredPrefix = "retired with its card: "

// MovedOnPrefix opens the decided note of a judgment its card has moved past:
// the card reached another attempt, or its brief was replaced, since the
// judgment was raised.
const MovedOnPrefix = "answered by the card's move: "

// retiresWithCards are the stream-level judgments the tick keeps on the cards
// they name: each retires once every card it names has left the table.
var retiresWithCards = []string{NNoRoute, NStarving}

// aboutTheLanding are the judgments about a landed card by design: its landing
// is their cause, not their end. NInvariant is the check's own, closed when its
// rule holds again.
var aboutTheLanding = []string{NScoredLow, NInvariant}

// offTable is the event a card left the table by: "<id> landed", "<id> dropped
// (<reason>)", "<id> replaced by <twin>"; "" while it is on the table, or when the
// work table never had it (a subject that is not a card).
func offTable(s *Snapshot, id string) string {
	c := s.Work.Card(id)
	switch {
	case c == nil:
		return ""
	case c.Placed() && c.Col == Landed:
		return id + " landed"
	case c.Placed():
		return ""
	}
	reason := c.F("reason")
	if strings.HasPrefix(reason, "replaced by ") {
		return id + " " + reason
	}
	if c.F("outcome") == "dropped" {
		if reason == "" {
			return id + " dropped"
		}
		return id + " dropped (" + reason + ")"
	}
	return id + " off the table (" + orDash(c.F("outcome")) + ")"
}

// movedOn is the event a judgment's card moved past it by: "<id> moved to attempt
// <m> (was <n>)", or "<id> was re-briefed" for a judgment about the brief itself;
// "" while the card is still the one the judgment was raised for, the judgment
// names no attempt, or the work table never had it (docs/SPEC-SPRINT.md section 8,
// "A judgment retires with its card"). A judgment records the
// attempt it was raised at (its Attempt), and the card's current attempt past it
// is the card moved on. A brief-defect judgment is outlived once the card no
// longer carries the defect (FieldBriefDefect, unset by the re-brief: briefInPlace),
// and a brief-bound judgment once the card's brief was replaced at or after the
// attempt it was raised at (FieldBriefAttempt). A reader's finding on a brief
// edited before it is not outlived by the edit (the same attempt's new work).
func movedOn(s *Snapshot, n Note, id string) string {
	c := s.Work.Placed(id)
	if c == nil || n.Attempt <= 0 {
		return ""
	}
	if now := c.Int("attempt"); now > n.Attempt {
		return fmt.Sprintf("%s moved to attempt %d (was %d)", id, now, n.Attempt)
	}
	switch n.Type {
	case NBriefDefect:
		if c.F(FieldBriefDefect) == "" {
			return id + " was re-briefed"
		}
	case NBriefWrong:
		if at := c.Int(FieldBriefAttempt); at > 0 && at >= n.Attempt {
			return fmt.Sprintf("%s was re-briefed at attempt %d", id, at)
		}
	}
	return ""
}

// outlived is the card's-move event when every subject of a judgment has left the
// table or moved past the state it was raised for (docs/SPEC-SPRINT.md section 8,
// "A judgment retires with its card"): the judgment holds no card that still
// needs a decision, so the coordinator may ack it (Ack, steps_ack.go), and the
// tick retires it (RetireJudgments).
func outlived(s *Snapshot, n Note, entries []Open) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	var events []string
	for _, o := range entries {
		e := offTable(s, o.Subject())
		if e == "" {
			e = movedOn(s, n, o.Subject())
		}
		if e == "" {
			return "", false
		}
		events = append(events, e)
	}
	return strings.Join(events, "; "), true
}

// RetireJudgments is the closes and the machine's answers of every open judgment
// whose cards have all left the table or moved past the state the judgment was
// raised for (docs/SPEC-SPRINT.md section 8, "A judgment retires with its card"),
// skipping those the plan closes already: one decided note a judgment, naming
// every card's event.
func RetireJudgments(s *Snapshot, p *Plan, who string) {
	if s.Work == nil {
		return
	}
	closing := map[string]bool{}
	for _, o := range p.Closes {
		closing[o.Key] = true
	}
	var order []string
	events := map[string][]string{}
	moved := map[string]bool{}
	prims := map[string][]string{}
	subject := map[string]Open{}
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment || closing[o.Key] || n.SprintLevel || contains(aboutTheLanding, n.Type) {
			continue
		}
		cards := []string{o.Subject()}
		if n.StreamLevel {
			if !contains(retiresWithCards, n.Type) || len(n.Primaries) == 0 {
				continue
			}
			cards = n.Primaries
		}
		var es []string
		didMove := false
		for _, id := range cards {
			e := offTable(s, id)
			if e == "" {
				if e = movedOn(s, n, id); e == "" {
					es = nil
					break
				}
				didMove = true
			}
			es = append(es, e)
		}
		if len(es) == 0 {
			continue
		}
		closing[o.Key] = true
		p.Closes = append(p.Closes, o)
		if _, seen := events[n.ID]; !seen {
			order = append(order, n.ID)
			subject[n.ID] = o
		}
		events[n.ID] = append(events[n.ID], es...)
		if didMove {
			moved[n.ID] = true
		}
		if !n.StreamLevel {
			prims[n.ID] = append(prims[n.ID], o.Subject())
		}
	}
	for _, id := range order {
		o := subject[id]
		prefix := RetiredPrefix
		if moved[id] {
			prefix = MovedOnPrefix
		}
		d := decided(o, prefix, who, s.Now, prims[id]...)
		d.What = prefix + strings.Join(events[id], "; ")
		p.Notes = append(p.Notes, d)
	}
}
