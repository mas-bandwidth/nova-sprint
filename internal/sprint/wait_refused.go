package sprint

import (
	"fmt"
	"slices"
	"strings"
)

// A wait never parks a card (docs/SPEC-SPRINT.md section 8, "Judgments that hold a card";
// tla/SprintRules.tla, Part "answers": FailedWithAFindingNeverParked). On 2026-10-11, for about
// two hours, the seat answered "work came back failed" and "a reader found it broken" with
// `wait <id> --for 60m`, again and again, and parked bound and brief-defect judgments for
// hours the same day: those judgments hold their card until it is reworked, briefed, accepted
// or dropped, so about 22 of 42 review cards froze while the fleet idled. The owner: "i would
// very much like it if this was not possible to happen again." A judgment that holds a card
// from progressing takes no wait: the tick answers the mechanical ones (rules.go, ruleFailed
// and ruleReadBroken rework below the bound with the finding or the report as the fix), and
// the rest are reworked, briefed, accepted, released or dropped. A wait stays the answer to
// the informational kinds (an alarm, a status, a lateness the tick re-raises, CI's word).

// WaitRefused is every judgment kind that holds a card from progressing, with what to run
// instead of a wait: the one table. `wait` checks a kind against it by name (WaitRefusal),
// CardsHeldByJudgments counts the cards its kinds hold, and no kind in it offers "wait" among
// its decisions (TestNoJudgmentOffersADecisionItsVerbRefuses).
var WaitRefused = map[string]string{
	// work back from a worker
	NWorkFailed: "rework it with its report as the fix (the tick does this itself below the bound: nova-sprint rework <card>), drop it, or brief it again",
	NReadBroken: "rework it with the finding as the fix (the tick does this itself below the bound: nova-sprint rework <card>), drop it, or brief it again",
	// in review with nothing outstanding
	NReadyToAccept:  "accept it, rework it, or drop it",
	NReadsExhausted: "ask another reader, rework it, or drop it",
	NStranded:       "ask, rework it, or drop it",
	// at its bound, or its brief wrong: a mind's, at once
	NBound:       "rework it on a higher tier, or drop it",
	NBriefWrong:  "brief it again, or drop it",
	NBriefDefect: "re-cut its brief, or drop it",
	// a read that cannot be asked, a base or a read branch gone: the card waits on it
	NMissingBase:       "rebase the card, or drop it",
	NReadBranchMissing: "rework it with a fix, drop it, or ack it once its branch is on origin",
	// a card the deal cannot place, or whose merge base is gone
	NNoRoute:   "add a route, look at the card, or drop it",
	NReadyOver: "look at the card and give the deal what it lacks, or drop it",
	NDeadBase:  "rebase or return the card, or drop it",
	// a friend's report, a repair the store refused
	NReportJudgment: "read the report and rework, accept or drop the card",
	NCIRed:          "rework the card, return it, or drop it; ack it when the red is not the card's",
	NRepairSkipped:  "look at the card and return, rework or drop it",
	// waiting on a card that will not come
	NBlocked:     "drop it, or ack it once its need is mended",
	NMissingNeed: "drop it, or ack it once its need is mended",
	// merge stopped: a card sent back from merging
	NReturned: "rework it, accept it, or drop it",
	// stream stopped: every card of the stream behind the stop is held
	NConflict: "resolve and resume the stream, rework the card, or drop it",
	NRed:      "return the suspect and resume, rework it, or drop it",
	NCross:    "rank the card it needs first, look at both, return it, or drop it",
	NRejected: "rework the card the merge queue rejected, or drop it, and resume",
	NBaseRed:  "mend the base (the base-gate rule resumes the stream once its tip is green), or resume",
	// the fleet's failure: every card failing that way reworks itself to its bound meanwhile
	NFleetFailure: "mend its cause (the toolchain, a member, a route or a provider) and ack it; the cards rework themselves to their bound meanwhile",
	// a harness fault repeated: the second identical one on a card, or on many cards, holds
	// the card until the cause is mended and it is rebriefed or dropped (harness_fault.go)
	NHarnessFault: "mend the cause (the toolchain, a route or a provider), then rebrief it, or drop it",
	// the seat's silence: the judgments holding cards past their deadline wait on the seat,
	// and it clears only when each is answered (seat_answers.go, SeatSilentDecision)
	NSeatSilent: "answer each judgment it names: nova-sprint inbox",
}

// WaitAllowedOnACard is the kinds a rework discharges on its primary (ReworkResolves) that
// still take a wait, each with why: the exceptions TestEveryKindThatHoldsACardRefusesWait
// reads, kept as few as the code allows.
var WaitAllowedOnACard = map[string]string{
	NCannotAsk: "the primary awaits a reader rather than a coordinator action: wait quiets the reader condition until its review time, and eligibility still closes it",
	// a stall that names a card holds it and is refused (WaitRefusedWhenItNamesACard)
	NStalled: "a stall that names no card on the work table is a report about a stream or the fleet with nothing held; one that names a card refuses wait",
	// lateness: the card keeps working (or being read) under its holder, and the late rules
	// answer it (rules.go ruleLate, judgment_rules.go read-late); a wait holds the alarm, not the card
	NWorkLate: "the card is still working under its holder: the late rule waits, returns or redeals it; a wait quiets the lateness alarm, not the card",
	NReadLate: "the read is still with its reader: the read-late rule asks another; a wait quiets the lateness alarm, not the card",
	// the held wave's sentinel, never a card: a stop the coordinator placed
	NStarving: "names the first held wave's sentinel, a stop the coordinator placed, never a single card",
	// a friend's session: the judgment names a friend, not a card
	NFriendIdle:    "names a friend whose session holds working cards and finishes none, not a card: the cards stay on that friend's row and keep their deadlines",
	NFriendStalled: "names a friend, not a card: the cards keep their deadlines and the stall ladder wakes the friend",
	// a deliberate stop: "do more before going on" is the sentinel's own decision
	NSentinelReached: "a sentinel is a stop the coordinator placed: doing more before going on is its own decision, and its release is a gated cut",
}

