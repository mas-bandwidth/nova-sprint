package sprint

import "fmt"

// The base's CI after a landing (docs/SPEC-SPRINT.md section 8, "Drift alarms",
// beside the base red alarm). On 2026-10-11 the base was red for eight landings
// and nothing told the seat: the lander's narrow gate (NBaseRed) judged its own
// tree, NDriftBaseRed judged the whole-tree gate at the base's tip, and GitHub's
// check runs were read only by the release cut. So a red base after a landing is
// a fact the binding reads and the tick raises, once per red stretch, and closes
// when a fact for that (Repo, Base) is green. The facts are the binding's
// (the-server-reads-the-base-ci-after-a-landing reads them from the forge; this
// card reads none). The episode machine is the pass's, tla/CoordinatorPass.tla,
// as the drift alarms' and the coordinator pass's are: no module of its own.
const NBaseCIRed = "the base's CI is red after a landing"

// BaseCIFacts is the base's CI after a landing, read by the binding (one per
// repository and base): the newest landed commit whose check runs all completed,
// whether any of them concluded a failure or timed out, the oldest red commit
// since the last green one, and the failing check names of that first red
// commit. Repo or Base empty is no fact this tick, and the judgment it would
// judge is neither raised nor closed.
type BaseCIFacts struct {
	Repo     string   // the repository, owner/name
	Base     string   // the sprint base, the branch every stream lands on
	Tip      string   // the newest landed commit whose check runs all completed
	Red      bool     // any concluded failure or timed_out at Tip
	FirstRed string   // the oldest red commit since the last green one
	Failed   []string // the failing check names of FirstRed
}

// TickBaseCI is the tick's red-base judgment, planned in the deadlines part on
// the facts the binding read (TickReq.BaseCI; docs/SPEC-SPRINT.md section 8,
// "Drift alarms"): one judgment per red (Repo, Base) when the red stretch
// starts, closed when a fact for that (Repo, Base) is green. A (Repo, Base) the
// facts do not name keeps its open judgment, and the coordinator's wait on it,
// as they are: no fact is never a clear, as TickDrift carries a drift the facts
// say nothing of. No raise again: the coordinator's pass reminds of open
// judgments as it does for any.
func TickBaseCI(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	var conds []cond
	named := map[string]bool{} // the (repo, base) this read names
	for _, f := range r.BaseCI {
		if f.Repo == "" || f.Base == "" {
			continue
		}
		sub := f.Repo + "@" + f.Base
		named[sub] = true
		if !f.Red {
			continue // green: no condition, and notify closes any open judgment
		}
		conds = append(conds, cond{typ: NBaseCIRed, primaries: []string{sub}, decisions: TickDecisions[NBaseCIRed],
			what: fmt.Sprintf("%s %s is red after a landing: first red %s (%s), now %s; the cards landing onto it land onto red; fix it on %s first",
				f.Repo, f.Base, shortCommit(f.FirstRed), Preview(f.Failed, ", "), shortCommit(f.Tip), f.Base)})
	}
	carry := func(o Open) {
		// held as it stands: the same key, so notify neither writes nor closes it (a
		// wait run out is closed by notify, and raised again on its last facts)
		conds = append(conds, cond{typ: NBaseCIRed, primaries: []string{o.Subject()}, what: o.Note.What})
	}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NBaseCIRed && !named[o.Subject()] {
			carry(o)
		}
	}
	for _, o := range s.Acked {
		if !o.Note.Review.IsZero() && o.Note.Type == NBaseCIRed && !named[o.Subject()] {
			carry(o)
		}
	}
	due := notify(&p, s, conds, []string{NBaseCIRed}, r)
	return p, due
}
