package sprint

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A card-holding judgment is never left unanswered (docs/SPEC-SPRINT.md section 8, "A
// judgment that holds a card"; the owner, 2026-10-12 ~00:08Z: "This must never happen again.
// Fix it."). At 00:06Z eleven judgments had held their cards 5 to 169 minutes while the seat
// was busy elsewhere and its hook kept lapsing: ready to accept on cards whose two reads had
// passed, returned to review, work came back failed, a card reached its bound, no route
// serves the tier, a card pinned to one friend waits while she is not up. Two rules.
//
// (a) What has a safe default the machine answers without the seat: a card whose reads
// have all passed is accepted by the tick, also after a return that was only for a missing
// read or a needs repair (AcceptHeld, ReturnedFor); a hard pin to a friend not up is offered
// on after PinReleaseBound, the judgment deadline; a card whose tier no route serves is
// raised to the nearest served tier above it once the tier's judgment is past its deadline
// (TickRuleNoRoute), never down, so never flash for heavy.
//
// (b) Everything else that holds a card escalates: past its deadline each such judgment is
// raised again every DeadlineJudgment of running time (a push to the seat, reraiseHeld),
// and one sprint-level judgment, NSeatSilent, names every card held and for how long. It is
// raised again every PassEvery while it holds, the server sends it to the owner on the bus
// as well as on the seat's hook (cmd/nova-sprint, tellSeatSilent), it cannot be waited or
// acknowledged, and it closes when the last held judgment is answered.

// NSeatSilent is the sprint-level judgment that judgments holding cards wait on the seat
// past their deadline.
const NSeatSilent = "the seat is not answering"

// NHeldRaised is the record of a card-holding judgment raised again past its deadline: an
// acknowledged-kind note (shown in no inbox) whose What is the judgment's id and whose
// Before is the deadlines it has been raised at, so each deadline raises it once.
const NHeldRaised = "a judgment holding a card was raised again"

// NPinReleased is the record of a hard pin released at its deadline (releasePins): an
// acknowledged-kind note (shown in no inbox) whose What is the id of the pin-waits stop's
// note it releases. A new stop note (an ack or a wait writes one) has no record, so its
// clock starts again.
const NPinReleased = "a hard pin was released at its deadline"

// NSeatNotMoving is the sprint-level judgment raised once an unchanged judgment has
// been pushed RepeatPushes times: the seat is not moving it, escalate to the owner.
// tla/CoordinatorPass.tla PushesBoundedWhileUnchanged, EscalatedOnce
const NSeatNotMoving = "the seat is not moving this: escalate to the owner"

// SeatNotMovingDecision is the one decision of NSeatNotMoving.
const SeatNotMovingDecision = "answer the judgment it names"

// NOwnerNoTarget is the judgment the server raises when the owner escalation has no
// target that exists; its decisions are ack and wait.
const NOwnerNoTarget = "the owner escalation has no target"

// RepeatPushesDefault is the repeat bound when the sprint row names none.
const RepeatPushesDefault = 3

// RepeatBound is the repeat bound: the sprint row's repeat_pushes as the routes read
// applied it (Snapshot.RepeatPushes), else RepeatPushesDefault. A whole number of at least 1.
// tla/CoordinatorPass.tla PushesBoundedWhileUnchanged
func (s *Snapshot) RepeatBound() int {
	v := strings.TrimSpace(s.RepeatPushes)
	if v == "" {
		return RepeatPushesDefault
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || strconv.Itoa(n) != v {
		return RepeatPushesDefault
	}
	return n
}

// SeatSilentDecision is the one decision of NSeatSilent: answer the judgments it names.
// It offers neither ack nor wait: the alarm is quieted only by the answers.
const SeatSilentDecision = "answer each judgment it names: nova-sprint inbox"

// HoldsACard says a judgment of the type holds a card until it is answered: its type is a
// kind of WaitRefused (wait_refused.go), the one table of the judgments that hold a card
// from progressing. A kind that holds a card only when it names one (a stall,
// WaitRefusedWhenItNamesACard) and the kinds a wait may hold (WaitAllowedOnACard: a
// sentinel, a lateness, a friend's session) are not escalated here.
func HoldsACard(typ string) bool {
	_, ok := WaitRefused[typ]
	return ok
}

// heldBy is one open judgment that holds cards past its deadline: its id and type, the
// cards it holds, and how long it has been open in running time.
type heldBy struct {
	id, typ string
	cards   []string
	age     time.Duration
	note    Note
}

// heldPastDeadline is every open card-holding judgment past its deadline, the oldest first.
func heldPastDeadline(s *Snapshot, r TickReq) []heldBy {
	seen := map[string]bool{}
	var out []heldBy
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment || !HoldsACard(n.Type) || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		if !JudgmentOverdue(s, r, n) {
			continue
		}
		age, ok := r.running(s.Now, stamp(n.At))
		if !ok {
			continue
		}
		cards := cardsHeldBy(n)
		if len(cards) > 0 && s.everyHeldCardTierBlocked(cards) {
			// reraiseHeld and seatSilentConds skip a judgment whose every held card is tier-blocked
			// tla/SprintRules.tla QuietWhileUnfunded
			continue
		}
		out = append(out, heldBy{id: n.ID, typ: n.Type, cards: cards, age: age, note: n})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].age > out[j].age })
	return out
}

