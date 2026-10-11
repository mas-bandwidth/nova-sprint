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
	// a friend's report, a repair the store refused
	NReportJudgment: "read the report and rework, accept or drop the card",
	NRepairSkipped:  "look at the card and return, rework or drop it",
	// waiting on a card that will not come
	NBlocked:     "drop it, or ack it once its need is mended",
	NMissingNeed: "drop it, or ack it once its need is mended",
	// merge stopped: a card sent back from merging
	NReturned: "rework it, accept it, or drop it",
	// stream stopped: every card of the stream behind the stop is held
	NConflict:        "resolve and resume the stream, rework the card, or drop it",
	NRed:             "return the suspect and resume, rework it, or drop it",
	NCross:           "rank the card it needs first, look at both, return it, or drop it",
	NRejected:        "rework the card the merge queue rejected, or drop it, and resume",
	NBaseRed:         "mend the base (the base-gate rule resumes the stream once its tip is green), or resume",
	NSentinelReached: "release it, do more before going on, or drop it",
	// the fleet's failure: every card failing that way reworks itself to its bound meanwhile
	NFleetFailure: "mend its cause (the toolchain, a member, a route or a provider) and ack it; the cards rework themselves to their bound meanwhile",
}

// WaitAllowedOnACard is the kinds a rework discharges on its primary (ReworkResolves) that
// still take a wait, each with why: the exceptions TestEveryKindThatHoldsACardRefusesWait
// reads, kept as few as the code allows.
var WaitAllowedOnACard = map[string]string{
	NCIRed:   "CI's word on a head: the card's own read or failed-work judgment holds it, and CI runs again on its own",
	NStalled: "the no-stall rule's judgment names its own subject (a card, a stream or the fleet) and re-raises it every pass",
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
func WaitRefusal(n Note) string {
	instead, ok := WaitRefused[n.Type]
	if !ok || n.Kind != Judgment {
		return ""
	}
	return fmt.Sprintf("%s (%s) holds its card from progressing, and a wait would park it: %s; run: nova-sprint inbox --open %s", n.ID, n.Type, instead, n.ID)
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
		if _, ok := WaitRefused[n.Type]; !ok || n.Kind != Judgment {
			continue
		}
		for _, sub := range append(n.Subjects(), n.Primaries...) {
			if s.Work.Placed(sub) != nil {
				seen[sub] = true
			}
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
