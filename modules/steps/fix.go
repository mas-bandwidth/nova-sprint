package steps

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// fix is the fix step (LAYER-3 "Fix"; the model's FixSet): once every read
// of a producer's current review is done or withdrawn and one is done,
// policy's verdict over all of them (LESSONS 3.7) is ReadYes or
// ReadFix{finding}, the finding every fix read's joined. A finding
// byte-equal to the producer's current one opens a brief judgement
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
			continue
		case "retry": // every read faulted: owed() deals one more; nothing to move
			continue
		}
		fixes = append(fixes, p)
	}
	for _, p := range fixes { // every yes before every fix, as the model orders them
		c := t.v.Cards[p]
		finding := joinFindings(t.settled(c))
		if n := t.sameFinding(c, finding); n >= t.lim.SameFinding {
			t.judge("brief", p, fmt.Sprintf("the reads of %s found the same thing %d reviews running: ", p, n)+finding+"; the brief is wrong, not the worker. rework closes this and nothing more: the fix is dealt on the brief as it is; to change the brief, nova-sprint replace "+p+" --by <card>; drop cancels it")
		}
		t.group("fix", p, t.producerMove(p, card.ReadFix{Finding: finding}))
	}
}

// sameFinding is how many of c's reviews running, ending at the current
// one, found finding: the current, then each earlier review whose done
// reads joined to the same text (policy's same_finding, LESSONS 5.9).
func (t *tick) sameFinding(c store.Card, finding string) int {
	n := 1
	for r := c.Review - 1; r >= 1; r-- {
		var reads []ReadFacts
		for _, x := range t.v.Consumers(c.ID) {
			if x.Kind == "read" && x.Review == r && x.State == "done" {
				reads = append(reads, ReadFacts{Row: x.Holder, Result: x.Result, Finding: x.Reason})
			}
		}
		if len(reads) == 0 || joinFindings(reads) != finding {
			return n
		}
		n++
	}
	return n
}

// settled is the done reads of c's current review and head when every read
// of that review is final (done or withdrawn) and at least one is done; nil
// while a read is live or none has reported.
func (t *tick) settled(c store.Card) []ReadFacts {
	if c.State != "review" || t.hasLive(c.ID) {
		return nil
	}
	var out []ReadFacts
	for _, r := range t.v.Consumers(c.ID) {
		if r.Kind == "read" && r.Review == c.Review && r.Head == c.Head && r.State == "done" {
			out = append(out, ReadFacts{Row: r.Holder, Rank: t.rank(r.Holder), Result: r.Result, Finding: r.Reason})
		}
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
// the current review that are live or done (a withdrawn one is owed again).
func (t *tick) owed(c store.Card) int {
	if c.State != "review" {
		return 0
	}
	have := 0
	for _, r := range t.v.Consumers(c.ID) {
		// a done read that faulted is not a read (defect 58): it is owed again
		if r.Kind == "read" && r.Review == c.Review && r.State != "fail" && !(r.State == "done" && r.Result == "fault") {
			have++
		}
	}
	return t.readsWanted(c) - have
}

// readsWanted is reads(card); the engine default while the questions are
// still being gathered (before the oracle exists).
func (t *tick) readsWanted(c store.Card) int {
	if t.o == nil {
		return Engine.Reads
	}
	return t.o.reads(t.facts(c))
}
