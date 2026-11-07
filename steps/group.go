package steps

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// group is one producer's move with the consumers it touches (a preemption
// with its deal, a report with its Finished, a drop with its dependants), as
// one card.Merge: the unit the packer orders and never splits across ops
// (list item 4; OneGroupPerCard, GroupWhole). Its key is what it claims:
// every card its plan writes plus the card it is about, with the gens it
// read them at; a later step skips a claimed card until the next tick.
type group struct {
	kind    string // the step's name for it: pre, start, read, yes, fix, resolve, reached, drop, landed, red, park, withdraw, block, gone, report, take, none
	about   string // the card or row it is about
	plan    card.Plan
	key     GroupKey
	blocker bool          // a blocker's group: never cut by the budget, first in order
	edges   []EdgeRequest // dispatched only with this group's op (EdgeAfterAccepted)
	judge   []store.Write // judgement writes that ride in this group's op
	none    bool          // an inbox item consumed with no move
	answer  *RowAnswer    // the answer to the item, out only with the group
}

// size is the cards the group touches: the budget's unit.
func (g group) size() int { return len(g.key.Cards) }

// move is one layer 2 move, planned on the view's records.
type move struct {
	label string
	plan  func() (card.Plan, *card.Refusal)
}

// groupOp composes a group's moves as one plan. Today each move plans on the
// view and card.Merge composes the plans sequentially (card round 4); card
// round 7 makes Merge take the moves themselves, each planned against the
// folded state, and this is the one place that changes. Built against
// card.Merge of v2-card-fix4 (b2b0b8c2).
func groupOp(moves ...move) (card.Plan, *card.Refusal) {
	plans := make([]card.Plan, 0, len(moves))
	for _, m := range moves {
		p, r := m.plan()
		if r != nil {
			if len(moves) == 1 {
				return card.Plan{}, r
			}
			return card.Plan{}, &card.Refusal{Reason: m.label + ": " + r.Reason}
		}
		plans = append(plans, p)
	}
	if len(plans) == 1 {
		return plans[0], nil
	}
	return card.Merge(plans...)
}

// newGroup is a group of kind about card, from moves: its key is every card
// the plan writes and about, each with the gen the plan fenced it at (0 for a
// card the plan creates). A refusal is a defect in the step's pre-checks
// (the steps check every store rule before planning, RefusedOnlyByRace);
// the caller opens an engine judgement with it.
func (t *tick) newGroup(kind, about string, moves ...move) (group, *card.Refusal) {
	plan, r := groupOp(moves...)
	if r != nil {
		return group{}, r
	}
	g := group{kind: kind, about: about, plan: plan}
	g.key = keyOf(plan, about)
	g.key.Kind = kind
	return g, nil
}

// keyOf is the claim of a plan: the cards it writes plus about, in id order,
// with the gens it read them at.
func keyOf(plan card.Plan, about string) GroupKey {
	k := GroupKey{Gens: map[string]int{}}
	cards := set{}
	if about != "" {
		cards[about] = struct{}{}
	}
	for _, w := range plan.Writes {
		if id, row := written(w); id != "" {
			cards[id] = struct{}{}
		} else if row != "" {
			k.Row = row
		}
	}
	k.Cards = sorted(cards)
	for _, e := range plan.Expect {
		if cg, ok := e.(store.CardGen); ok && slices.Contains(k.Cards, cg.Card) {
			k.Gens[cg.Card] = cg.Gen
		}
	}
	if len(k.Gens) == 0 {
		k.Gens = nil
	}
	return k
}

// written is the card, or the row, a write is about.
func written(w store.Write) (card, row string) {
	switch x := w.(type) {
	case store.PutCard:
		return x.ID, ""
	case store.SetCardState:
		return x.Card, ""
	case store.SetCardResult:
		return x.Card, ""
	case store.SetCardAttempt:
		return x.Card, ""
	case store.SetCardBranch:
		return x.Card, ""
	case store.SetCardHead:
		return x.Card, ""
	case store.SetCardReason:
		return x.Card, ""
	case store.SetCardScore:
		return x.Card, ""
	case store.SetCardAuthor:
		return x.Card, ""
	case store.SetCardBaseSHA:
		return x.Card, ""
	case store.SetCardNeeds:
		return x.Card, ""
	case store.SetCardAfter:
		return x.Card, ""
	case store.SetCardPriorHead:
		return x.Card, ""
	case store.SetCardBounces:
		return x.Card, ""
	case store.SetCardReview:
		return x.Card, ""
	case store.SetCardFinding:
		return x.Card, ""
	case store.SetCardRed:
		return x.Card, ""
	case store.SetCardCost:
		return x.Card, ""
	case store.SetCardUsage:
		return x.Card, ""
	case store.SetCardPriority:
		return x.Card, ""
	case store.SetCardPaths:
		return x.Card, ""
	case store.SetCardDoneWhen:
		return x.Card, ""
	case store.SetCardProse:
		return x.Card, ""
	}
	return "", writtenRow(w)
}

