package sprint

import (
	"fmt"
	"slices"
	"strings"
)

// The coordinator's hold (docs/SPEC-SPRINT.md section 11, hold and unhold): one verb
// pair for the four things that take cards, a fleet member, a reader, a friend and a
// stream. A hold takes no new cards; what is dealt and not begun is handed back now
// (a member's ready cards dealt round the fleet, a reader's reads asked and not begun
// asked of another, a stream's ready work cards withdrawn); what is begun finishes, or
// with --return is handed back now too. A held friend keeps no card, --return or
// not (FriendTake with Hold: her started ones carry their pushed head to the next taker). The status reads held, the reason beside it,
// and the hold is a line of the log. The member's and the stream's hold live on their
// control cards; the reader's and the friend's, as their presence does, in their
// records outside the tables (store/readers.go, store/friends.go), which the store
// writes after this step commits.

// What a hold names.
const (
	HoldMember = "member"
	HoldReader = "reader"
	HoldFriend = "friend"
	HoldStream = "stream"
)

// FieldHeldReason is a held member's or stream's control card's reason for its hold,
// and FieldHeldFinish marks a member's hold that lets its working cards finish (hold
// with no --return): the rebalance's sweep leaves those cards where they are.
// FieldHeldReturn records a coordinator's hold made with --return (fleet down): the
// operator took the member out of service, so it runs no land gate (gateCapable). It
// carries the hold's own held stamp, so a value left behind by a release that does not
// unset it never marks a later hold (HeldToReturn). held_finish cannot say this: a
// fleet down on an up member lets its working cards finish too.
const (
	FieldHeldReason = "held_reason"
	FieldHeldFinish = "held_finish"
	FieldHeldReturn = "held_return"
)

// HeldByFault is the hold mark (FieldHeldBy) of a member the machine holds for a
// failure: a harness fault, a failed take, or the member going down. Such a hold is
// the machine's own, and the tick clears it by itself once the member proves healthy
// by taking a card and finishing it cleanly (faultClear); a hold the coordinator
// made carries no mark and stays until a person lifts it (fleet up). The model is
// tla/DealFill.tla, FaultHoldClears.
const HeldByFault = "fault"

// HeldByProbe is the hold mark (FieldHeldBy) of a member the sync added but whose
// own probe has not passed yet: a member new to the sprint, added by fleet sync, is
// known only once its own probe passes, so the add writes the control card held (a
// held member's status is held, and presence never brings it up). Only fleet up <m>
// --probe passed lifts it; no other path clears it (docs/SPEC-SPRINT.md section 5,
// the fleet; tla/ProviderBudget.tla, NewMemberProbed).
const HeldByProbe = "probe"

// ProbeHeld says the member is held until its own probe passes (HeldByProbe): the
// sync's add marks it so, and fleet up --probe passed is the one path that lifts it.
func ProbeHeld(ctl *Card) bool {
	return ctl != nil && ctl.F(FieldHeldBy) == HeldByProbe
}

// FieldFaultSince is a fault-held member's control card's stamp of when the machine
// held it: kept through the probe (faultClear) so the tick can tell a clean finish
// after the hold from one before it.
const FieldFaultSince = "fault_since"

// FaultHeld says the member is held by the machine for a failure (HeldByFault), the
// one kind of hold the tick lifts by itself; the coordinator's hold (fleet down, no
// held_by) and the sync's and the adopt's are never cleared by this rule. It is true
// through the probe too: while the member beats again the machine keeps the mark, so
// the clear still knows it is the machine's own hold to lift.
func FaultHeld(ctl *Card) bool {
	return ctl != nil && ctl.F(FieldHeldBy) == HeldByFault
}