// cardsHeldBy is the cards a judgment holds: its primaries, else the card it names, else its
// subject.
func cardsHeldBy(n Note) []string {
	switch {
	case len(n.Primaries) > 0:
		return append([]string(nil), n.Primaries...)
	case n.Card != "":
		return []string{n.Card}
	}
	return n.Subjects()
}

// seatSilentConds is the one condition NSeatSilent: every card-holding judgment past its
// deadline, by the cards it holds and for how long, the oldest first.
func seatSilentConds(s *Snapshot, r TickReq) []cond {
	held := heldPastDeadline(s, r)
	if len(held) == 0 {
		return nil
	}
	cards := map[string]bool{}
	var parts []string
	for _, h := range held {
		for _, c := range h.cards {
			cards[c] = true
		}
		// by id, as the overdue line names it: an alias is the store's, not the decision's
		parts = append(parts, fmt.Sprintf("%s held %s by %s (%s)", Preview(h.cards, ","), h.age.Truncate(time.Minute), h.id, h.typ))
	}
	ids := slices.Sorted(maps.Keys(cards))
	what := fmt.Sprintf("the seat is not answering: %d judgments hold %d cards past their deadline (%s): %s; answer each: nova-sprint inbox; this cannot be waited or acknowledged, it clears when they are answered",
		len(held), len(ids), DeadlineJudgment, Preview(parts, "; "))
	return []cond{{typ: NSeatSilent, streamLevel: true, primaries: ids, what: what, decisions: []string{SeatSilentDecision}}}
}

