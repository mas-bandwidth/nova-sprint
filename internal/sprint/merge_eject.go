package sprint

import (
	"fmt"
	"strings"
	"time"
)

// The lander's eject (docs/SPEC-SPRINT.md section 7, the land batch; tla/Land.tla, THE
// EJECT; the owner, 2026-10-05, on one bad card that held a batch of 16 for minutes: "true,
// unless the cards after DEPEND on the card that is in front of the merge..."). A card of a
// batch that cannot merge (a git failure of its own head, a conflict, the lander's checks,
// the tree gate) is ejected: back to review with its reason, and every card of the batch
// whose needs reach it, directly or through another ejected card, goes with it, each naming
// the card it waited on. The rest of the batch lands in the same run, and the stream stops
// only when a card that is not ejected cannot land (red, rejected).

// NMergeStuck is the judgment that a stream's batch has landed nothing for MergeStuckAfter
// while it holds cards: the lander's refusals said why, and this says it once.
const NMergeStuck = "merge stuck: a batch has landed nothing while it holds cards"

// MergeStuckAfter is how long a batch may land nothing, refused on every run, before the
// coordinator is told (the owner, 2026-10-05: "It still needs judgement and notification to
// you, so you see what is going on, and when something is stuck").
const MergeStuckAfter = 15 * time.Minute

// EjectBriefWrong is the ejects of one card for one cause that the brief is wrong at: the
// third is not an eject's judgment but NBriefWrong ("a card ejected twice for the same
// reason raises the brief is wrong instead of a third eject"). The card still leaves the
// queue: it cannot land.
const EjectBriefWrong = 3

// The eject's fields. On a primary: the cause of its last eject for its own head and how
// many such ejects in a row it has had for that cause (counted across attempts: a rework
// that fails the same way again is the brief's fault, not the worker's), and the card it
// waited on when it went out with another. On a stream's control card: when its lander was
// first refused with cards queued, and why, since the last landing or eject; the cards of
// the batch it refused then, and when it last refused (the stall's count is that batch's,
// and only while the refusals go on).
const (
	FieldEjectCause    = "eject_cause"
	FieldEjectCount    = "eject_count"
	FieldEjectWaited   = "eject_waited"
	FieldLandIdle      = "land_idle_since"
	FieldLandIdleWhy   = "land_idle_why"
	FieldLandIdleCards = "land_idle_cards"
	FieldLandIdleLast  = "land_idle_last"
)

var landIdle = []string{FieldLandIdle, FieldLandIdleWhy, FieldLandIdleCards, FieldLandIdleLast}

// landIdleTouch is how stale the last refusal's stamp may grow before a refusal writes it
// again: the stall is told apart from a gap in it (MergeStuckAfter without a refusal) without
// a write to the control card on every refusal.
const landIdleTouch = MergeStuckAfter / 3

// mergeStuckDecisions is merge stuck's decisions, carried on the note: look at the reason,
// return a card that holds the batch, or drop it.
var mergeStuckDecisions = []string{"look", "return", "drop"}

// EjectPin is one card the lander ejects from its batch: its reason (Why, the lander's
// words), the cause's class (EjectCauseOf; "" for a card ejected with another), and the
// card it needs that was ejected (Waits, "" for a card ejected for its own head).
type EjectPin struct {
	ID    string
	Why   string
	Cause string `json:",omitempty"`
	Waits string `json:",omitempty"`
}

// EjectCauseOf is the class of a lander's reason for one card, the "same reason" an eject
// counts: the head in the text changes with every attempt, its class does not.
func EjectCauseOf(why string) string {
	for _, c := range []struct{ word, cause string }{
		{"is not a commit id", "not-commit"},
		{"is missing", "missing"},
		{"does not merge", "conflict"},
		{"git refused to merge it", "refused"},
		{"fails the lander's checks", "checks"},
		{"fails the tree gate", "gate"},
	} {
		if strings.Contains(why, c.word) {
			return c.cause
		}
	}
	return "other"
}

// LandNeeds is a primary's needs as the eject reads them: those it names and did not waive.
func LandNeeds(pr *Card) []string {
	if pr == nil {
		return nil
	}
	return without(Split(pr.F("needs")), Split(pr.F("waived")))
}

