package sprint

import (
	"fmt"
	"strings"
)

// CoordinatorReadReq is the coordinator's own read of a primary in review,
// through the read verb: read --as <coordinator> --ok|--broken <card>
// --finding <text>. It is recorded on the primary as the coordinator's read of
// the card's attempt and head and counts toward the card's read rule as a
// reader's ok read does (ReadsStanding). It is refused for the card's own
// worker: no one reads their own work.
type CoordinatorReadReq struct {
	IDs     []string
	Verdict string // ok or broken
	Finding string
	Who     string // the coordinator's name
}

// CoordinatorReadKind is the heavy-read kind of the coordinator's own read
// through the read verb, beside the accept --heavy kind (HeavyKind).
const CoordinatorReadKind = "coordinator-read"

// CoordinatorRead records the coordinator's own read of each named primary in
// review. An ok read stands as one of the reads the card needs (ReadsStanding
// counts it); a broken read raises the read-broken judgment as a reader's would
// and stands beside the reader's oks so accept refuses the head.
func CoordinatorRead(s *Snapshot, r CoordinatorReadReq) Plan {
	var p Plan
	if len(r.IDs) == 0 {
		p.refuse("read", "a coordinator read names the card it read: read --as <coordinator> --ok|--broken <card> --finding <text>")
		return p
	}
	if r.Verdict != "ok" && r.Verdict != "broken" {
		p.refuse("read", "a coordinator read is --ok or --broken")
		return p
	}
	who := r.Who
	if who == "" {
		who = "coordinator"
	}
	for _, id := range r.IDs {
		c := s.Work.Placed(id)
		if c == nil {
			p.refuse(id, "no such primary in review")
			continue
		}
		if c.Col != Review {
			p.refuse(id, "not in review (it is "+c.Col+")")
			continue
		}
		attempt := max(c.Int("attempt"), 1)
		if w := attemptWorker(s, id, attempt); w != "" && w == who {
			p.refuse(id, "the card's own worker reads it not: "+who+" worked this attempt")
			continue
		}
		if wc := s.Fleet.Card(WorkCardID(id, attempt)); wc != nil {
			if wc.Row == who || wc.F("member") == who {
				p.refuse(id, "the card's own worker reads it not: "+who+" worked this attempt")
				continue
			}
		}
		if r.Verdict == "broken" && strings.TrimSpace(r.Finding) == "" {
			p.refuse(id, "a broken coordinator read names its defect (--finding <text>)")
			continue
		}
		set := map[string]string{
			FieldHeavyReader:  CoordinatorReaderPrefix + who,
			FieldHeavyKind:    CoordinatorReadKind,
			FieldHeavyVerdict: r.Verdict,
			FieldHeavyAttempt: c.F("attempt"),
			FieldHeavyHead:    c.F("head"),
			FieldHeavyAt:      stamp(s.Now),
		}
		if r.Finding != "" {
			set[FieldHeavyFinding] = r.Finding
		}
		u := Unit{Key: id, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, set))},
			Moved: fmt.Sprintf("%s read by %s: %s", id, CoordinatorReaderPrefix+who, r.Verdict)}
		if r.Verdict == "broken" {
			n := judgment(NReadBroken, c.Row, s.Now, c.Int("broken_reads"), id)
			n.Who, n.Attempt, n.What = CoordinatorReaderPrefix+who, c.Int("attempt"), r.Finding
			u.Notes = append(u.Notes, n)
		}
		p.Units = append(p.Units, u)
	}
	p = Lawful(p)
	return p
}

// coordinatorReadStanding is 1 when the primary carries the coordinator's own ok
// read at its current attempt and head, else 0: it counts as one of the reads
// the card needs (Accept's eligible, ReadsStanding).
func coordinatorReadStanding(c *Card) int {
	if c == nil {
		return 0
	}
	if c.F(FieldHeavyReader) == "" || c.F(FieldHeavyVerdict) != "ok" {
		return 0
	}
	if c.F(FieldHeavyAttempt) != c.F("attempt") || c.F(FieldHeavyHead) != c.F("head") {
		return 0
	}
	return 1
}
