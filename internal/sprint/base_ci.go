package sprint

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// A tip moved outside the lander is judged before merging by gateBase and
// OutsideBaseRed (land_base.go); this reader covers CI after lander pushes.
//
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

// The fix-red card of a red base (docs/SPEC-SPRINT.md section 7, the base's class gate;
// tla/CoordinatorPass.tla, Kind "fixred"). On 2026-10-15 the space bases went red on gofmt and
// vet; the lander named a fix-red card in its judgment and admitted none, so each base waited
// for the coordinator. A class-red tree gate now admits that card into the stream whose base is
// red (FixRedAdmit): a blocker, first in the stream's line, one card for a class and base
// while one is open (OneFixRedPerClass), admitted in the plan of the stop that opens the episode
// (WithFixRed). The tick deals it as any ready card. When it has been
// read ok and sits queued to merge, the stream the base's red stopped is resumed by the
// base-gate rule (TickRuleFixRed), so the lander's cure search lands it first onto the red base
// (land_cure.go); the episode, the stop's judgment, stays open until the card lands, and its
// landing answers it (EpisodeOpenUntilLanding, EpisodeClosesOnLanding): the base is then green
// on the new tip and the stream's next landing goes on (ResumesWithoutCoordinator). A card tried as a cure that did not cure resumes nothing again at
// the same head (FieldFixRedTried): the judgment stays for a mind.

// FixRedPrefix starts the id of a fix-red card (cardgen.PlanClassRed: fix-red-<class>-<base>).
const FixRedPrefix = "fix-red-"

// FieldFixRedTried is the fix-red card and head the base-gate rule last resumed a stream for
// ("<card>@<head>"), on its control card: a stop that meets the same pair again is left.
const FieldFixRedTried = "fixred_tried"

// PartRuleFixRed is the tick part of the fix-red resume; TickRules (rules.go) runs it.
const PartRuleFixRed = "rule fixred"

func init() { TickRules = append(TickRules, TickPartDef{PartRuleFixRed, TickRuleFixRed}) }