// ejectStep takes the ejected cards (the batch, by name: r.Cards) off the stream's queue
// and back to review, each with its reason as the finding (return_reason), returned at its
// attempt as a return marks it (the pump does not take it back on its standing reads), and
// raises one judgment naming the card that did not merge, its reason, the cards ejected
// with it and what landed (r.EjectLanded). A card ejected EjectBriefWrong times in a row
// for one cause raises NBriefWrong instead. The stream is not stopped.
func ejectStep(p Plan, s *Snapshot, ctl *Card, r MergeReq, batch []*Card) Plan {
	pins := map[string]EjectPin{}
	for _, e := range r.Eject {
		pins[e.ID] = e
	}
	leaving := map[string]bool{}
	var roots, with, withIDs, wrong []string
	reason := ""
	acceptAny := false
	for _, m := range batch {
		e, ok := pins[m.ID]
		pr := s.Work.Placed(m.ID)
		if !ok || pr == nil || pr.Col != Merging {
			p.refuse(m.ID, "not merging in stream "+r.Stream+" ("+placeWord(orEmpty(pr, m.ID))+"); nothing was ejected")
			continue
		}
		set := map[string]string{"returns": itoa(pr.Int("returns") + 1), FieldReturnedAttempt: itoa(pr.Int("attempt")),
			"return_reason": cutText("ejected by land: "+e.Why, MaxCardTextBytes)}
		var unset []string
		if e.Waits == "" {
			n := 1
			if pr.F(FieldEjectCause) == e.Cause {
				n = pr.Int(FieldEjectCount) + 1
			}
			set[FieldEjectCause], set[FieldEjectCount] = e.Cause, itoa(n)
			unset = append(unset, FieldEjectWaited)
			roots = append(roots, m.ID)
			if reason == "" {
				reason = m.ID + ": " + e.Why
			}
			if n >= EjectBriefWrong {
				wrong = append(wrong, m.ID)
			}
		} else {
			set[FieldEjectWaited] = e.Waits
			with, withIDs = append(with, m.ID+" (needs "+e.Waits+")"), append(withIDs, m.ID)
		}
		acceptAny = acceptAny || acceptable(s, pr)
		u := Unit{Key: m.ID, Stream: r.Stream, Changes: []Change{
			change(Merge, moveEntry(m, r.Stream, Returned, nil, "need_card", "need_stream")),
			change(Work, moveEntry(pr, r.Stream, Review, set, unset...))},
			Closes: closesFor(s.Open, append(append([]string(nil), ReturnResolves...), NMergeStuck), m.ID),
			Moved:  fmt.Sprintf("%s merging -> review (ejected by land: %s)", m.ID, e.Why)}
		leaving[m.ID] = true
		p.Units = append(p.Units, u)
	}
	if len(p.Refused) > 0 || len(p.Units) == 0 {
		p.Units = nil
		return p
	}
	// the stream made progress: its idle count starts again
	if len(unsetPresent(ctl, landIdle)) > 0 {
		p.Units[0].Changes = append(p.Units[0].Changes, change(Merge, setEntry(ctl, nil, landIdle...)))
	}
	landed := "nothing landed"
	if len(r.EjectLanded) > 0 {
		landed = "landed: " + Preview(r.EjectLanded, ", ")
	}
	what := "ejected " + reason
	if reason == "" {
		what = "ejected"
	}
	if len(with) > 0 {
		what += "; ejected with it: " + Preview(with, ", ")
	}
	what += "; " + landed
	last := &p.Units[len(p.Units)-1]
	if len(wrong) > 0 {
		// the third eject for one cause: the brief is wrong, not the attempt
		j := judgment(NBriefWrong, r.Stream, s.Now, 0, wrong...)
		j.Who, j.Card = r.Who, wrong[0]
		j.What = cutText(fmt.Sprintf("ejected %d times in a row for one cause (%s), the brief is wrong: %s", EjectBriefWrong, pins[wrong[0]].Cause, what), MaxCardTextBytes)
		last.Notes = append(last.Notes, j)
	}
	var named []string
	for _, id := range append(append([]string(nil), roots...), withIDs...) {
		if !contains(wrong, id) {
			named = append(named, id)
		}
	}
	if len(named) > 0 {
		j := judgment(NReturned, r.Stream, s.Now, 0, named...)
		j.Who, j.What = r.Who, cutText(what, MaxCardTextBytes)
		if len(roots) > 0 {
			j.Card = roots[0]
		}
		if !acceptAny {
			j.Decisions = removeDecision(j.Decisions, "accept")
		}
		last.Notes = append(last.Notes, j)
	}
	settle(&p, s, r.Who, leaving, nil)
	return p
}

