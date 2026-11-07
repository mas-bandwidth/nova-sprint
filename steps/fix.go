package steps

import (
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// fix is the fix step (LAYER-3 "Fix"; the model's FixSet): once every read
// asked of a producer's current review is done (as many as policy's reads
// count) and none is live, policy's verdict over all of them (LESSONS 3.7)
// is ReadYes or ReadFix{finding}, the finding every fix read's joined. A
// finding byte-equal to the producer's current one opens a brief judgement
// (LESSONS 5.9). A producer back from Broken or Faulted needs no plan here.
func (t *tick) fix() {
	var fixes []string
	for _, p := range t.v.Producers("review") {
		c := t.v.Cards[p]
		if !t.free(p) {
			continue
		}
		reads := t.settled(c)
		if reads == nil {
			continue
		}
		switch t.o.verdict(VerdictQuestion{Card: t.facts(c), Reads: reads}) {
		case "yes":
			t.group("yes", p, t.producerMove(p, card.ReadYes{}))
		case "fix":
			fixes = append(fixes, p)
		default:
			t.retry[p] = struct{}{} // one more read is asked (the deal)
		}
	}
	for _, p := range fixes { // every yes before every fix, as the model orders them
		c := t.v.Cards[p]
		finding := joinFindings(t.settled(c))
		if finding == c.Finding {
			t.judge("brief", p, "the read of "+p+" found the same thing twice: "+finding+"; the brief is wrong, not the worker")
		}
		t.group("fix", p, t.producerMove(p, card.ReadFix{Finding: finding}))
	}
}

// settled is the done reads of c's current review and head when as many as
// policy asked are done and none is live; nil while a read is live or owed
// (round 2 ruling 9: fewer reads than asked never merge). A read that
// faulted is not a read (the intent audit, row 12): it is owed again.
func (t *tick) settled(c store.Card) []ReadFacts {
	if c.State != "review" || t.hasLive(c.ID) {
		return nil
	}
	var out []ReadFacts
	for _, r := range t.v.Consumers(c.ID) {
		if r.Kind == "read" && r.Review == c.Review && r.Head == c.Head && r.State == "done" && r.Result != "fault" {
			out = append(out, ReadFacts{Row: r.Holder, Rank: t.rank(r.Holder), Result: r.Result, Finding: r.Reason})
		}
	}
	if len(out) < t.readsWanted(c) {
		return nil
	}
	return out
}

// joinFindings is every fix read's finding, in read order, joined; a fix
// read that said nothing is named as such (a fix read says what to fix).
func joinFindings(reads []ReadFacts) string {
	var parts []string
	for _, r := range reads {
		if r.Result == "fix" {
			f := r.Finding
			if f == "" {
				f = "read on " + r.Row + " said fix without a finding"
			}
			parts = append(parts, f)
		}
	}
	return strings.Join(parts, "\n")
}

// owed is how many reads c still needs: policy's count less the reads of
// the current review that are live or done with a verdict (a withdrawn or
// faulted one is owed again), plus one after a retry verdict.
func (t *tick) owed(c store.Card) int {
	if c.State != "review" {
		return 0
	}
	have := 0
	for _, r := range t.v.Consumers(c.ID) {
		if r.Kind == "read" && r.Review == c.Review && r.State != "fail" && !(r.State == "done" && r.Result == "fault") {
			have++
		}
	}
	owed := t.readsWanted(c) - have
	if _, again := t.retry[c.ID]; again && owed <= 0 {
		owed = 1
	}
	return owed
}

// readsWanted is reads(card), asked once in the tick's first call and read
// by the owed count and the deal alike.
func (t *tick) readsWanted(c store.Card) int { return t.o.reads(t.facts(c)) }