// HoldReq is one hold or unhold of names, each a fleet member, a reader, a friend or a
// stream (docs/SPEC-SPRINT.md section 11).
type HoldReq struct {
	Names []string
	// Release is unhold: each name's hold is cleared.
	Release bool
	// Return hands back what each name holds begun, now. An up fleet member keeps its
	// taken cards and finishes them even with Return; a down member's working cards are
	// dead runs and are handed back. Readers and streams keep their existing rules.
	Return bool
	// Kind, when set, is the one kind the names may name (the old verbs, fleet down,
	// reader away and friend down, alias hold for theirs): a name of another kind, or
	// of none, is refused in the old verb's words.
	Kind   string `json:",omitempty"`
	Reason string
	Who    string
	// ForceLast holds the last member that can run a land gate anyway: the hold passes
	// and its note says no gate bench is left (--force-last).
	ForceLast bool `json:",omitempty"`
	// Cause, with a member's hold, is what ends it (hold_cause.go): a judgment as the
	// coordinator named it (j<n>, or its id), with CauseNote the id it named (a
	// doctor:<check> cause is refused: DoctorCauseDeferred). The tick ends the hold in the
	// step that sees the cause end; a hold with none is reported every HoldNoCauseEvery.
	Cause     string `json:",omitempty"`
	CauseNote string `json:",omitempty"`
	// HeldBy, when the hold is the machine's own (HeldByFault), marks it so the tick
	// clears it by itself once the member is healthy; a coordinator's hold leaves it
	// empty and stays until a person lifts it.
	HeldBy string `json:",omitempty"`
	// Friends is the friend roster's names (nova-config's friend rows): the snapshot
	// holds no table of them.
	Friends []string `json:",omitempty"`
	// Alive, with Release, is the members whose beat is alive now: one comes up at once.
	Alive []string `json:",omitempty"`
	// Started is the work cards the friends held have started, as friend down reads them
	// (a push on the branch at its tip, her beat naming it running), each with its why: the
	// hold takes them too, and a push's head is carried (FriendTake). It is read state, so
	// no part of the step's arguments.
	Started map[string]string `json:"-"`
}

// HoldTarget is a name of a hold and what it names.
type HoldTarget struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// HoldView is one hold in force, for where --json and handover: what is held, why, by
// whom and since when.
type HoldView struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
	By     string `json:"by,omitempty"`
	At     string `json:"at,omitempty"`
	// Return says the hold handed back the work begun (--return); else that work
	// finishes.
	Return bool `json:"return,omitempty"`
	// Cause is what ends a member's hold (hold_cause.go), "" for none; Who is who made a
	// coordinator's hold; Release is the verb that ends it by hand.
	Cause   string `json:"cause,omitempty"`
	Who     string `json:"who,omitempty"`
	Release string `json:"release,omitempty"`
}

// HoldTargets is what each name names: a fleet member, a reader, a friend or a stream;
// a name that names none, or more than one, is refused naming every kind it matched.
func HoldTargets(s *Snapshot, r HoldReq) ([]HoldTarget, []Refusal) {
	var out []HoldTarget
	var refused []Refusal
	seen := map[string]bool{}
	for _, n := range r.Names {
		if seen[n] {
			continue
		}
		seen[n] = true
		var kinds []string
		if !IsFriendRow(n) && s.MemberCtl(n) != nil {
			kinds = append(kinds, HoldMember)
		}
		if s.Readers != nil && s.Readers.HasRow(n) {
			kinds = append(kinds, HoldReader)
		}
		if slices.Contains(r.Friends, n) {
			kinds = append(kinds, HoldFriend)
		}
		if s.StreamCtl(n) != nil {
			kinds = append(kinds, HoldStream)
		}
		switch {
		case r.Kind != "" && !slices.Contains(kinds, r.Kind):
			refused = append(refused, Refusal{Key: n, Why: "no " + holdKindWords[r.Kind] + " " + n + holdElsewhere(n, kinds, r.Release)})
		case r.Kind != "":
			out = append(out, HoldTarget{Name: n, Kind: r.Kind})
		case len(kinds) == 0:
			refused = append(refused, Refusal{Key: n, Why: "names no fleet member, reader, friend or stream of this sprint: nova-sprint where --all shows them"})
		case len(kinds) == 1:
			out = append(out, HoldTarget{Name: n, Kind: kinds[0]})
		default:
			refused = append(refused, Refusal{Key: n, Why: "names a " + strings.Join(kinds, " and a ") + ": a hold names one; rename one of them"})
		}
	}
	return out, refused
}

// holdKindWords is each kind a hold names, in words.
var holdKindWords = map[string]string{HoldMember: "fleet member", HoldReader: "reader", HoldFriend: "friend", HoldStream: "stream"}

