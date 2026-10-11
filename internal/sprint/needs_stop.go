package sprint

import (
	"fmt"
	"regexp"
	"strings"
)

// A STOP ON NEEDS THAT HAVE NOT LANDED IS A COORDINATION FAILURE (docs/SPEC-SPRINT.md, needs
// and the deal; the owner, 2026-10-11: "Blocker bug to fix that please."). A card whose
// named needs have not landed is neither dealt nor taken (TickDeal, friendDealPass,
// needsWithdrawals, takeOne); one that reached a worker anyway (dealt before the rule, or
// given a need after its take) and that the worker rightly stopped on is the coordination's
// failure, never the worker's and never the brief's: no brief defect, no failed work, no
// fail mark and no ok% hit on the worker's row, no attempt and no redeal spent. No counter
// of coordination failures exists, so the stop is classed as not the worker's: the work card
// is withdrawn untouched and the primary goes back to ready, where the deal deals it nowhere
// while its needs have not landed and the next tick moves it to waiting (needsWaits), with
// one happened note, NNeedsUnlanded, naming the needs.

// needsNotLanded is a worker's words for a need that has not landed.
var needsNotLanded = regexp.MustCompile(`(?i)\bnot\s+(?:yet\s+)?landed\b|\bunlanded\b|n['’]t\s+(?:yet\s+)?landed\b`)

// stoppedOnNeeds is the needs a failed finish of the primary's work card stopped on: its
// named needs that still have not landed (NamedWaits), else, when every one has landed since,
// those of its named needs (not waived) that the worker's report names as not landed. nil
// when there are none: the failure is judged as any other.
func stoppedOnNeeds(s *Snapshot, pr *Card, report string) []string {
	if pr == nil {
		return nil
	}
	if w := NamedWaits(s, pr, nil); len(w) > 0 {
		return w
	}
	if !needsNotLanded.MatchString(report) {
		return nil
	}
	waived := Split(pr.F("waived"))
	var out []string
	for _, n := range Split(pr.F("needs")) {
		if !contains(waived, n) && hasWord(report, n) {
			out = append(out, n)
		}
	}
	return out
}

// needsStopReturned is the unit of a failed finish that stopped on needs that had not
// landed (stoppedOnNeeds; docs/SPEC-SPRINT.md, needs and the deal): the work card goes back
// to the pool as needsWithdrawals sends it (it leaves the fleet table at a new generation,
// so check.go rule 2 holds once the primary waits), WITHOUT FieldTakeEnded, so no redeal
// and no attempt is spent and it ends in neither done cell (no fail mark, no ok% hit), and the primary goes back to
// ready (to waiting at the next tick while its needs have not landed). The happened note names
// the needs and the worker's report; there is nothing for the seat to answer.
func needsStopReturned(s *Snapshot, c, pr *Card, r FinishReq, waits []string, who string) Unit {
	needs := strings.Join(waits, ",")
	set := nextGen(c, "", s.Now)
	set["withdrawn"] = stamp(s.Now)
	set[FieldTakenBack] = "its needs " + needs + " had not landed: a coordination failure, not the worker's"
	prSet := map[string]string{}
	if r.Usage != "" {
		dealt, taken := takeStamps(c)
		addConsumer(pr, prSet, workConsumer(s, c, 0, "needs not landed", costRecord(s, r.Usage, c.F(FieldRoute), c.F(FieldModel), false, dealt, taken)))
	}
	// working -> ready, the one lifecycle move open to a working primary whose work card is
	// withdrawn (lifecycle.go Moves); while its needs have not landed the tick's next deal
	// deals it nowhere and moves it ready -> waiting (needsWaits, steps_tick.go)
	col := Ready
	n := happened(NNeedsUnlanded, pr.Row, s.Now, pr.ID)
	n.Who, n.Attempt, n.Card = who, c.Int("attempt"), c.ID
	n.What = fmt.Sprintf("%s stopped on %s: needs %s had not landed; dealt before them, a coordination failure, not the worker's (no fail mark, no attempt or redeal spent); %s -> %s",
		c.ID, c.Row, needs, pr.ID, col)
	if rep := strings.TrimSpace(r.Report); rep != "" {
		n.What += "; " + cutText(rep, MaxProviderErrorBytes)
	}
	return Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, removeEntry(c, set)),
		change(Work, moveEntry(pr, pr.Row, col, prSet, "work")),
	}, Notes: []Note{n}, Moved: fmt.Sprintf("%s working -> pool gen=%d, its needs %s had not landed (a coordination failure); %s working -> %s", c.ID, c.Int("gen")+1, needs, pr.ID, col)}
}