// heldFingerprint is the state a raise is counted against: each held card's column,
// attempt, tier and result, its stream's control state, and the stream's counts by
// column. A change in any of it resets the repeat count.
// tla/CoordinatorPass.tla PushesBoundedWhileUnchanged
func heldFingerprint(s *Snapshot, stream string, cards []string) string {
	var parts []string
	sortedCards := append([]string(nil), cards...)
	sort.Strings(sortedCards)
	for _, id := range sortedCards {
		c := s.Work.Placed(id)
		if c == nil {
			parts = append(parts, id+" not placed")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s in %s (attempt %s, tier %s, result %s)", id, c.Col, orDash(c.F("attempt")), orDash(c.F("tier")), orDash(c.F("result"))))
	}
	ctl := s.StreamCtl(stream)
	parts = append(parts, fmt.Sprintf("stream %s control %s/%s", stream, orDash(ctl.F("state")), orDash(ctl.F("cause"))))
	if s.Work != nil {
		counts := map[string]int{}
		for _, c := range s.Work.Cards() {
			if c != nil && c.Row == stream && c.Placed() {
				counts[c.Col]++
			}
		}
		var cols []string
		for _, col := range slices.Sorted(maps.Keys(counts)) {
			cols = append(cols, fmt.Sprintf("%s=%d", col, counts[col]))
		}
		parts = append(parts, "counts "+strings.Join(cols, ","))
	}
	return strings.Join(parts, "; ")
}

// raiseRecordState is the fingerprint and unchanged count a judgment's latest
// NHeldRaised record (its Hint) carries; ok false when it carries none.
func raiseRecordState(records map[string][]Open, id string) (string, int, bool) {
	bestK := -1
	fp, count, ok := "", 0, false
	for _, o := range records[id] {
		if o.Note.Before < bestK {
			continue
		}
		bestK = o.Note.Before
		f, c, parsed := strings.Cut(o.Note.Hint, "; unchanged=")
		if !parsed {
			continue
		}
		n, err := strconv.Atoi(c)
		if err != nil {
			continue
		}
		fp, count, ok = strings.TrimPrefix(f, "fp="), n, true
	}
	return fp, count, ok
}

// seatNotMovingNote is the one escalation of an unchanged judgment: it names the
// judgment, its type, its cards, the count and the unchanged state in words.
// tla/CoordinatorPass.tla EscalatedOnce
func seatNotMovingNote(s *Snapshot, r TickReq, n Note, id, typ string, cards []string, count int, fp string) Note {
	return Note{Kind: Judgment, Type: NSeatNotMoving, Stream: n.Stream, Primaries: append([]string(nil), cards...), Count: len(cards),
		SprintLevel: true, Card: id, Who: r.who(), At: s.Now, Hint: fp, Decisions: []string{SeatNotMovingDecision},
		What: fmt.Sprintf("the seat is not moving this: %s (%s) still holds %s after %d unchanged pushes (state: %s); escalate to the owner; answer the judgment it names", id, typ, Preview(cards, ", "), count, fp)}
}

// reraiseHeld raises again every card-holding judgment past its deadline, once each
// DeadlineJudgment of running time: the k-th deadline after it was written raises it the
// k-th time (the first is the overdue line, tickOverdue), as a push to the seat
// (NRaisedAgain), with the record of it (NHeldRaised) so a deadline raises it once. A
// record whose judgment is no longer open is closed. The pass's own judgments and the
// automatic stops are raised again by the pass and its digest, not here.
func reraiseHeld(s *Snapshot, r TickReq) Plan {
	var p Plan
	records := map[string][]Open{} // judgment id -> its raise record's open entries
	raised := map[string]int{}
	for _, o := range s.Acked {
		if o.Note.Type != NHeldRaised {
			continue
		}
		records[o.Note.What] = append(records[o.Note.What], o)
		raised[o.Note.What] = max(raised[o.Note.What], o.Note.Before)
	}
	to := s.Coordinator
	if to == "" {
		to = "coordinator"
	}
	open := map[string]bool{}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment {
			open[o.Note.ID] = true
		}
	}
	// an escalation whose judgment closed, or whose state changed, closes
	byID := map[string]Note{}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment {
			byID[o.Note.ID] = o.Note
		}
	}
	escalated := map[string]bool{}
	for _, o := range s.Open {
		if o.Note.Type != NSeatNotMoving {
			continue
		}
		named := o.Note.Card
		jn, ok := byID[named]
		if !ok {
			p.Closes = append(p.Closes, o)
			continue
		}
		if heldFingerprint(s, jn.Stream, cardsHeldBy(jn)) != o.Note.Hint {
			p.Closes = append(p.Closes, o)
			continue
		}
		escalated[named] = true
	}
	for _, h := range heldPastDeadline(s, r) {
		if slices.Contains(PassTypes, h.typ) {
			continue
		}
		k := int(h.age / DeadlineJudgment)
		if k <= max(raised[h.id], 1) {
			continue
		}
		n := h.note
		// the repeat bound: at most RepeatBound unchanged pushes, then one
		// escalation and no further pushes until the state changes
		// (tla/CoordinatorPass.tla PushesBoundedWhileUnchanged, EscalatedOnce)
		fp := heldFingerprint(s, n.Stream, h.cards)
		prevFP, prevCount, hasPrev := raiseRecordState(records, h.id)
		count := prevCount
		if !hasPrev || prevFP != fp {
			count = 0
		}
		if count >= s.RepeatBound() {
			if !escalated[h.id] {
				p.Notes = append(p.Notes, seatNotMovingNote(s, r, n, h.id, h.typ, h.cards, s.RepeatBound(), fp))
				escalated[h.id] = true
			}
			continue
		}
		p.Closes = append(p.Closes, records[h.id]...)
		rec := Note{Kind: Acknowledged, Type: NHeldRaised, Stream: n.Stream, Primaries: n.Primaries, Count: n.Count,
			StreamLevel: n.StreamLevel, SprintLevel: n.SprintLevel, What: h.id, Before: k, Who: r.who(), At: s.Now,
			Hint: fmt.Sprintf("fp=%s; unchanged=%d", fp, count+1)}
		push := Note{Kind: Happened, Type: NRaisedAgain, Stream: n.Stream, Primaries: h.cards, Count: len(h.cards), Who: r.who(), To: to, At: s.Now,
			What: fmt.Sprintf("%s (%s) still holds %s, open since %s, %s: raised again at its deadline (%d); answer it", h.id, h.typ, Preview(h.cards, ", "), stamp(n.At), h.age.Truncate(time.Second), k),
			Hint: "run: nova-sprint inbox; a judgment that holds a card is answered, never left"}
		p.Notes = append(p.Notes, rec, push)
	}
	for id, os := range records {
		if !open[id] {
			p.Closes = append(p.Closes, os...)
		}
	}
	return p
}