// WaitRefusedWhenItNamesACard is the kinds that refuse wait only when the judgment names a
// card (its primaries): a stall holds the card it names in place until it is answered, while a
// stall about a stream or the fleet holds nothing.
var WaitRefusedWhenItNamesACard = map[string]string{
	NStalled: "look at the card and rework, return or drop it",
}

// WaitRefusedKinds is WaitRefused's kinds in name order.
func WaitRefusedKinds() []string {
	var out []string
	for k := range WaitRefused {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// WaitRefusal is why wait is refused on the judgment n: it holds a card from progressing,
// and what to run instead; "" when a wait may hold it (an informational kind).
//
// tla/NeedsBlocked.tla BlockedDependentsCovered
func WaitRefusal(n Note) string {
	if n.Type == NNeedBlocked {
		return fmt.Sprintf("%s (%s) cannot be waited: decide the blocker (rework it on a higher tier, or drop it), or remove its needs; run: nova-sprint inbox --open %s", n.ID, n.Type, n.ID)
	}
	instead, ok := WaitRefused[n.Type]
	if c, cardOnly := WaitRefusedWhenItNamesACard[n.Type]; !ok && cardOnly && !n.StreamLevel && !n.SprintLevel && len(n.Primaries) > 0 {
		instead, ok = c, true
	}
	if !ok || n.Kind != Judgment {
		return ""
	}
	return fmt.Sprintf("%s (%s) holds its card from progressing, and a wait would park it: %s; run: nova-sprint inbox --open %s", n.ID, n.Type, instead, n.ID)
}

// WaitRefusalIn is WaitRefusal read against the sprint: besides the kinds of the table, any
// judgment whose subject or primaries are cards on the work table refuses wait, whatever its
// kind (a kind added later included, a stop that names a card), unless its kind is one of
// WaitAllowedOnACard. A judgment naming a friend, a timer, a tier or a stream holds no card.
func WaitRefusalIn(s *Snapshot, n Note) string {
	if s == nil || s.Work == nil {
		return WaitRefusalOn(n, nil)
	}
	return WaitRefusalOn(n, func(id string) bool { return s.Work.Placed(id) != nil })
}

// WaitRefusalOn is WaitRefusalIn with the work table's membership given as onWork (nil: the
// table alone): the one rule the wait verb and the inbox's printed decisions both apply.
func WaitRefusalOn(n Note, onWork func(id string) bool) string {
	if why := WaitRefusal(n); why != "" {
		return why
	}
	if _, ok := WaitAllowedOnACard[n.Type]; ok || n.Kind != Judgment || onWork == nil || n.StreamLevel || n.SprintLevel {
		return ""
	}
	for _, sub := range n.Primaries {
		if onWork(sub) {
			return fmt.Sprintf("%s (%s) names %s, a card it holds from progressing, and a wait would park it: answer it by its decisions; run: nova-sprint inbox --open %s", n.ID, n.Type, sub, n.ID)
		}
	}
	return ""
}

// WaitRefusedAction is the action WaitRefused names in place of a wait on the judgment n: the
// one table's text for the kind, or "" when the kind takes a wait. A kind of
// WaitRefusedWhenItNamesACard names it only when the judgment names a card.
//
// tla/NeedsBlocked.tla BlockedDependentsCovered
func WaitRefusedAction(n Note) string {
	if n.Kind != Judgment {
		return ""
	}
	if n.Type == NNeedBlocked {
		return "decide the blocker (rework it on a higher tier, or drop it), or remove its needs"
	}
	if instead, ok := WaitRefused[n.Type]; ok {
		return instead
	}
	if c, cardOnly := WaitRefusedWhenItNamesACard[n.Type]; cardOnly && !n.StreamLevel && !n.SprintLevel && len(n.Primaries) > 0 {
		return c
	}
	return ""
}

// OffersRefusedVerb is the decisions of the kind that name a verb the kind refuses: "wait"
// on a kind of WaitRefused. The inbox never offers one (notes.go Decisions, steps_tick.go
// TickDecisions).
func OffersRefusedVerb(kind string, decisions []string) []string {
	if _, ok := WaitRefused[kind]; !ok {
		return nil
	}
	var out []string
	for _, d := range decisions {
		if d == "wait" || strings.HasPrefix(d, "wait ") {
			out = append(out, d)
		}
	}
	return out
}

// CardsHeldByJudgments is the work cards an open judgment holds from progressing now, each
// once: the cards on the work table an open judgment of a WaitRefused kind names, and every
// card a stopped stream holds from landing (its cards in review and merging) under a
// stream-level one (a stream stopped: Stella's read of 2026-10-11, "stopped streams count zero
// held cards").
func CardsHeldByJudgments(s *Snapshot) int {
	if s.Work == nil {
		return 0
	}
	seen := map[string]bool{}
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment || WaitRefusal(n) == "" {
			continue
		}
		// an open entry is one active subject: a card-level entry holds its own card only (its
		// note may still name cards answered since; Stella's read of 2026-10-11)
		if sub := o.Subject(); !n.StreamLevel && s.Work.Placed(sub) != nil {
			seen[sub] = true
		}
		if n.StreamLevel && n.Stream != "" {
			for _, c := range s.Work.Column(Review, Merging) {
				if c.Row == n.Stream {
					seen[c.ID] = true
				}
			}
		}
	}
	return len(seen)
}