func writtenRow(w store.Write) string {
	switch x := w.(type) {
	case store.SetRowState:
		return x.Row
	case store.SetRowFlaps:
		return x.Row
	case store.SetRowHeld:
		return x.Row
	}
	return ""
}

// The records a move reads, as the card machine wants them, from the view.

func (t *tick) cardOf(id string) card.Card { return card.FromStore(t.v.Cards[id]) }

func (t *tick) consOf(p string) []card.Card {
	cs := t.v.Consumers(p)
	out := make([]card.Card, len(cs))
	for i, c := range cs {
		out[i] = card.FromStore(c)
	}
	return out
}

// rows is every row, as the machine reads them, built once per tick.
func (t *tick) rows() map[string]card.Row {
	if t.rowsCache == nil {
		t.rowsCache = map[string]card.Row{}
		for n, r := range t.v.Rows {
			t.rowsCache[n] = t.rowRecord(r)
		}
	}
	return t.rowsCache
}

func (t *tick) streamOf(name string) card.Stream { return card.StreamFromStore(t.v.Streams[name]) }

// needsOf is every card reachable from p through needs and after, plus any
// extra named (Replace's by, Relink's to), as the machine's needs map.
func (t *tick) needsOf(p string, extra ...string) map[string]card.Card {
	out := map[string]card.Card{}
	var walk func(id string)
	walk = func(id string) {
		c, ok := t.v.Cards[id]
		if !ok {
			return
		}
		if _, seen := out[id]; seen {
			return
		}
		out[id] = card.FromStore(c)
		for _, n := range append(slices.Clone(c.Needs), c.After...) {
			walk(n)
		}
	}
	for _, id := range append(slices.Clone(extra), t.v.Cards[p].Needs...) {
		walk(id)
	}
	for _, a := range t.v.Cards[p].After {
		walk(a)
	}
	delete(out, p)
	return out
}

// producerMove is card.Move of producer p on e, read from the view.
func (t *tick) producerMove(p string, e card.Event, extra ...string) move {
	return move{label: eventName(e) + " " + p, plan: func() (card.Plan, *card.Refusal) {
		pc := t.cardOf(p)
		return card.Move(pc, t.consOf(p), t.rows(), t.streamOf(pc.Stream), t.needsOf(p, extra...), e)
	}}
}

// consumerMove is card.MoveConsumer of consumer c on e, read from the view.
func (t *tick) consumerMove(c string, e card.Event) move {
	return move{label: eventName(e) + " " + c, plan: func() (card.Plan, *card.Refusal) {
		cc := t.cardOf(c)
		return card.MoveConsumer(cc, t.cardOf(cc.Of), t.rows()[cc.Holder], e)
	}}
}

// rowMove is card.MoveRow of row r on e.
func (t *tick) rowMove(r string, e card.Event) move {
	return move{label: eventName(e) + " " + r, plan: func() (card.Plan, *card.Refusal) {
		return card.MoveRow(t.rows()[r], e)
	}}
}

// withdraw is a consumer's Withdraw with its producer's stream, so a release
// into a held stream parks.
func (t *tick) withdraw(c, reason string) move {
	cc := t.v.Cards[c]
	head := ""
	if cc.Kind == "work" && cc.State == "working" && cc.Head != "" {
		head = cc.Head
	}
	return t.consumerMove(c, card.Withdraw{Reason: reason, Head: head, Stream: t.streamOf(cc.Stream)})
}

func eventName(e card.Event) string { return strings.TrimPrefix(fmt.Sprintf("%T", e), "card.") }