// holdElsewhere is the rest of a kind's refusal for a name of another kind: what it is
// and the verb that holds or releases it (fleet hold for a member, friend hold for a
// friend, hold for a reader or a stream); "" for a name of no kind.
func holdElsewhere(n string, kinds []string, release bool) string {
	if len(kinds) != 1 {
		return ""
	}
	verb := map[string]string{HoldMember: "fleet hold", HoldFriend: "friend hold"}[kinds[0]]
	if verb == "" {
		verb = "hold"
	}
	tail := " --reason <text>"
	if release {
		verb = strings.Replace(verb, "hold", "unhold", 1)
		tail = ""
	}
	return ": " + n + " is a " + holdKindWords[kinds[0]] + "; run: nova-sprint " + verb + " " + n + tail
}

// gateCapable says a member can run a land gate now: it is up, or held by the coordinator
// with no --return (a hold is a deal control, and a gate is not a card). A member a
// machine holds (held_by or fault_since) is no bench, and neither is one the coordinator
// took down with fleet down (hold --return, HeldToReturn): the operator took it out of
// service. Snapshot.GateMembers (the land gate's benches) and check.go rule 11 count
// the benches by it, and this guard counts the members a hold does not name by it (it
// still counts a named member as gone, and never refuses a --return). tla/DirtyTick.tla LandGate (MCDirtyTickGateHeld.cfg; the reversed witness
// MCDirtyTickBrokenGateReturned.cfg gates a returned member).
func gateCapable(ctl *Card) bool {
	if ctl == nil {
		return false
	}
	if ctl.F("status") == Up {
		return true
	}
	return ctl.F(FieldHeld) != "" && ctl.F(FieldHeldBy) == "" && ctl.F(FieldFaultSince) == "" && !HeldToReturn(ctl)
}

// HeldToReturn says a member's hold is a coordinator's hold made with --return (fleet
// down): its held_return carries the hold's own held stamp (FieldHeldReturn).
func HeldToReturn(ctl *Card) bool {
	return ctl != nil && ctl.F(FieldHeld) != "" && ctl.F(FieldHeldReturn) == ctl.F(FieldHeld)
}

// lastGateGuardOff is the tests' seam: the last-gate guard reads it (true) to show the
// guard a plant removed would be caught by its own test. Only export_test.go sets it,
// and only the plant test, which resets it; no verb and no request carries it.
var lastGateGuardOff bool

// lastGateRefusal is the guard's refusal when a hold would leave no gate bench: the
// member being held, and the consequence with the override that holds it anyway.
func lastGateRefusal(member string) Refusal {
	return Refusal{Key: member, Code: RefusedLastGate, Why: "holding " + member + " leaves no gate bench: every landing will be refused until one is unheld; --force-last holds it anyway"}
}