// seatSilentRefusal is why a wait or an acknowledgement of the note is refused: NSeatSilent
// is quieted only by the answers to the judgments it names. "" for any other type.
func seatSilentRefusal(typ, verb string) string {
	if typ == NSeatNotMoving {
		return NSeatNotMoving + " cannot be " + verb + ": it clears when the judgment it names is answered; run: nova-sprint inbox"
	}
	if typ != NSeatSilent {
		return ""
	}
	return NSeatSilent + " cannot be " + verb + ": it clears when every judgment it names is answered; run: nova-sprint inbox"
}

// SeatSilentRefusal is seatSilentRefusal for the store's wait (store/epoch.go).
func SeatSilentRefusal(typ, verb string) string { return seatSilentRefusal(typ, verb) }

// RuleNoRoute is the rule that raises a card whose tier no route serves to the nearest
// served tier above it (TickRuleNoRoute). Like RulePaths it is not in RuleNames: nova-config's
// answer_rules_off enum does not name it, so only run --answer-rules=false turns it off.
const RuleNoRoute = "no-route"

// PartRuleNoRoute is the tick's part that applies RuleNoRoute.
const PartRuleNoRoute = "rule no-route"

// TickRuleNoRoute answers "no route serves the tier" once it is past its deadline: each
// ready primary it holds is raised to the nearest tier above its own that a route (or a
// friend up) serves, pinned there (FieldTier, as rework --tier pins it), and the seat is
// told the change in a note naming the card and both tiers. Never down: a card is never
// moved to a weaker tier than the one no route serves, so a heavy card never goes to
// flash; a card with no served tier above it (heavy, frontier), a friend's card, a brief
// defect and a card whose tier is pinned (pinnedTier: the coordinator pinned it with
// brief --tier, tier --set or rework --tier, its brief pins a model, or it is frontier)
// stay held, and the alarm names them: a pin is the coordinator's word, and the rule never
// overrides it.
// tla/SprintRules.tla NoRouteRaise
func TickRuleNoRoute(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if !r.AnswerRules || s.Work == nil {
		return p, 0
	}
	to := s.Coordinator
	if to == "" {
		to = "coordinator"
	}
	seen := map[string]bool{}
	done := map[string]bool{}
	for _, o := range s.Open {
		n := o.Note
		if n.Kind != Judgment || n.Type != NNoRoute || seen[n.ID] || !JudgmentOverdue(s, r, n) {
			continue
		}
		seen[n.ID] = true
		from := strings.TrimPrefix(n.Stream, TierSubject(""))
		for _, id := range n.Primaries {
			pr := s.Work.Placed(id)
			if pr == nil || done[id] || pr.Col != Ready || IsSentinel(pr) || mindCard(pr) != "" {
				continue
			}
			if m, bad := modelOf(pr); bad != "" || pinnedTier(pr, m) {
				continue // a pinned tier is the coordinator's: it stays put, held
			}
			up := servedAbove(s, pr, from)
			if up == "" {
				continue
			}
			done[id] = true
			said := fmt.Sprintf("%s re-tiered from %s to %s: no route served %s past the deadline (%s) of %s", id, from, up, from, DeadlineJudgment, n.ID)
			p.Units = append(p.Units, Unit{Key: pr.ID, Stream: pr.Row,
				Changes: []Change{change(Work, setEntry(pr, map[string]string{FieldTier: up, FieldRuleAnswer: RuleNoRoute + ": re-tiered " + from + " -> " + up + " at " + stamp(s.Now)}))},
				Moved:   RuleSaid(RuleNoRoute, said),
				Notes: []Note{{Kind: Happened, Type: NRuleAnswered, Stream: pr.Row, Primaries: []string{id}, Count: 1, Who: r.who(), To: to, At: s.Now,
					What: RuleSaid(RuleNoRoute, said)}}})
		}
	}
	return p, 0
}

