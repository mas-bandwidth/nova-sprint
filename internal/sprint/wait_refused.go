package sprint

import (
	"fmt"
	"slices"
)

// A wait never parks a card (docs/SPEC-SPRINT.md section 8, "Judgments that hold a card";
// tla/SprintRules.tla, Part "answers": FailedWithAFindingNeverParked). On 2026-10-11, for about
// two hours, the seat answered "work came back failed" and "a reader found it broken" with
// `wait <id> --for 60m`, again and again: those judgments hold their card in review until it
// is reworked or dropped, so about 22 of 42 review cards froze while the fleet idled. The
// owner: "i would very much like it if this was not possible to happen again." A judgment
// that holds a card from progressing takes no wait: the tick answers the mechanical ones
// (rules.go, ruleFailed and ruleReadBroken rework below the bound with the finding or the
// report as the fix), and the rest are reworked, dropped or briefed. A wait stays the answer
// to the informational kinds (an alarm, a status, a lateness the tick re-raises).

// WaitRefused is every judgment kind that holds a card from progressing, with what to run
// instead of a wait: the one table `wait` checks a kind against, by name.
var WaitRefused = map[string]string{
	// failed work: the failed rule reworks it with its report below its bound
	NWorkFailed: "rework it with its report as the fix (the tick does this itself below the bound: nova-sprint rework <card>), drop it, or brief it again",
	// broken read: the read-broken rule reworks it with the finding below its bound
	NReadBroken: "rework it with the finding as the fix (the tick does this itself below the bound: nova-sprint rework <card>), drop it, or brief it again",
	// merge stopped: a card sent back from merging
	NReturned: "rework it, accept it, or drop it",
	// stream stopped: every card of the stream behind the stop is held
	NConflict: "resolve and resume the stream, rework the card, or drop it",
	NRed:      "return the suspect and resume, rework it, or drop it",
	NCross:    "land the card it needs first and resume, or drop it",
	NRejected: "rework the card the merge queue rejected, or drop it, and resume",
	NBaseRed:  "mend the base (the base-gate rule resumes the stream once its tip is green), or drop the cards",
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

// CardsHeldByJudgments is the work cards an open judgment holds from progressing now: the
// subjects of the open judgments of a WaitRefused kind, and of the bound's (NBound, NBriefWrong),
// that are on the work table, each once.
func CardsHeldByJudgments(s *Snapshot) int {
	seen := map[string]bool{}
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment {
			continue
		}
		if _, ok := WaitRefused[n.Type]; !ok && n.Type != NBound && n.Type != NBriefWrong {
			continue
		}
		for _, sub := range n.Subjects() {
			if s.Work != nil && s.Work.Placed(sub) != nil {
				seen[sub] = true
			}
		}
	}
	return len(seen)
}