// fixRedBriefLine is the value of a header line of a brief.
func fixRedBriefLine(brief, key string) string {
	for line := range strings.SplitSeq(brief, "\n") {
		if k, v, ok := cardhdr.KeyValue(line); ok && k == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// inFixRedFamily says card is id or id's -<n> successor (a card of the same class and base
// whose id a landed card of an earlier red already held).
func inFixRedFamily(id, card string) bool {
	if card == id {
		return true
	}
	n, ok := strings.CutPrefix(card, id+"-")
	if !ok {
		return false
	}
	_, err := strconv.Atoi(n)
	return err == nil
}

// FixRedOpen is the open card (placed, not landed) of the fix-red family id, nil when none is.
func FixRedOpen(s *Snapshot, id string) *Card {
	for _, c := range s.Work.Cards() {
		if c.Placed() && c.Col != Landed && !IsSentinel(c) && inFixRedFamily(id, c.ID) {
			return c
		}
	}
	return nil
}

// FixRedStream is the stream whose base is red: the first stream, in stream order, holding a
// card not yet landed whose brief names base on its BASE line. "" when none does (nothing is
// waiting on the base).
func FixRedStream(s *Snapshot, base string) string {
	if base == "" {
		return ""
	}
	for _, st := range s.Streams() {
		for _, c := range s.Work.Cards() {
			if c.Row == st && c.Placed() && c.Col != Landed && !IsSentinel(c) && fixRedBriefLine(c.F("brief"), "BASE") == base {
				return st
			}
		}
	}
	return ""
}

// FixRedRepo is the repository the stream's cards on base name on their REPO line.
func FixRedRepo(s *Snapshot, stream, base string) string {
	for _, c := range s.Work.Cards() {
		if c.Row == stream && c.Placed() && c.Col != Landed && fixRedBriefLine(c.F("brief"), "BASE") == base {
			if repo := fixRedBriefLine(c.F("brief"), "REPO"); repo != "" {
				return repo
			}
		}
	}
	return ""
}

// FixRedAdd is a class-red base's fix-red card as the lander built it: Card is its id, file and
// brief (cardgen.Render of cardgen.PlanClassRed), already held to the add lint by the caller.
// Stream is the stream the lander is stopping on the base, the card's stream when it holds a card
// not landed on that base; else the first stream that does (FixRedStream).
type FixRedAdd struct {
	Base   string
	Stream string
	Card   CardAdd
	Who    string
}

// fixRedStreamOf is the stream the card of r goes into: r.Stream when it holds a card not
// landed on the base, else FixRedStream.
func fixRedStreamOf(s *Snapshot, r FixRedAdd) string {
	if r.Stream != "" {
		for _, c := range s.Work.Cards() {
			if c.Row == r.Stream && c.Placed() && c.Col != Landed && !IsSentinel(c) && fixRedBriefLine(c.F("brief"), "BASE") == r.Base {
				return r.Stream
			}
		}
	}
	return FixRedStream(s, r.Base)
}

// FixRedAdmit admits the fix-red card of a class-red base through the add path (Add): into
// the stream whose base is red (FixRedStream), a blocker (its PRIORITY line) scored first in
// line (FixCards). Idempotent: a card of the family open on the table admits nothing, so a
// second red of the same class and base, or a re-raise of the episode, adds none
// (tla/CoordinatorPass.tla OneFixRedPerClass). An id a landed card of an earlier red holds
// takes the next free -<n>. An empty plan when there is nothing to admit.
func FixRedAdmit(s *Snapshot, r FixRedAdd) Plan {
	stream := fixRedStreamOf(s, r)
	if stream == "" || FixRedOpen(s, r.Card.ID) != nil {
		return Plan{}
	}
	add, _ := FixCards(s, AddReq{Stream: stream, Cards: []CardAdd{r.Card}, Who: r.Who})
	if len(add.Cards) == 0 {
		return Plan{}
	}
	if c := &add.Cards[0]; c.ID != r.Card.ID {
		c.Brief = strings.Replace(c.Brief, "RESULT: "+r.Card.ID, "RESULT: "+c.ID, 1)
	}
	return Add(s, add)
}

// WithFixRed is the plan of a stop with the fix-red card admitted in it (the one store step of
// the stop that opens the base-red episode; tla/CoordinatorPass.tla FxRaise): p is the lander's
// plan (LandBaseRefused), and when it raises the episode's judgment (NBaseRed) the card add
// joins it, so the episode never exists without its card, nor the card without its episode. A
// plan that raises no episode (a refusal counted, a stream refused under another's judgment)
// admits nothing. A refusal of the add (its lint, the stream's cards) is never routed around and
// never refuses the stop: it is said in the episode's judgment text. add nil admits nothing.
//
// A stream resumed with its fix-red card queued keeps its episode's judgment open until the
// card lands (TickRuleFixRed); a stop raised on that stream again (the card did not cure the
// base) answers that judgment as superseded, so the episode has one judgment
// (OneJudgmentAnEpisode).
func WithFixRed(s *Snapshot, p Plan, add *FixRedAdd) Plan {
	if len(p.Refused) > 0 {
		return p
	}
	ju, jn := -1, -1
	for i, u := range p.Units {
		for j, n := range u.Notes {
			if n.Kind == Judgment && n.Type == NBaseRed {
				ju, jn = i, j
			}
		}
	}
	if ju < 0 {
		return p
	}
	st := p.Units[ju].Stream
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NBaseRed && o.Note.Stream == st && o.Key != "" {
			u := &p.Units[ju]
			u.Closes = append(u.Closes, o)
			u.Notes = append(u.Notes, decided(o, RuleSaid(RuleBaseGate, "the stop is raised again; this episode's judgment is the new one's"), u.Notes[jn].Who, s.Now))
		}
	}
	if add == nil {
		return p
	}
	in := *add
	in.Stream = st // the stream the stop is on
	q := FixRedAdmit(s, in)
	if len(q.Refused) > 0 {
		n := &p.Units[ju].Notes[jn]
		n.What = cutText(n.What+"; the fix-red card "+add.Card.ID+" was refused by the add: "+q.Refused[0].Why, MaxCardTextBytes)
		return p
	}
	if len(q.Units) == 0 {
		return p
	}
	p.on(s)
	p.Rows = append(p.Rows, q.Rows...)
	p.Places = append(p.Places, q.Places...)
	p.Units = append(p.Units, q.Units...)
	p.Notes = append(p.Notes, q.Notes...)
	p.Said = append(p.Said, q.Said...)
	p.Props = append(p.Props, q.Props...)
	return p
}

// fixRedQueued is the fix-red card of the stream's base that sits queued to merge, nil when
// none does.
func fixRedQueued(s *Snapshot, stream, base string) *Card {
	for _, m := range s.Merge.Cell(stream, Queued) {
		pr := s.Work.Placed(m.ID)
		if pr != nil && pr.Row == stream && pr.Col == Merging && strings.HasPrefix(pr.ID, FixRedPrefix) && fixRedBriefLine(pr.F("brief"), "BASE") == base {
			return pr
		}
	}
	return nil
}

// TickRuleFixRed is the base-gate rule's two steps on an episode that admitted a fix-red card.
//
// The resume: a stream stopped on its base's red whose fix-red card has been read ok and sits
// queued to merge is resumed, so the lander's pass lands the card first as the base's cure. The
// episode stays open: its judgment is not answered (EpisodeOpenUntilLanding). The card at the
// head it was resumed for is recorded (FieldFixRedTried), and a stop that meets that pair again
// is left to a mind. tla/CoordinatorPass.tla FxResume.
//
// The close: a stream resumed that way whose recorded card has landed, and is not stopped again,
// has its episode closed: the judgment is answered by the rule, the record cleared. The landing
// is the lander's and was gated (the cure search lands a head only when its tree passes the
// whole class gate), so the base is green on the new tip. tla/CoordinatorPass.tla FxLandCard,
// EpisodeClosesOnLanding.
func TickRuleFixRed(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if !r.AnswerRules || s.RuleOff(RuleBaseGate) {
		return p, 0
	}
	for _, st := range BaseRedStreams(s) {
		ctl := s.StreamCtl(st)
		c := fixRedQueued(s, st, ctl.F(FieldBaseGateBase))
		if c == nil || ctl.F(FieldFixRedTried) == c.ID+"@"+c.F("head") {
			continue
		}
		why := RuleSaid(RuleBaseGate, "its fix-red card "+c.ID+" is read ok and queued to merge: the stream resumes and the lander lands it first as the base's cure; the episode stays open until it lands")
		q := Resume(s, ResumeReq{Stream: st, Did: why, Who: r.who()})
		for _, u := range q.Units {
			clearBasePassed(&u, ctl)
			for i := range u.Changes {
				if e := &u.Changes[i].Entry; e.ID == ctl.ID && u.Changes[i].Table == Merge {
					if e.Set == nil {
						e.Set = map[string]string{}
					}
					e.Set[FieldFixRedTried] = c.ID + "@" + c.F("head")
				}
			}
			u.Closes = nil // the episode's judgment is the card's landing to answer
			p.Units = append(p.Units, u)
		}
		p.Refused = append(p.Refused, q.Refused...)
	}
	for _, o := range s.Open {
		if o.Note.Kind != Judgment || o.Note.Type != NBaseRed {
			continue
		}
		st := o.Note.Stream
		ctl := s.StreamCtl(st)
		if ctl == nil || ctl.F("state") == StreamStopped {
			continue
		}
		id, _, ok := strings.Cut(ctl.F(FieldFixRedTried), "@")
		if !ok || s.StateOf(id) != Landed {
			continue
		}
		why := RuleSaid(RuleBaseGate, "its fix-red card "+id+" landed: the base is green on the new tip and the episode is closed")
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: st,
			Changes: []Change{change(Merge, setEntry(ctl, nil, FieldFixRedTried))},
			Closes:  []Open{o}, Notes: []Note{decided(o, why, r.who(), s.Now)},
			Moved: "stream " + st + ": the fix-red card " + id + " landed; the base-red episode is closed"})
	}
	return p, 0
}
