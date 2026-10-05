package sprint

import (
	"fmt"
	"strings"
	"time"
)

// The lander's eject (docs/SPEC-SPRINT.md section 7, land-ejects-a-bad-card-and-its-dependents;
// tla/Land.tla, THE EJECT; the owner, 2026-10-05: "true, unless the cards after DEPEND on the
// card that is in front of the merge..."). A card of a landing batch that cannot be merged (a
// git failure of its own head, a conflict, the lander's checks, the tree gate) no longer stops
// its stream: it goes back to review with the reason as its finding, every later card of the
// batch whose needs reach it goes back with it, each naming the card it waited on, and the rest
// of the batch lands in the same merge step (MergeReq.Ejects). The card's returned judgment is
// the eject's one judgment to the coordinator: the card, its reason, the dependents ejected with
// it and what landed. A card ejected twice the same way is not ejected a third time: the
// lander stops the stream on it as a conflict, with the judgment "the brief is wrong"
// (MergeReq.BriefWrong). A stream whose landings land nothing while it holds cards raises "merge
// stuck" once MergeStuckAfter has run (MergeReq.Idle).

// Eject is one card a landing returns to review instead of landing it.
type Eject struct {
	ID string
	// Why is the lander's words: the head's own failure, or for a dependent the card it
	// waited on and why that one was ejected.
	Why string
	// Way is how the head failed, the eject's count of the same reason (FieldEjectWay):
	// RefusedConflict, RefusedPaths, RefusedChecks, RefusedGate, EjectedGit or EjectedNeeds.
	Way string
	// WaitedOn is, for a dependent, the ejected card whose needs it reaches; "" for the card
	// whose own head failed.
	WaitedOn string `json:",omitempty"`
	// Head and Attempt are the head and attempt the lander pinned (the report's guard).
	Head    string `json:",omitempty"`
	Attempt string `json:",omitempty"`
}

// The eject's ways besides the conflict rule's (RefusedConflict, RefusedPaths, RefusedChecks,
// RefusedGate).
const (
	EjectedGit   = "git"   // git refused the head itself: unrelated history, a missing object or ref
	EjectedNeeds = "needs" // a dependent: it needs a card the same landing ejected
)

// The eject's fields: on the primary, the way of its last eject and how many ejects in a row
// were that way; on the stream's control card, since when its landings have landed nothing.
const (
	FieldEjectWay  = "eject_way"
	FieldEjects    = "ejects"
	FieldLandIdle  = "land_idle_since"
	FieldLandIdleW = "land_idle_why"
)

// EjectsBeforeBriefWrong is the ejects of one card the same way that make the next one "the
// brief is wrong" instead of a third eject.
const EjectsBeforeBriefWrong = 2

// MergeStuckAfter is how long a stream's landings may land nothing while it holds cards before
// the judgment NMergeStuck is raised.
const MergeStuckAfter = 15 * time.Minute

// NMergeStuck is the judgment of a stream whose landings have landed nothing for
// MergeStuckAfter while it held cards; a landing closes it. Its decisions ride on the note
// (MergeStuckDecisions): look, then wait.
const NMergeStuck = "merge stuck"

// MergeStuckDecisions are the merge-stuck judgment's decisions.
var MergeStuckDecisions = []string{"look", "wait"}

// BriefWrongNext says a card's next eject the way given would be its third the same way: the
// lander stops the stream on it with the judgment "the brief is wrong" instead.
func BriefWrongNext(pr *Card, way string) bool {
	return pr != nil && way != EjectedNeeds && pr.F(FieldEjectWay) == way && pr.Int(FieldEjects) >= EjectsBeforeBriefWrong
}