// HoldNames is hold and unhold as one step (docs/SPEC-SPRINT.md section 11): every name
// resolved first (HoldTargets), all or none; then each kind's part, and a happened
// note per name, the reason in it, so the log holds every hold and release. A dealt
// card moves freely; only a taken card is its worker's and is never pulled back (the
// owner, 2026-10-09; NoTakenCardReturned): an up member's hold --return hands back its
// ready cards alone, and its working cards stay at their generation and finish.
// tla/WhoPreference.tla's NoStartedCardMoves states that rule for started cards.
func HoldNames(s *Snapshot, r HoldReq) Plan {
	targets, refused := HoldTargets(s, r)
	if len(r.Names) == 0 {
		refused = append(refused, Refusal{Key: "hold", Why: "names nothing: name a fleet member, a reader, a friend or a stream"})
	}
	if why := holdCauseWhy(s, r, targets); len(refused) == 0 && why != "" {
		refused = append(refused, Refusal{Key: "hold", Why: why})
	}
	if len(refused) > 0 {
		return Plan{Refused: refused}
	}
	var members []string
	keep := map[string]bool{} // streams held here: their hold withdraws their cards, a member's hold leaves them
	for _, t := range targets {
		switch {
		case t.Kind == HoldMember:
			members = append(members, t.Name)
		case t.Kind == HoldStream && !r.Release:
			keep[t.Name] = true
		}
	}
	// A hold that would leave no member able to run a land gate is refused before any
	// plan is built: every landing would be refused until one is unheld. It fires only
	// when at least one member is outside the hold (a hold naming every member is a
	// deliberate full-fleet action) and every such member is already no bench. The
	// members named here count as gone; release, a member already unable to run a gate,
	// and the old --return maintenance path (fleet down) are never refused. A fleet of
	// one member is never refused (its only member is its routine maintenance hold, a
	// long-standing flow). --force-last records the override on the note instead.
	forceLast := false
	if len(members) > 0 && !r.Release && !r.Return && !lastGateGuardOff && len(s.Members()) >= 2 {
		named := map[string]bool{}
		for _, m := range members {
			named[m] = true
		}
		capable, last := 0, ""
		unnamed := 0
		for _, m := range s.Members() {
			if named[m] {
				continue
			}
			unnamed++
			if gateCapable(s.MemberCtl(m)) {
				capable++
			}
		}
		for _, m := range members {
			if gateCapable(s.MemberCtl(m)) {
				last = m
			}
		}
		if unnamed > 0 && capable == 0 && last != "" {
			if !r.ForceLast {
				return Plan{Refused: []Refusal{lastGateRefusal(last)}}
			}
			forceLast = true
		}
	}
	var p Plan
	add := func(q Plan) {
		p.Rows = append(p.Rows, q.Rows...)
		p.Units = append(p.Units, q.Units...)
		p.Refused = append(p.Refused, q.Refused...)
		p.Notes = append(p.Notes, q.Notes...)
	}
	// every card placed on a member goes round the fleet from the deal's rolling index and
	// moves it (round.go), as a fleet step's does
	rr := dealRoundWith(s, members...)
	moves := roundMoves{}
	// a member quiet, under its floor or draining is given nothing (notQuiet; drain.go)
	up := notQuiet(s, without(s.UpMembers(), members))
	q, widths := memberLoads(s, up), memberWidths(s, up)
	for _, t := range targets {
		var line string
		switch {
		case t.Kind == HoldMember && r.Release:
			add(fleetStepPlan(s, FleetReq{Op: "release", Member: t.Name, Who: r.Who, Fresh: slices.Contains(r.Alive, t.Name), Why: r.Reason}, rr, moves))
		case t.Kind == HoldMember:
			ctl := s.MemberCtl(t.Name)
			// a member is up when its status is up; a member already marked to finish
			// (FieldHeldFinish, a prior hold) keeps that, so a repeat hold never pulls
			// back a card it already let finish (the owner, 2026-10-09)
			upNow := ctl != nil && (ctl.F("status") == Up || ctl.F(FieldHeldFinish) != "")
			taken := s.Fleet.Cell(t.Name, Working)
			finish := !r.Return || upNow
			if r.Return && upNow {
				if len(taken) > 0 {
					ids := make([]string, 0, len(taken))
					for _, c := range taken {
						ids = append(ids, c.ID)
					}
					slices.Sort(ids)
					line = fmt.Sprintf("--return: %d taken cards stay with their worker and finish: %s", len(ids), strings.Join(ids, ","))
				}
			}
			add(downPlan(s, FleetReq{Op: "hold", Member: t.Name, Who: r.Who, Why: holdWhy(r), Reason: r.Reason, HeldBy: r.HeldBy, Finish: finish, Return: r.Return, Cause: r.Cause, CauseNote: r.CauseNote, keep: keep}, up, rr, moves, q, widths))
		case t.Kind == HoldStream:
			var sp Plan
			sp, line = holdStream(s, t.Name, r)
			add(sp)
		case t.Kind == HoldReader && r.Return && !r.Release:
			var rp Plan
			rp, line = returnReads(s, t.Name, r.Who)
			add(rp)
		case t.Kind == HoldFriend && !r.Release:
			var fp Plan
			fp, line = holdFriendCards(s, t.Name, r)
			add(fp)
			if r.Return {
				rp, rline := takeBackFriendReaderReads(s, t.Name, r.Who)
				add(rp)
				if rline != "" {
					line = joinSaid(line, rline)
				}
			}
		}
		p.Notes = append(p.Notes, holdNote(s, t, r, line, forceLast))
	}
	p = Lawful(p)
	roundWrites(&p, rr, moves)
	return p
}

