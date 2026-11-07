package steps

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Request is one per-card verb asked of the engine (LAYER-3 "Requests"; list
// item 8): who asks and what. Event is the move: a producer event on Card, a
// consumer event (Take, Report) on Card, a row event on Row, a stream event
// on Stream, or card.Add. Judgement names an open judgement the request
// answers with Event as the chosen move (Event nil: the answer alone). Drop
// names, per dependant of a Cancel or Replace, the Waive or Relink the drop
// chose; a dependant not named fails with its need, the whole closure in
// the same op (LESSONS 2.4, 2.18).
type Request struct {
	Actor        string
	Coordinators []string // the rows nova-config names as coordinators
	Card         string
	Row          string
	Stream       string
	Event        card.Event
	Judgement    string
	Answer       string
	Drop         map[string]card.Event
}

// coordinatorEvents are the events only a coordinator may raise (an engine
// fact: a wrong coordinator could corrupt).
var coordinatorEvents = []string{"Unblock", "Waive", "Relink", "SetPriority", "WaiveRed", "Cancel", "Replace", "Edit", "Rescore", "Hold", "Unhold", "HoldStream", "UnholdStream"}

// runEvents are the run loop's own (presence and nova-config's width).
var runEvents = []string{"Seen", "Gone", "Width"}

// Verb is one request as one op, or a refusal naming the fix: the actor
// check, the move through the card machine, one op. A verb refused for its
// actor writes nothing.
func Verb(v *View, in Inputs, r Request) (store.Op, *card.Refusal) {
	t := newTick(v, in)
	t.o = newOracle(Answers{}, Engine)
	t.lim, t.tmo = Engine.Limits, Engine.Timeouts
	if ref := t.actorOK(r); ref != nil {
		return store.Op{}, ref
	}
	var answer card.Plan
	if r.Judgement != "" {
		a, ref := t.answerPlan(r)
		if ref != nil {
			return store.Op{}, ref
		}
		answer = a
	}
	g := group{kind: "judge", key: GroupKey{Kind: "judge"}}
	if r.Event != nil {
		moves, ref := t.requestMoves(r)
		if ref != nil {
			return store.Op{}, ref
		}
		if g, ref = t.newGroup(strings.ToLower(eventName(r.Event)), r.Card, moves...); ref != nil {
			return store.Op{}, ref
		}
	} else if r.Judgement == "" {
		return store.Op{}, &card.Refusal{Reason: "a request names an event or a judgement to answer"}
	}
	// The answer rides beside the move, outside the card machine's merge (a
	// judgement condition is not one the machine makes).
	g.plan.Expect = append(g.plan.Expect, answer.Expect...)
	g.plan.Writes = append(g.plan.Writes, answer.Writes...)
	kind := g.kind
	op := t.storeOp("", []group{g})
	op.ID = fmt.Sprintf("v%d.%d.%s", v.Epoch, v.Seq, hashBody(op.Expect, op.Writes))
	op.Actor, op.Kind = r.Actor, kind
	return op, nil
}

// actorOK is the actor rule: coordinator events need a coordinator; Take
// and Report the consumer's holder; Seen, Gone and Width the run loop; Add
// anyone.
func (t *tick) actorOK(r Request) *card.Refusal {
	name := ""
	if r.Event != nil {
		name = eventName(r.Event)
	}
	coordinator := slices.Contains(r.Coordinators, r.Actor)
	switch {
	case (r.Judgement != "" || slices.Contains(coordinatorEvents, name)) && !coordinator:
		what := name
		if what == "" {
			what = "judge"
		}
		return &card.Refusal{Reason: fmt.Sprintf("%s is the coordinator's; ask on the bus: `nova-bus send --as %s --to %s ...`", what, r.Actor, firstOr(r.Coordinators, "<coordinator>"))}
	case slices.Contains(runEvents, name) && r.Actor != Actor:
		return &card.Refusal{Reason: name + " is the run loop's; nova-config is where width and presence are set"}
	case name == "Take" || name == "Report":
		c, ok := t.v.Cards[r.Card]
		if !ok {
			return &card.Refusal{Reason: fmt.Sprintf("card %s does not exist: run `nova-sprint next --as %s`", r.Card, r.Actor)}
		}
		if c.Holder != r.Actor {
			return &card.Refusal{Reason: fmt.Sprintf("consumer %s is held by %s, not %s", c.ID, c.Holder, r.Actor)}
		}
	}
	return nil
}

func firstOr(ss []string, or string) string {
	if len(ss) == 0 {
		return or
	}
	return ss[0]
}

// requestMoves is the move (or moves) an event asks for.
func (t *tick) requestMoves(r Request) ([]move, *card.Refusal) {
	switch e := r.Event.(type) {
	case card.Add:
		return []move{{label: "Add " + e.ID, plan: func() (card.Plan, *card.Refusal) {
			cards := t.needsOf(e.ID)
			for _, id := range append(slices.Clone(e.Needs), e.After...) {
				for k, c := range t.needsOf(id, id) {
					cards[k] = c
				}
			}
			return card.MoveAdd(e, cards)
		}}}, nil
	case card.Take, card.Report:
		if _, ok := t.v.Cards[r.Card]; !ok {
			return nil, &card.Refusal{Reason: "card " + r.Card + " does not exist"}
		}
		return []move{t.consumerMove(r.Card, e)}, nil
	case card.Seen, card.Gone, card.Width, card.Hold, card.Unhold:
		if _, ok := t.v.Rows[r.Row]; !ok {
			return nil, &card.Refusal{Reason: "row " + r.Row + " does not exist"}
		}
		return []move{t.rowMove(r.Row, e)}, nil
	case card.HoldStream, card.UnholdStream:
		return t.streamMoves(r, e)
	case card.Cancel, card.Replace:
		return t.dropMoves(r)
	}
	if _, ok := t.v.Cards[r.Card]; !ok {
		return nil, &card.Refusal{Reason: "card " + r.Card + " does not exist"}
	}
	extra := []string{}
	if rel, ok := r.Event.(card.Relink); ok {
		extra = append(extra, rel.To)
	}
	return []move{t.producerMove(r.Card, r.Event, extra...)}, nil
}

// streamMoves is a stream's hold with every ready producer of it parked in
// the same op, fenced at the view's seq, or its lifting.
func (t *tick) streamMoves(r Request, e card.Event) ([]move, *card.Refusal) {
	if _, ok := t.v.Streams[r.Stream]; !ok {
		return nil, &card.Refusal{Reason: "stream " + r.Stream + " does not exist"}
	}
	if h, ok := e.(card.HoldStream); ok {
		h.Ready, h.Seq = t.readyIn(r.Stream), t.v.Seq
		e = h
	}
	return []move{{label: eventName(e) + " " + r.Stream, plan: func() (card.Plan, *card.Refusal) {
		return card.MoveStream(t.streamOf(r.Stream), e)
	}}}, nil
}

// dropMoves is a Cancel or Replace with its closure of dependants in the same
// op: each one's chosen Waive or Relink, else NeedFailed in this op.
func (t *tick) dropMoves(r Request) ([]move, *card.Refusal) {
	if _, ok := t.v.Cards[r.Card]; !ok {
		return nil, &card.Refusal{Reason: "card " + r.Card + " does not exist"}
	}
	extra := []string{}
	if rep, ok := r.Event.(card.Replace); ok {
		extra = append(extra, rep.By)
	}
	moves := []move{t.producerMove(r.Card, r.Event, extra...)}
	seen := set{r.Card: {}}
	queue := []string{r.Card}
	for len(queue) > 0 {
		need := queue[0]
		queue = queue[1:]
		for _, d := range t.v.Dependants(need) {
			c := t.v.Cards[d]
			if _, s := seen[d]; s || c.State == "fail" || c.State == "landed" {
				continue
			}
			seen[d] = struct{}{}
			if chosen, ok := r.Drop[d]; ok {
				moves = append(moves, t.producerMove(d, inThisOp(chosen, need), relinkTo(chosen)...))
				continue
			}
			moves = append(moves, t.producerMove(d, card.NeedFailed{Need: need, InThisOp: true}))
			queue = append(queue, d)
		}
	}
	return moves, nil
}

// inThisOp is the drop's chosen move for a dependant, marked as in this op
// and naming the need.
func inThisOp(e card.Event, need string) card.Event {
	switch x := e.(type) {
	case card.Waive:
		x.Need, x.InThisOp = need, true
		return x
	case card.Relink:
		x.Need, x.InThisOp = need, true
		return x
	}
	return e
}

func relinkTo(e card.Event) []string {
	if x, ok := e.(card.Relink); ok {
		return []string{x.To}
	}
	return nil
}

// answerPlan answers an open judgement: refused when it is not open, naming
// who closed it.
func (t *tick) answerPlan(r Request) (card.Plan, *card.Refusal) {
	j, ok := t.v.Judgements[r.Judgement]
	switch {
	case !ok:
		return card.Plan{}, &card.Refusal{Reason: "judgement " + r.Judgement + " does not exist: run `nova-sprint judge`"}
	case j.State != "open":
		return card.Plan{}, &card.Refusal{Reason: fmt.Sprintf("judgement %s was %s by %s; nothing to answer", j.ID, j.State, j.Actor)}
	case r.Answer == "":
		return card.Plan{}, &card.Refusal{Reason: "judgement " + j.ID + " needs a choice: " + choices[j.Kind]}
	}
	return card.Plan{Expect: []store.Expect{store.JudgementOpen{ID: j.ID}}, Writes: []store.Write{store.AnswerJudgement{ID: j.ID, Answer: r.Answer, Actor: r.Actor}}}, nil
}