// ejectRefusals is why the ejects cannot be recorded: each card must be queued in the stream
// and merging, named once.
func ejectRefusals(s *Snapshot, stream string, ejects []Eject) []Refusal {
	var out []Refusal
	seen := map[string]bool{}
	for _, e := range ejects {
		pr, m := s.Work.Placed(e.ID), s.Merge.Placed(e.ID)
		switch {
		case seen[e.ID]:
			out = append(out, Refusal{e.ID, "ejected twice in one landing"})
		case pr == nil || pr.Col != Merging || m == nil || m.Row != stream || m.Col != Queued:
			out = append(out, Refusal{e.ID, "not queued in stream " + stream + " to eject (it is " + placeWord(orEmpty(pr, e.ID)) + ")"})
		}
		seen[e.ID] = true
	}
	return out
}

// ejectSaid is the eject's judgment text for the card whose own head failed: its reason, the
// dependents ejected with it, and what landed.
func ejectSaid(e Eject, ejects []Eject, landed []string) string {
	var deps []string
	for _, d := range ejects {
		if d.WaitedOn != "" && rootOf(d, ejects) == e.ID {
			deps = append(deps, d.ID+" (waited on "+d.WaitedOn+")")
		}
	}
	with := "none"
	if len(deps) > 0 {
		with = strings.Join(deps, ", ")
	}
	lands := "nothing"
	if len(landed) > 0 {
		lands = strings.Join(landed, ", ")
	}
	return fmt.Sprintf("land ejected %s: %s; ejected with it: %s; landed: %s", e.ID, e.Why, with, lands)
}

// rootOf is the card whose own head failed that a dependent waits on, through other dependents.
func rootOf(e Eject, ejects []Eject) string {
	for seen := 0; e.WaitedOn != "" && seen <= len(ejects); seen++ {
		next := e
		for _, x := range ejects {
			if x.ID == e.WaitedOn {
				next = x
			}
		}
		if next.ID == e.ID {
			return e.WaitedOn // the card waited on is not in this eject: it is the root
		}
		e = next
	}
	return e.ID
}

// ejectUnits returns each ejected card to review as a return does (Return), its merge card
// to the returned column, marked to be redone on the tip at this attempt (the conflict rule's
// FieldRuleRedo, so with the rules on it is reworked as a conflict card is), with the reason
// as its finding (FieldRuleRefusal) and the eject's count (FieldEjectWay, FieldEjects). Each
// card's returned judgment says why; the failed card's is the eject's judgment (ejectSaid).
func ejectUnits(s *Snapshot, r MergeReq, landed []string) []Unit {
	var out []Unit
	for _, e := range r.Ejects {
		pr, m := s.Work.Placed(e.ID), s.Merge.Placed(e.ID)
		n := 1
		if pr.F(FieldEjectWay) == e.Way {
			n = pr.Int(FieldEjects) + 1
		}
		what := ejectSaid(e, r.Ejects, landed)
		if e.WaitedOn != "" {
			what = "land ejected " + e.ID + " with " + e.WaitedOn + ": " + e.Why
		}
		what = cutText(what, MaxCardTextBytes)
		set := map[string]string{"returns": itoa(pr.Int("returns") + 1), FieldReturnedAttempt: itoa(pr.Int("attempt")),
			"return_reason": what, FieldRuleRedo: pr.F("attempt"), FieldRuleRefusal: cutText(e.Why, MaxCardTextBytes),
			FieldRuleAnswer: RuleConflict + ": ejected by the lander to be redone on the tip at " + stamp(s.Now),
			FieldEjectWay:   e.Way, FieldEjects: itoa(n)}
		if e.Way != EjectedNeeds && e.Way != EjectedGit {
			set[FieldRuleRefused] = e.Way
		}
		u := Unit{Key: e.ID, Stream: r.Stream, Changes: []Change{
			change(Merge, moveEntry(m, r.Stream, Returned, nil, "need_card", "need_stream")),
			change(Work, moveEntry(pr, r.Stream, Review, set))},
			Closes: closesFor(s.Open, ReturnResolves, e.ID),
			Moved:  fmt.Sprintf("%s merging -> review (ejected: %s)", e.ID, oneLine(e.Why))}
		j := judgment(NReturned, r.Stream, s.Now, pr.Int("returns"), e.ID)
		j.Who, j.Attempt, j.What, j.Card = r.Who, pr.Int("attempt"), what, e.ID
		if e.WaitedOn != "" {
			j.Other = e.WaitedOn
		}
		if !acceptable(s, pr) {
			j.Decisions = removeDecision(j.Decisions, "accept")
		}
		u.Notes = append(u.Notes, j)
		out = append(out, u)
	}
	return out
}

