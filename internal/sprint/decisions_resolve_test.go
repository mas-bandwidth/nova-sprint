package sprint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// decisionsVerb is the verb a decision names when it is one of the five that resolve a
// judgment on a card by acting on it: rework, return, ask (and ask --another), drop and ack.
// "" for the others (accept, resume, brief, look, wait, ...), which name a step that is not
// a closing of the judgment on the card or whose answer is another table's.
func decisionsVerb(d string) string {
	switch {
	case d == "ask another reader" || d == "ask --another":
		return "ask --another"
	case d == "ask":
		return "ask"
	case d == "rework" || strings.HasPrefix(d, "rework "):
		return "rework"
	case d == "return":
		return "return"
	case d == "drop":
		return "drop"
	case d == "ack":
		return "ack"
	}
	return ""
}

// streamLevelTypes are the judgments about a stopped stream: a card verb of theirs records its
// answer and the judgment stays open while the stream is stopped (answerListed, plan.go), so
// they are resolved by the stream's own verb (resume), never by closing on a card.
var streamLevelTypes = map[string]bool{NConflict: true, NRed: true, NCross: true, NRejected: true, NBaseRed: true}

// notClosedByItsVerb are the rows whose verb is offered on purpose and does not close the
// judgment, each with why: the test reads them, and a new one is a decision.
var notClosedByItsVerb = map[string]string{
	NReadBroken + "|ask --another": "one more reader never overrules a broken verdict: the judgment stays open and the card goes back to work with the finding (Ask, steps_review.go; docs/SPEC-SPRINT.md section 8; TestG3ReadsExhaustedIsAJudgment)",
}

// TestEveryDecisionResolvesItsJudgment is the property of the whole decisions table
// (tla/ReadBranchMissing.tla, OfferedDecisionResolves), tested once for every row: every
// decision a judgment type lists (Decisions, TickDecisions) that names rework, return, ask,
// ask --another, drop or ack is a verb whose step closes that type. On 2026-10-15 the
// missing-branch judgment listed "rework with a fix" and the ask's plain closes, and neither
// closed it: a decision that names a verb which cannot resolve it fails here.
func TestEveryDecisionResolvesItsJudgment(t *testing.T) {
	t.Parallel()
	closes := map[string][]string{
		"rework":        ReworkResolves,
		"return":        ReturnResolves,
		"ask":           AskResolves,
		"ask --another": AskAnotherResolves,
	}
	all := map[string][]string{}
	for typ, ds := range Decisions {
		all[typ] = ds
	}
	for typ, ds := range TickDecisions {
		all[typ] = append(slices.Clone(all[typ]), ds...)
	}
	for typ, ds := range all {
		for _, d := range ds {
			verb := decisionsVerb(d)
			if verb == "" || streamLevelTypes[typ] {
				continue
			}
			if why, ok := notClosedByItsVerb[typ+"|"+verb]; ok {
				assert.NotEmpty(t, why)
				continue
			}
			if TickKept(typ) && verb != "ask" && verb != "rework" {
				// a condition the tick keeps closes when the condition clears (TickKept): the
				// verb changes the condition (the late read is asked of another reader)
				continue
			}
			switch verb {
			case "rework", "return", "ask", "ask --another":
				assert.Contains(t, closes[verb], typ, "%q: the decision %q names %s, whose step does not close it", typ, d, verb)
			case "drop":
				w := setup(t, 1)
				n := Note{ID: "n-x.1", Kind: Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, Decisions: ds, At: w.s.Now}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, "s1-1"), Note: n})
				p := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-1"}}, Reason: "decided", Answers: []string{n.ID}, Who: w.s.Coordinator})
				assert.Empty(t, p.Refused, "%q: the decision %q is refused: %v", typ, d, p.Refused)
				assert.True(t, slices.ContainsFunc(p.Units, func(u Unit) bool {
					return slices.ContainsFunc(u.Closes, func(o Open) bool { return o.Note.ID == n.ID })
				}), "%q: the decision %q names drop, whose step does not close it", typ, d)
			case "ack":
				w := setup(t, 1)
				n := Note{ID: "n-x.1", Kind: Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, Decisions: ds, At: w.s.Now}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, "s1-1"), Note: n})
				p := Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "seen", Who: w.s.Coordinator})
				assert.Empty(t, p.Refused, "%q: the decision %q is refused: %v", typ, d, p.Refused)
				assert.NotEmpty(t, p.Units, "%q: the decision %q names ack, which closes nothing", typ, d)
			}
		}
	}
}