// holdWhy is a member hold's words on its status note: the reason, else who held it.
func holdWhy(r HoldReq) string {
	if r.Reason != "" {
		return r.Reason
	}
	return "held by " + r.Who
}

// holdNote is the happened note of one name held or released: the log's line of the
// hold, the reason in it.
func holdNote(s *Snapshot, t HoldTarget, r HoldReq, line string, forceLast bool) Note {
	typ, word := NHeld, "held"
	if r.Release {
		typ, word = NUnheld, "released from its hold"
	}
	stream := ""
	if t.Kind == HoldStream {
		stream = t.Name
	}
	n := happened(typ, stream, s.Now)
	n.Who = r.Who
	n.What = t.Kind + " " + t.Name + " " + word
	if r.Reason != "" {
		n.What += ": " + r.Reason
	}
	if !r.Release {
		switch {
		case r.Return && t.Kind == HoldMember && line != "":
			n.What += "; --return hands back only what was dealt and not taken"
		case r.Return:
			n.What += "; --return: its work begun is handed back now"
		case t.Kind == HoldFriend:
			n.What += "; every held card, begun or not, is handed back now"
		default:
			n.What += "; its work begun finishes"
		}
	}
	if line != "" {
		n.What += "; " + line
	}
	if forceLast && t.Kind == HoldMember && !r.Release {
		n.What += "; --force-last: no gate bench left"
	}
	return n
}

// StreamHeld says the coordinator holds the stream (hold <stream>): none of its ready
// primaries is dealt, to a machine or a friend, until unhold (docs/SPEC-SPRINT.md section
// 11, hold).
func StreamHeld(s *Snapshot, stream string) bool {
	return s.Merge != nil && s.StreamCtl(stream).F(FieldHeld) != ""
}

// StreamStateText is a stream's state cell: held while the coordinator holds it and it
// is not stopped (a stop is the merge's, and says more), else its control card's state
// (docs/SPEC-SPRINT.md section 11, hold).
func StreamStateText(fields map[string]string) string {
	if fields[FieldHeld] != "" && fields["state"] != StreamStopped {
		return Held
	}
	return fields["state"]
}

// HeldToFinish says a member's hold lets its working cards finish (hold with no
// --return): no sweep moves them, and the no-stall rule counts the member as holding
// them while their deadline runs (docs/SPEC-SPRINT.md sections 5 and 11).
func HeldToFinish(ctl *Card) bool { return ctl.F("held") != "" && ctl.F(FieldHeldFinish) != "" }

// holdStream is a stream's hold or its release: the control card's held stamp and
// reason, and with a hold the stream's work cards ready on members withdrawn (never
// begun, so no redeal is spent), its working ones too with --return (docs/SPEC-SPRINT.md
// section 11, hold).
// tla/Holds.tla Hold
func holdStream(s *Snapshot, stream string, r HoldReq) (Plan, string) {
	var p Plan
	ctl := s.StreamCtl(stream)
	if r.Release {
		if ctl.F(FieldHeld) == "" {
			return p, "it was not held"
		}
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: stream, Changes: []Change{change(Merge, setEntry(ctl, nil, FieldHeld, FieldHeldReason))},
			Moved: "stream " + stream + " released"})
		return p, ""
	}
	set := map[string]string{FieldHeld: stamp(s.Now)}
	var unset []string
	if r.Reason != "" {
		set[FieldHeldReason] = r.Reason
	} else {
		unset = append(unset, FieldHeldReason)
	}
	p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: stream, Changes: []Change{change(Merge, setEntry(ctl, set, unset...))},
		Moved: "stream " + stream + " held"})
	var withdrew []string
	for _, m := range s.Fleet.Rows() {
		cols := []string{Ready}
		if r.Return {
			cols = append(cols, Working)
		}
		for _, col := range cols {
			for _, c := range s.Fleet.Cell(m, col) {
				if c.F("stream") != stream || c.F("kind") != "work" {
					continue
				}
				p.Units = append(p.Units, withdrawCard(s, c, col == Working, NWithdrawn, r.Who, "stream "+stream+" is held"))
				withdrew = append(withdrew, c.ID)
			}
		}
	}
	if len(withdrew) == 0 {
		return p, ""
	}
	return p, fmt.Sprintf("withdrew %d: %s", len(withdrew), Preview(withdrew, ","))
}

