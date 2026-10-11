package sprint

import (
	"fmt"
	"strings"
)

// A STOP ON NEEDS THAT HAVE NOT LANDED IS A COORDINATION FAILURE (docs/SPEC-SPRINT.md, needs
// and the deal; the owner, 2026-10-11: "Blocker bug to fix that please."). A card whose
// named needs have not landed is neither dealt nor taken (TickDeal, friendDealPass,
// needsWithdrawals, takeOne); one that reached a worker anyway (dealt before the rule, or
// given a need after its take) and whose failed finish arrives while a named need has still
// not landed is the coordination's failure, never the worker's and never the brief's: no
// brief defect, no failed work, no fail mark and no ok% hit on the worker's row, no redeal
// spent. It is decided on the table's facts alone (NamedWaits at the finish), never on the
// report's words: once every need has landed a HOLD is judged as any other (the brief-defect
// and hold rules). No counter of coordination failures exists, so the stop is classed as not
// the worker's: the work card goes back to the pool and the primary waits for its needs
// (working -> waiting), with one happened note, NNeedsUnlanded, naming the needs. The next
// deal cuts the primary's next attempt: an attempt number is spent, but no redeal and no
// failure. A primary returned this way MaxNeedsReturns times is judged as any other failure
// from then on, so no loop runs unseen.

// MaxNeedsReturns is how many failed finishes of one primary are returned as stops on
// unlanded needs (FieldNeedsReturns); past it the finish is judged as any other.
const MaxNeedsReturns = 3

// FieldNeedsReturns is the primary's count of finishes returned as stops on unlanded needs.
const FieldNeedsReturns = "needs_returns"

// EndNeedsUnlanded is the consumer end a returned stop records on the producer: counted in
// neither ok nor failed by the route and model stats (stats.go).
const EndNeedsUnlanded = "needs not landed"

// stoppedOnNeeds is the needs a failed finish of the primary's work card stopped on: its
// named needs that have not landed at the finish (NamedWaits), while the primary has been
// returned fewer than MaxNeedsReturns times. nil when there are none, or past the cap: the
// failure is judged as any other.
func stoppedOnNeeds(s *Snapshot, pr *Card) []string {
	if pr == nil || pr.Int(FieldNeedsReturns) >= MaxNeedsReturns {
		return nil
	}
	return NamedWaits(s, pr, nil)
}

// needsStopReturned is the unit of a failed finish that stopped on needs that have not
// landed (stoppedOnNeeds; docs/SPEC-SPRINT.md, needs and the deal): the work card goes back
// to the pool as needsWithdrawals sends it (it leaves the fleet table at a new generation),
// WITHOUT FieldTakeEnded, so no redeal is spent and it ends in neither done cell (no fail
// mark, no ok% hit), and the primary goes working -> waiting with its needs (lifecycle.go,
// the move for needs not landed). The next deal cuts its next attempt. The happened note
// names the needs and the worker's report; there is nothing for the seat to answer.
func needsStopReturned(s *Snapshot, c, pr *Card, r FinishReq, waits []string, who string) Unit {
	needs := strings.Join(waits, ",")
	set := nextGen(c, "", s.Now)
	set["withdrawn"] = stamp(s.Now)
	set[FieldTakenBack] = "its needs " + needs + " have not landed: a coordination failure, not the worker's"
	prSet := needsSet(pr, waits)
	prSet[FieldNeedsReturns] = itoa(pr.Int(FieldNeedsReturns) + 1)
	// the take's consumer record, priced or not, as an ordinary finish writes one: the route
	// and model stats count their takes from the consumers alone (StatsSince), so a stop
	// whose worker reported no usage is still one take, counted neither ok nor failed
	dealt, taken := takeStamps(c)
	addConsumer(pr, prSet, workConsumer(s, c, 0, EndNeedsUnlanded, costRecord(s, r.Usage, c.F(FieldRoute), c.F(FieldModel), false, dealt, taken)))
	n := happened(NNeedsUnlanded, pr.Row, s.Now, pr.ID)
	n.Who, n.Attempt, n.Card = who, c.Int("attempt"), c.ID
	n.What = fmt.Sprintf("%s stopped on %s: needs %s have not landed; dealt before them, a coordination failure, not the worker's (no fail mark, no redeal spent; return %d of %d); %s -> waiting",
		c.ID, c.Row, needs, pr.Int(FieldNeedsReturns)+1, MaxNeedsReturns, pr.ID)
	if rep := strings.TrimSpace(r.Report); rep != "" {
		n.What += "; " + cutText(rep, MaxProviderErrorBytes)
	}
	return Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, removeEntry(c, set)),
		change(Work, moveEntry(pr, pr.Row, Waiting, prSet, "work")),
	}, Notes: []Note{n}, Moved: fmt.Sprintf("%s working -> pool gen=%d, its needs %s have not landed (a coordination failure); %s working -> waiting", c.ID, c.Int("gen")+1, needs, pr.ID)}
}