// idleStep counts a lander's refusal of a stream that holds cards (r.Idle, its reason): the
// refusal that begins a stall is stamped on the control card with its batch (r.Cards);
// MergeStuckAfter or more after it, with nothing landed or ejected since, the judgment
// NMergeStuck is raised once on the batch's cards, naming the reason and how long. A landing,
// an eject, a stop and a resume clear the stamp. The count is the stall's, never a stale
// stamp's: a refusal that finds the stamped batch gone (no card of it in this batch) or no
// refusal for MergeStuckAfter before it (the queue was emptied by hand, a return or a drop,
// and filled again later) begins the stall again, stamped, with no judgment.
func idleStep(p Plan, s *Snapshot, ctl *Card, r MergeReq) Plan {
	since, err := time.Parse(time.RFC3339, ctl.F(FieldLandIdle))
	last, lerr := time.Parse(time.RFC3339, ctl.F(FieldLandIdleLast))
	if lerr != nil {
		last = since
	}
	if err != nil || s.Now.Sub(last) > MergeStuckAfter || !sharesCard(Split(ctl.F(FieldLandIdleCards)), r.Cards) {
		set := map[string]string{FieldLandIdle: stamp(s.Now), FieldLandIdleLast: stamp(s.Now), FieldLandIdleWhy: cutText(r.Idle, MaxCardTextBytes),
			FieldLandIdleCards: cutText(strings.Join(r.Cards, ","), MaxCardTextBytes)}
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: r.Stream, Changes: []Change{change(Merge, setEntry(ctl, set))},
			Moved: "stream " + r.Stream + ": the lander refused its batch; nothing landed since " + stamp(s.Now)})
		return p
	}
	waited := s.Now.Sub(since)
	again := func() Plan {
		if s.Now.Sub(last) >= landIdleTouch {
			p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: r.Stream, Changes: []Change{change(Merge, setEntry(ctl, map[string]string{FieldLandIdleLast: stamp(s.Now)}))},
				Moved: "stream " + r.Stream + ": the lander refused its batch again; nothing landed since " + stamp(since)})
		}
		return p
	}
	if waited < MergeStuckAfter || len(r.Cards) == 0 {
		return again()
	}
	for _, o := range s.Open {
		if o.Note.Type == NMergeStuck && o.Note.Stream == r.Stream {
			return again() // said once
		}
	}
	j := judgment(NMergeStuck, r.Stream, s.Now, 0, r.Cards...)
	j.Who, j.Card, j.Decisions = r.Who, r.Cards[0], append([]string(nil), mergeStuckDecisions...)
	j.What = cutText(fmt.Sprintf("the batch %s of stream %s has landed nothing for %s (since %s): %s", span(r.Cards), r.Stream,
		waited.Truncate(time.Second), since.UTC().Format("15:04:05 MST"), r.Idle), MaxCardTextBytes)
	p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: r.Stream, Changes: []Change{change(Merge, setEntry(ctl, map[string]string{FieldLandIdleLast: stamp(s.Now)}))}, Notes: []Note{j},
		Moved: "stream " + r.Stream + ": merge stuck for " + waited.Truncate(time.Second).String()})
	return p
}

// sharesCard is whether two batches hold a card in common: the stamped stall's batch and the
// batch refused now are one stall while any card of it is still there.
func sharesCard(a, b []string) bool {
	for _, id := range b {
		if contains(a, id) {
			return true
		}
	}
	return false
}