// IsEjectMoved says a merge step's moved line is an eject's, not a landing's.
func IsEjectMoved(line string) bool {
	_, rest, _ := strings.Cut(line, " ")
	return strings.HasPrefix(rest, "merging -> review (ejected")
}

// ejectOnly is the merge step that ejects and lands nothing: the stream goes on, waiting when
// nothing is left queued or stuck.
func ejectOnly(p Plan, s *Snapshot, ctl *Card, r MergeReq) Plan {
	if why := ejectRefusals(s, r.Stream, r.Ejects); len(why) > 0 {
		p.Refused = append(p.Refused, why...)
		return p
	}
	units := ejectUnits(s, r, nil)
	if ctl.F("state") == StreamMerging && s.Merge.Count(r.Stream, Queued)+s.Merge.Count(r.Stream, Stuck) == len(r.Ejects) {
		units[0].Changes = append(units[0].Changes, change(Merge, setEntry(ctl, map[string]string{"state": StreamWaiting, "since": stamp(s.Now)})))
	}
	p.Units = append(p.Units, units...)
	return p
}

// idleStep records that a landing of the stream landed nothing while it held cards: the first
// time, since when; once MergeStuckAfter has run since then and no merge-stuck judgment is open
// on the stream, the judgment NMergeStuck with the reason. Nothing else changes.
func idleStep(p Plan, s *Snapshot, ctl *Card, r MergeReq) Plan {
	since := ctl.F(FieldLandIdle)
	if since == "" {
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: r.Stream, Changes: []Change{change(Merge, setEntry(ctl, map[string]string{FieldLandIdle: stamp(s.Now), FieldLandIdleW: cutText(r.Idle, MaxCardTextBytes)}))},
			Moved: "stream " + r.Stream + ": a landing landed nothing (" + oneLine(r.Idle) + ")"})
		return p
	}
	at, err := time.Parse(time.RFC3339, since)
	if err != nil || s.Now.Sub(at) < MergeStuckAfter {
		return p
	}
	for _, o := range s.Open {
		if o.Note.Type == NMergeStuck && o.Note.Stream == r.Stream {
			return p
		}
	}
	j := judgment(NMergeStuck, r.Stream, s.Now, 0)
	j.StreamLevel, j.Who, j.Decisions = true, r.Who, append([]string(nil), MergeStuckDecisions...)
	j.What = cutText(fmt.Sprintf("stream %s has landed nothing since %s (%s) while it holds cards: %s", r.Stream, at.UTC().Format("15:04:05 MST"),
		s.Now.Sub(at).Round(time.Second), r.Idle), MaxCardTextBytes)
	p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: r.Stream, Changes: []Change{change(Merge, setEntry(ctl, map[string]string{FieldLandIdleW: cutText(r.Idle, MaxCardTextBytes)}))},
		Notes: []Note{j}, Moved: "stream " + r.Stream + ": merge stuck"})
	return p
}

// mergeStuckCloses is the open merge-stuck judgments of the stream, which a landing closes.
func mergeStuckCloses(s *Snapshot, stream string) []Open {
	var out []Open
	for _, o := range s.Open {
		if o.Note.Type == NMergeStuck && o.Note.Stream == stream {
			out = append(out, o)
		}
	}
	return out
}

// briefWrongNote is the judgment a conflict stop carries when the card would be ejected a third
// time the same way.
func briefWrongNote(s *Snapshot, r MergeReq, pr *Card) Note {
	j := judgment(NBriefWrong, r.Stream, s.Now, 0, r.Conflict)
	j.Who, j.Card = r.Who, r.Conflict
	if pr != nil {
		j.Attempt = pr.Int("attempt")
	}
	j.What = cutText(r.BriefWrong, MaxCardTextBytes)
	return j
}
