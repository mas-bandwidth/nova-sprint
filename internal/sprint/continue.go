package sprint

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// Continue is the coordinator continuing a primary from the carried head
// (nova-sprint continue; docs/SPEC-SPRINT.md, the continue verb): a card whose
// attempts pushed partial work and reached its brief's bound is not judged wrong
// and is not started over — its brief is edited in place, a CARRY: line at the
// newest full head any attempt pushed (or the --carry head) written in its
// header, the continue note appended, its bound reset (FieldBriefAttempt, as a
// brief edit resets it), every open bound and brief-defect judgment answered,
// and with --tier its tier re-pinned in the same step. A card in review goes
// back to ready, its open reads retired, its next attempt staged from the
// carried head. It refuses, nothing written, when the primary is landed, when
// no attempt pushed a head and no --carry is given, and when --carry is not a
// 40-hex commit sha.
type ContinueReq struct {
	ID      string
	Note    string
	Tier    string
	Carry   string // the sha to carry; "" to carry the newest full head an attempt pushed
	Answers []string
	Who     string
}

// Continue is the continue step (docs/SPEC-SPRINT.md, the continue verb).
func Continue(s *Snapshot, r ContinueReq) Plan {
	var p Plan
	p.on(s)
	if why := notCoordinator(s, r.Who, "continue"); why != "" {
		p.refuse(r.ID, why)
		return p
	}
	c := s.Work.Placed(r.ID)
	switch {
	case c == nil:
		p.refuse(r.ID, "no primary "+r.ID+" on the work table; nothing was changed")
		return p
	case IsSentinel(c):
		p.refuse(r.ID, r.ID+" is a sentinel, not a primary; nothing was changed")
		return p
	case c.Col == Landed:
		p.refuse(c.ID, c.ID+" is landed: a card landed keeps its brief and is never continued; nothing was changed")
		return p
	}
	if r.Tier != "" && !cardhdr.IsRoute(r.Tier) {
		p.refuse(c.ID, "--tier wants "+cardhdr.RouteList+", found "+r.Tier+"; nothing was changed")
		return p
	}
	if r.Tier != "" {
		if why := tierBelowFrontierForTLA(c.ID, r.Tier, c.F("brief")); why != "" {
			p.refuse(c.ID, why)
			return p
		}
	}
	head, attempt := r.Carry, 0
	if head == "" {
		head, attempt = lastPushedHead(s, c)
		if attempt == 0 {
			p.refuse(c.ID, c.ID+" has no attempt pushed a head and no --carry was given; nothing was changed")
			return p
		}
	} else if !typedrec.IsFullSha(head) {
		p.refuse(c.ID, "--carry wants a 40-hex commit sha; found "+head+"; nothing was changed")
		return p
	} else {
		attempt = c.Int("attempt")
		if attempt == 0 {
			attempt = 1
		}
	}
	n := c.Int("attempt")
	brief := PathsWidened(c.F("brief"), nil, member.CarryLine(member.Carry{Card: r.ID, Attempt: attempt, Head: head}))
	if text := strings.TrimSpace(r.Note); text != "" {
		note := "CONTINUE " + stamp(s.Now) + " " + cutText(text, MaxCardTextBytes-64)
		brief = strings.TrimRight(brief, "\n") + "\n\n" + note
	}
	// a continue is a coordinator decision on the card: the same-finding bound and the attempt
	// cap's count both start again from this attempt (brief_bound.go, FieldDecidedAttempt)
	set := map[string]string{"brief": brief, FieldBriefAttempt: c.F("attempt"), FieldDecidedAttempt: c.F("attempt")}
	if r.Tier != "" {
		set[FieldTier] = r.Tier
	}
	set["why"] = cutText(fmt.Sprintf("continued by %s at attempt %d from head %s", orDash(r.Who), n, head), MaxCardTextBytes)
	unset := []string{"fix", "finding", FieldFindingAttempt}
	var changes []Change
	retire := func(table string, x *Card) {
		changes = append(changes, change(table, removeEntry(x, map[string]string{"retired": stamp(s.Now), "retired_by": "continue"})))
	}
	to := c.Col
	orphans := map[string]bool{}
	if c.Col == Review {
		to = Ready
		if s.Readers != nil {
			for _, rc := range s.Readers.Of(c.ID) {
				retire(Readers, rc)
			}
		}
		for _, rc := range openFriendReads(s, c.ID) {
			retire(Fleet, rc)
		}
		changes = append(changes, change(Work, moveEntry(c, c.Row, Ready, set, append(unset, "result", "readers", FieldPassedHead)...)))
		if m := orphanMerge(s, c); m != nil {
			changes = append(changes, change(Merge, moveEntry(m, c.Row, Returned, nil, "need_card", "need_stream")))
			orphans[c.ID] = true
		}
	} else {
		changes = append(changes, change(Work, setEntry(c, set, unset...)))
	}
	p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: changes,
		Closes: closesFor(s.Open, []string{NBound, NBriefWrong}, c.ID),
		Moved:  fmt.Sprintf("%s continued by %s from head %s; %s -> %s, attempt %d next, from the carried head; %d cards retired", c.ID, orDash(r.Who), head, c.Col, to, n+1, len(changes)-1)})
	if len(orphans) > 0 {
		settle(&p, s, r.Who, orphans, nil)
	}
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}

// lastPushedHead is the newest full head an attempt of the primary pushed, and
// the attempt that pushed it (BaseOf over the card's work cards); 0 and "" when
// no attempt pushed one.
func lastPushedHead(s *Snapshot, c *Card) (head string, attempt int) {
	if s.Fleet == nil {
		return "", 0
	}
	var attempts []*Card
	for a := 1; a <= c.Int("attempt"); a++ {
		if w := s.Fleet.Card(WorkCardID(c.ID, a)); w != nil {
			attempts = append(attempts, w)
		}
	}
	if b := BaseOf(attempts); b.Attempt > 0 {
		return b.Head, b.Attempt
	}
	return "", 0
}