// returnReads is a reader's hold with --return: its reads asked or begun are taken back
// (retired, as the readers' sweep takes a reader away's) wherever a reader up is free to
// read them at the next ask; one no reader up can take stays, and is said
// (docs/SPEC-SPRINT.md sections 6 and 11).
func returnReads(s *Snapshot, reader, who string) (Plan, string) {
	var p Plan
	cards := append(append([]*Card{}, s.Readers.Cell(reader, Asked)...), s.Readers.Cell(reader, Reading)...)
	SortCards(cards)
	var back, kept []string
	for _, c := range cards {
		if !readTakerUp(s, c, reader) {
			kept = append(kept, c.ID)
			continue
		}
		back = append(back, c.ID)
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"),
			Changes: []Change{change(Readers, removeEntry(c, map[string]string{"retired": stamp(s.Now), "retired_by": RetiredByHold}))},
			Moved:   fmt.Sprintf("%s %s:%s -> taken back (%s is held); the ask asks it of a reader up", c.ID, reader, c.Col, reader)})
	}
	var parts []string
	if len(back) > 0 {
		parts = append(parts, fmt.Sprintf("took back %d: %s", len(back), Preview(back, ",")))
	}
	if len(kept) > 0 {
		parts = append(parts, fmt.Sprintf("kept %d no reader up is free to read: %s", len(kept), Preview(kept, ",")))
	}
	return p, strings.Join(parts, "; ")
}

// RetiredByHold is a read card's retired_by when the coordinator's hold --return took it
// back from its reader.
const RetiredByHold = "hold"

// readTakerUp says a reader up other than reader, with no read of c's primary at its
// attempt, can be asked the read (the readers' sweep's rule, sweepReads).
func readTakerUp(s *Snapshot, c *Card, reader string) bool {
	if pr := s.Work.Card(c.F("primary")); pr == nil || !enoughReadersUp(s, pr) {
		return false
	}
	for _, rd := range s.UpReaders() {
		if rd != reader && s.Readers.Card(ReadCardID(c.F("primary"), c.Int("attempt"), rd)) == nil {
			return true
		}
	}
	return false
}

// holdFriendCards is a friend's hold: a held friend keeps no card, begun or not (the owner,
// 2026-10-04, on a held friend still showing two working cards: "nonono"), so every card
// she has begun, working on her row or read as started, is taken back as friend down takes
// it (FriendTake with Hold), each primary ready again for the tick to deal to a friend up, a
// started one with its pushed head carried. Her ready cards not begun are taken back too, with
// --return or without: a held friend keeps no card at all, so a dealt card never waits on a
// friend who will not take it (the owner, 2026-10-09; docs/SPEC-SPRINT.md sections 1 and 11, hold).
func holdFriendCards(s *Snapshot, friend string, r HoldReq) (Plan, string) {
	p := FriendTake(s, FriendTakeReq{Friend: friend, All: true, Hold: true, Begun: false, Started: r.Started, Who: r.Who})
	var back, carried []string
	for _, u := range p.Units {
		back = append(back, u.Key)
		if PushedTip(r.Started[u.Key]) != "" {
			carried = append(carried, u.Key)
		}
	}
	if len(back) == 0 {
		return p, ""
	}
	line := fmt.Sprintf("withdrew %d: %s", len(back), Preview(back, ","))
	if len(carried) > 0 {
		line += fmt.Sprintf("; carried the pushed head of %d: %s", len(carried), Preview(carried, ","))
	}
	return p, line
}

// takeBackFriendReaderReads returns a held friend's reader-row reads: reader-<friend>
// (nova-tools pkg/friend ReaderOf) is her reader row, and a friend and her reader row are one
// member to the server, so a hold with --return takes back the reads asked of it too
// (returnReads), where a reader up is free to read them (docs/SPEC-SPRINT.md section
// 11, hold).
func takeBackFriendReaderReads(s *Snapshot, friend, who string) (Plan, string) {
	if s.Readers == nil {
		return Plan{}, ""
	}
	return returnReads(s, ReaderPrefix+friend, who)
}

// joinSaid joins two lines one note says, either empty left out.
func joinSaid(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}