// servedAbove is the nearest tier of the ladder above from that a route, or a friend up,
// serves for the primary; "" when none does.
func servedAbove(s *Snapshot, pr *Card, from string) string {
	i := slices.Index(tierLadder, from)
	if i < 0 {
		return ""
	}
	for _, t := range tierLadder[i+1:] {
		if _, why := s.noRoute(withField(withField(pr, FieldTier, t), FieldTierNow, t)); why == "" {
			return t
		}
	}
	return ""
}

// SeatAccepts says the primary's stream is set to seat-accept (FieldSeatAccept): its cards
// are accepted by the seat's hand, so a card whose reads have all passed is "ready to accept"
// rather than accepted by the tick.
func SeatAccepts(s *Snapshot, c *Card) bool {
	ctl := s.StreamCtl(c.Row)
	return ctl != nil && ctl.F(FieldSeatAccept) == SwitchOn
}

// seatAcceptNotes is the "ready to accept" judgment of each primary in review that the pump
// would accept but for its stream's seat-accept (SeatAccepts), and that no open judgment
// holds already: the seat is asked once, whether the setting came before its reads or after.
func seatAcceptNotes(s *Snapshot, r TickReq) []Note {
	var out []Note
	for _, c := range s.Work.Column(Review) {
		if !SeatAccepts(s, c) || c.F("result") == "failed" || !acceptable(s, c) || AcceptHeld(c) != "" || LandRefusedAtHead(c) {
			continue
		}
		if len(closesFor(s.Open, nil, c.ID)) > 0 {
			continue // a judgment open on it holds it for the seat already
		}
		n := judgment(NReadyToAccept, c.Row, s.Now, 0, c.ID)
		n.Who, n.Attempt = r.who(), c.Int("attempt")
		n.What = "its stream " + c.Row + " is set to seat-accept: the seat accepts it"
		out = append(out, n)
	}
	return out
}

// releasePins records each hard pin whose pin-waits stop (NStopPinWaits) has stood, open or
// acknowledged, PinReleaseBound or longer of running time (r.running: the STOPPED spans do
// not count), once per stop note (NPinReleased); PinReleased reads the record. A record whose
// stop note is gone (the friend came up, the card moved on) is closed.
// tla/SprintRules.tla PinReleased
func releasePins(s *Snapshot, r TickReq) Plan {
	var p Plan
	records := map[string][]Open{}
	for _, o := range s.Acked {
		if o.Note.Type == NPinReleased {
			records[o.Note.What] = append(records[o.Note.What], o)
		}
	}
	stops := map[string]bool{}
	for _, os := range [][]Open{s.Open, s.Acked} {
		for _, o := range os {
			n := o.Note
			if n.Type != NStopPinWaits || n.At.IsZero() {
				continue
			}
			stops[n.ID] = true
			if len(records[n.ID]) > 0 {
				continue
			}
			age, ok := r.running(s.Now, stamp(n.At))
			if !ok || age < PinReleaseBound {
				continue
			}
			rec := Note{Kind: Acknowledged, Type: NPinReleased, Stream: n.Stream, Primaries: []string{o.Subject()}, Count: 1, What: n.ID, Who: r.who(), At: s.Now}
			records[n.ID] = append(records[n.ID], Open{Note: rec})
			p.Notes = append(p.Notes, rec)
		}
	}
	for id, os := range records {
		if stops[id] {
			continue
		}
		for _, o := range os {
			if o.Note.ID != "" {
				p.Closes = append(p.Closes, o)
			}
		}
	}
	return p
}

// pinReleaseRecorded says the tick recorded the release of the pin-waits stop note id
// (releasePins).
func pinReleaseRecorded(s *Snapshot, id string) bool {
	for _, o := range s.Acked {
		if o.Note.Type == NPinReleased && o.Note.What == id {
			return true
		}
	}
	return false
}
