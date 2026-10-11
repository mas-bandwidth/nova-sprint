package sprint

import (
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// The lifecycle of a primary: six states and the legal moves between them,
// in one table (docs/SPEC-SPRINT.md section 3). The TLA+ model
// tla/SprintTables.tla has the same states and the same moves; compare the
// two row by row.
//
// A primary's state is the work-table column it is placed in. A primary that
// stops any other way than landing leaves the table (drop): its record,
// outcome and reason are kept, unplaced.

// State is a primary's state: its column in the work table.
type State = string

// The six states. Landed is final.
const (
	Waiting State = "waiting"
	Ready   State = "ready"
	Working State = "working"
	Review  State = "review"
	Merging State = "merging"
	Landed  State = "landed"
)

// States is every state, in the work table's column order.
var States = []State{Waiting, Ready, Working, Review, Merging, Landed}

// The class of a move: mechanical moves need no decision; the coordinator's
// moves are made only by the coordinator's verbs.
const (
	Mechanical  = "mechanical"
	Coordinator = "coordinator"
)

// Move is one legal move of a primary.
type Move struct {
	From, To State
	Verb     string // the verb that makes it
	Class    string // Mechanical or Coordinator
	Cause    string
}

// Moves is the lifecycle: every legal move, and nothing else. Off the table
// (drop) is legal from every open state and is not a state.
var Moves = []Move{
	{Waiting, Ready, "resolve", Mechanical, "everything it needs has landed"},
	{Ready, Working, "deal", Mechanical, "a work card is cut and dealt"},
	{Working, Review, "finish", Mechanical, "its work card finished, ok or failed"},
	{Working, Ready, "withdraw", Mechanical, "its work card was withdrawn, its attempt unchanged: no fleet member is up (fleet down), its route rests, its take ended or its staging was refused, its provider rate-limited it, its lane cap redealt it, its friend returned it, or a finished friend card is redealt"},
	{Review, Merging, "accept", Coordinator, "the readers it needs said ok at this head (one for a flash card, two different for a pro card)"},
	{Review, Working, "rework", Coordinator, "rework with a fix: the next attempt is delegated at once to an up member"},
	{Review, Ready, "rework", Coordinator, "rework with a fix when no fleet member is up: start delegates it later"},
	{Merging, Review, "return", Coordinator, "the stream's CI went red and the coordinator sent it back, or return"},
	{Merging, Working, "redo", Coordinator, "redo a conflicted card with the tip's rework: delegated at once to an up member"},
	{Merging, Ready, "redo", Coordinator, "redo a conflicted card when no fleet member is up: start delegates it later"},
	{Merging, Landed, "merge", Mechanical, "its batch, green on the stream branch, merged to the development branch"},
	{Waiting, Landed, "release", Coordinator, "a sentinel reached, or with nothing before it, released by the coordinator (kind sentinel only)"},
	{Ready, Waiting, "add", Mechanical, "a sentinel inserted in front of it, or its named needs now hold one not landed: a brief edit's DEPENDS-ON, needs set, or the tick's needs withdrawal"},
}

// The lifecycle's rows are the moves the code makes, and the model's actions are
// the same moves (tla/SprintTables.tla; docs/SPEC-SPRINT.md section 3, the
// inventory). Two moves are pending PR #96 and are not rows here: working ->
// waiting (a dealt card whose needs have not landed, needsWithdrawals) and the
// tick's ready -> waiting pass for a ready card with a need not landed (needsWaits;
// the needs row above already admits it). The model carries them behind its
// Pending96 constant.

// Legal says from -> to is a move of the lifecycle.
func Legal(from, to State) bool {
	return slices.ContainsFunc(Moves, func(m Move) bool { return m.From == from && m.To == to })
}

// IsOpen says a primary in s has not landed: drop may take it off the table.
func IsOpen(s State) bool {
	switch s {
	case Waiting, Ready, Working, Review, Merging:
		return true
	}
	return false
}

// Lawful holds a plan to the lifecycle: a primary is admitted waiting or
// ready, every unit that moves a primary in the work table moves it by a row
// of Moves, and a primary leaves the table only from an open state. A primary
// is admitted ready, or moves waiting -> ready, only when every one of its
// needs has landed (before the step or in it) or was waived, judged against
// the pre-state the plan was built on; a plan that carries none moves no
// primary into ready. A unit that does not is refused, naming the move; it is
// never applied. The store holds every plan to it before applying it,
// whatever step built it.
//
// It judges in two passes: first the lifecycle on every unit, then the needs
// rule, against the landings of the units the lifecycle kept only: a landing
// the lifecycle refuses satisfies nothing. A move in the work table whose
// expectation names no place is judged from the place the pre-state holds the
// card at, and is refused when the pre-state does not hold it: no entry skips
// the lifecycle or the needs rule by leaving its place out.
func Lawful(p Plan) Plan {
	if p.drained {
		return p
	}
	var lawful []Unit
	for _, u := range p.Units {
		if why := unlawful(u, &p); why != "" {
			p.refuse(u.Key, why)
			continue
		}
		lawful = append(lawful, u)
	}
	landing := map[string]bool{}
	for _, u := range lawful {
		for _, c := range u.Changes {
			if c.Table == Work && c.Entry.Move != nil && c.Entry.Move.Col == Landed {
				landing[c.Entry.ID] = true
			}
		}
	}
	var kept []Unit
	for _, u := range lawful {
		if why := unmet(u, p.pre, landing); why != "" {
			p.refuse(u.Key, why)
			continue
		}
		kept = append(kept, u)
	}
	p.Units = kept
	return p
}

// fromCol is the column a work-table entry moves its card from: the place its
// expectation names, else the place the pre-state holds the card at; false
// when neither says.
func fromCol(e ntable.BatchMemberEntry, pre *Snapshot) (string, bool) {
	if e.Expect != nil && e.Expect.Place != nil {
		return e.Expect.Place.Col, true
	}
	if pre == nil {
		return "", false
	}
	c := pre.Work.Placed(e.ID)
	if c == nil {
		return "", false
	}
	return c.Col, true
}

// unmet is why a unit admits a primary ready, or moves one waiting -> ready,
// with a need that has not landed and was not waived; "" when it does not.
// tla/CardISA.tla Wait: a card is resolved to ready only when its operand holds.
func unmet(u Unit, pre *Snapshot, landing map[string]bool) string {
	for _, c := range u.Changes {
		e := c.Entry
		if c.Table != Work {
			continue
		}
		var needs, waived []string
		switch {
		case e.Create != nil && e.Create.Col == Ready:
			needs, waived = Split(e.Set["needs"]), Split(e.Set["waived"])
			if pre != nil && e.Set["kind"] != "sentinel" {
				if st := StopBefore(pre, e.Create.Row, e.Create.Score, landing); st != nil {
					needs = append(needs, st.ID) // behind a stop: it waits
				}
			}
		case e.Move != nil && e.Move.Col == Ready && waitingFrom(e, pre):
			if pre == nil {
				return "a move waiting -> ready is judged against the step's pre-state, and this plan carries none"
			}
			pc := pre.Work.Card(e.ID)
			needs, waived = Split(pc.F("needs")), append(Split(pc.F("waived")), Split(e.Set["waived"])...)
			// a unit that rewrites the card's needs and moves it ready in one
			// step (needs set, steps_edit.go) is judged by the new needs, not
			// the ones the pre-state holds.
			if v, ok := e.Set["needs"]; ok {
				needs = Split(v)
			} else if slices.Contains(e.Unset, "needs") {
				needs = nil
			}
			needs = append(needs, PositionWaits(pre, pc, landing)...)
		case e.Move != nil && e.Move.Col == Working:
			// a card is dealt only when every need has landed (docs/SPEC-SPRINT.md,
			// needs and the deal): judge the move to dealt against the card's stored
			// needs, whichever path asks for it, so Lawful refuses the deal.
			if pre == nil {
				continue
			}
			pc := pre.Work.Card(e.ID)
			if pc == nil {
				continue
			}
			needs, waived = Split(pc.F("needs")), Split(pc.F("waived"))
		default:
			continue
		}
		var open []string
		for _, n := range needs {
			if contains(waived, n) || landing[n] || pre != nil && pre.StateOf(n) == Landed {
				continue
			}
			open = append(open, n)
		}
		if len(open) > 0 {
			return e.ID + " needs " + strings.Join(open, ",") + ", not landed: it waits"
		}
	}
	return ""
}

// waitingFrom says a work-table entry moves its card from waiting, by its
// expectation or, when that names no place, by the pre-state; a move from a
// place nothing names is taken as from waiting, so the needs rule judges it.
func waitingFrom(e ntable.BatchMemberEntry, pre *Snapshot) bool {
	from, ok := fromCol(e, pre)
	return !ok || from == Waiting
}

func unlawful(u Unit, p *Plan) string {
	// tla/CardISA.tla IsaLandEvent: the release lands from waiting only a
	// sentinel, and a sentinel moves only to landed; the other landing rows
	// (merge) are the only path to landed.
	for _, c := range u.Changes {
		e := c.Entry
		if c.Table == Work && e.Create != nil && e.Create.Col != Waiting && e.Create.Col != Ready {
			return "the lifecycle admits a primary waiting or ready, not " + e.Create.Col
		}
		if c.Table == Work && e.Create != nil && e.Set["kind"] == "sentinel" && e.Create.Col != Waiting {
			return "the lifecycle admits a sentinel waiting"
		}
		if c.Table != Work || e.Create != nil {
			continue
		}
		from, known := fromCol(e, p.pre)
		if !known {
			if e.Remove || e.Move != nil && (e.Expect == nil || e.Expect.Place == nil) {
				return "a move of " + e.ID + " in the work table names no place it moves from, and the step's pre-state does not hold it"
			}
			continue
		}
		sentinel := p.pre != nil && IsSentinel(p.pre.Work.Card(e.ID))
		to := ""
		if e.Move != nil && e.Move.Col != from {
			to = e.Move.Col
		}
		switch {
		case from == Waiting && to == Landed && (!p.releasing || !sentinel):
			return "the lifecycle lands from waiting only a sentinel, and only by release"
		case from == Ready && to == Waiting && !p.inserting && !needsOpen(e, p.pre):
			return "the lifecycle moves ready -> waiting only as the effect of inserting a sentinel, or with a named need not landed"
		case from == Working && to == Waiting && !namedNeedsOpen(e.ID, p.pre):
			// PR #96's move (a dealt card whose needs have not landed waits again) is
			// judged by the card's own named needs in the pre-state, never by the
			// needs the entry writes: a manifest that writes needs cannot move a
			// working card whose needs have all landed (tla/SprintTables.tla
			// WorkingToWaitingOnlyOnNeeds, witness "withdrawmet")
			return "the lifecycle moves working -> waiting only when the card's named needs have not landed"
		case sentinel && to != "" && to != Landed:
			return "a sentinel moves only waiting -> landed, by release"
		}
		switch {
		case e.Remove && !IsOpen(from) && !p.removing:
			return "the lifecycle has no move off the table from " + from
		case e.Move != nil && e.Move.Col != from && !Legal(from, e.Move.Col):
			return "the lifecycle has no move " + from + " -> " + e.Move.Col
		}
	}
	return ""
}

// needsOpen says a move ready -> waiting carries its cause: the needs the entry
// writes name one that has not landed and is not waived (docs/SPEC-SPRINT.md
// section 3: a brief edit, needs set or the tick's needs withdrawal gives it a
// need not landed). It is judged against the step's pre-state; a plan that
// carries none is judged by the needs it writes alone, as before.
// tla/SprintTables.tla ReadyToWaitingOnlyByInsertOrNeeds; its reversed witness
// "needsany" moves a ready card whose needs have all landed.
func needsOpen(e ntable.BatchMemberEntry, pre *Snapshot) bool {
	needs := Split(e.Set["needs"])
	if pre == nil {
		return len(needs) > 0
	}
	var waived []string
	if pc := pre.Work.Card(e.ID); pc != nil {
		waived = Split(pc.F("waived"))
	}
	waived = append(waived, Split(e.Set["waived"])...)
	for _, n := range needs {
		if !contains(waived, n) && pre.StateOf(n) != Landed {
			return true
		}
	}
	return false
}

// namedNeedsOpen says the card's named needs in the pre-state hold one that
// has not landed and is not waived; false when the plan carries no pre-state
// or the pre-state does not hold the card (nothing to judge by, so the move
// that needs it is refused).
func namedNeedsOpen(id string, pre *Snapshot) bool {
	if pre == nil {
		return false
	}
	pc := pre.Work.Card(id)
	if pc == nil {
		return false
	}
	waived := Split(pc.F("waived"))
	for _, n := range Split(pc.F("needs")) {
		if !contains(waived, n) && pre.StateOf(n) != Landed {
			return true
		}
	}
	return false
}

// Rejudge holds entries a repair is about to apply one by one to the
// lifecycle, together, against a fresh read (pre): each entry is its own
// unit, and an entry left out (one that will be skipped) counts as not
// happening, so a landing that is skipped satisfies no need. verb is the
// operation's verb: release may land a sentinel, add may move a primary
// ready -> waiting. It returns the entries refused, by card id, with why.
func Rejudge(pre *Snapshot, verb string, changes []Change) []Refusal {
	p := Plan{pre: pre, releasing: verb == "release", inserting: verb == "add", drained: verb == DrainVerb}
	for _, c := range changes {
		p.Units = append(p.Units, Unit{Key: c.Entry.ID, Changes: []Change{c}})
	}
	return Lawful(p).Refused
}
