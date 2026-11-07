package steps

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// fix is the fix step (LAYER-3 "Fix"; the model's FixSet): once every read
// of a producer's current review is done or withdrawn and one is done ok or
// fix, policy's verdict over all of them (LESSONS 3.7) is ReadYes or
// ReadFix{finding}, the finding every fix read's joined. A faulted read is
// not a read (directive 58): it is owed again, up to policy's faults since
// the coordinator last said read, and then one read-faults judgement asks
// the coordinator to read, yes or fix. A finding byte-equal to the
// producer's current one opens a brief judgement (LESSONS 5.9). A producer
// back from Broken or Faulted needs no plan here.
func (t *tick) fix() {
	var fixes []string
	for _, p := range t.v.Producers("review") {
		c := t.v.Cards[p]
		if !t.free(p) {
			continue
		}
		reads := t.settled(c)
		if reads == nil {
			t.readFaults(c)
			continue
		}
		if t.o.verdict(VerdictQuestion{Card: t.facts(c), Reads: reads}) == "yes" {
			t.group("yes", p, t.producerMove(p, card.ReadYes{}))
			continue
		}
		fixes = append(fixes, p)
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

// settled is the done reads of c's current review and head when every read
// of that review is final (done or withdrawn) and at least one is done; nil
// while a read is live or none has reported.
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
	return out
}

// readFaults opens the read-faults judgement for c when its reads have
// faulted up to the limit and none is live: no read is dealt for it until
// the coordinator answers read (the count starts again), yes (JudgedYes)
// or fix (JudgedFix with the reason as the finding).
func (t *tick) readFaults(c store.Card) {
	if t.hasLive(c.ID) || !t.faultCapped(c) {
		return
	}
	t.judge("read-faults", c.ID, fmt.Sprintf("producer %s: %d read(s) of review %d at head %s faulted (the limit is %d) and none said ok or fix; "+
		"read: ask a read again; yes: you read it and it is sound (it goes to merging); fix: it goes to fix with your reason as the finding",
		c.ID, t.faults(c), c.Review, c.Head, t.faultLimit()))
}

// faults is how many done reads of c's current review and head faulted
// since the coordinator last answered a read-faults judgement of c with
// read: a read dealt at or after that answer (none is dealt while capped,
// so every read the answer covered was dealt before it).
func (t *tick) faults(c store.Card) int {
	since := t.jindex().reads[c.ID]
	n := 0
	for _, r := range t.v.Consumers(c.ID) {
		if r.Kind == "read" && r.Review == c.Review && r.Head == c.Head && r.State == "done" && r.Result == "fault" &&
			!t.v.TakenAt[r.ID].Before(since) {
			n++
		}
	}
	return n
}

// faultCapped says whether c's faulted reads reached policy's faults: no
// more reads are owed until the coordinator answers.
func (t *tick) faultCapped(c store.Card) bool { return t.faults(c) >= t.faultLimit() }

// faultLimit is limits().faults; the engine's while the questions are
// still being gathered (before the oracle exists).
func (t *tick) faultLimit() int {
	if t.o == nil {
		return Engine.Limits.Faults
	}
	return t.lim.Faults
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
// the current review that are live or done ok or fix (a withdrawn one is
// owed again, and so is a faulted one: a fault is not a read, directive
// 58), none once its faults reach the limit (readFaults).
func (t *tick) owed(c store.Card) int {
	if c.State != "review" || t.faultCapped(c) {
		return 0
	}
	have := 0
	for _, r := range t.v.Consumers(c.ID) {
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
