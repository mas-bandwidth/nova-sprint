package verbs

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// move is a person's move through the steps' actor rules (SEAMS 3): one View
// built from the snapshot the hooked check read, one op from steps.Verb on
// the request req makes of it. The op is fenced (fenced): refused for a
// write to a record the request does not name (another card dealt, a
// bench's report), it is planned again on a fresh read; a change to the
// card, row, stream or judgement it names is the refusal, naming it.
// With --dry-run it prints what the op would do and applies nothing.
func (e *Engine) move(ctx context.Context, in *input, snap store.Snapshot, req func(store.Snapshot) steps.Request, subject string) (string, error) {
	plan := func(s store.Snapshot) (store.Op, error) {
		r := req(s)
		r.Actor, r.Coordinators = in.actor, []string{coordinatorOf(s)}
		op, why := steps.Verb(steps.Build(s, e.Now()), steps.Inputs{Now: e.Now()}, r)
		if why != nil {
			return op, moved(why, subject)
		}
		op.Note = in.str("reason")
		return op, nil
	}
	if in.has("dry-run") {
		op, err := plan(snap)
		if err != nil {
			return "", err
		}
		return effects(op) + " (dry run)", nil
	}
	own := func(was, now store.Snapshot) string { return targetChange(req(was), was, now) }
	rec, op, err := e.fenced(ctx, snap, "nova-sprint show "+subject, own, plan)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s applied at seq %d", effects(op), rec.Seq), nil
}

// fixed is a request that is the same on any read.
func fixed(r steps.Request) func(store.Snapshot) steps.Request {
	return func(store.Snapshot) steps.Request { return r }
}

// targetChange is what changed, between was and now, of the records r names
// as its own: its card or row (its gen), its stream (its hold) and the
// judgement it answers (still open); "" when none did. A replacing card, a
// dependant and every other record are not its own: the plan on the fresh
// read checks them again.
func targetChange(r steps.Request, was, now store.Snapshot) string {
	if a, ok := was.Cards[r.Card]; ok {
		if b, ok := now.Cards[r.Card]; !ok || a.Gen != b.Gen {
			return fmt.Sprintf("card %s changed since this verb read it (gen %d %s, now gen %d %s)", r.Card, a.Gen, a.State, b.Gen, orWord(b.State, "gone"))
		}
	}
	if a, ok := was.Rows[r.Row]; ok {
		if b := now.Rows[r.Row]; a.Gen != b.Gen {
			return fmt.Sprintf("row %s changed since this verb read it (gen %d %s held=%t, now gen %d %s held=%t)", r.Row, a.Gen, a.State, a.Held, b.Gen, orWord(b.State, "gone"), b.Held)
		}
	}
	if a, ok := was.Streams[r.Stream]; ok {
		if b, ok := now.Streams[r.Stream]; !ok || a.Held != b.Held {
			return fmt.Sprintf("stream %s changed since this verb read it (held=%t, now held=%t)", r.Stream, a.Held, b.Held)
		}
	}
	if j, ok := now.Judgements[r.Judgement]; ok && j.State != "open" {
		return fmt.Sprintf("judgement %s was %s by %s since this verb read it", j.ID, j.State, j.Actor)
	}
	return ""
}

// again is the snapshot the i-th of several moves acts on: the hooked
// check's for the first, a fresh read after (each move is its own op, so
// each is fenced on what the one before it wrote).
func (e *Engine) again(ctx context.Context, snap store.Snapshot, i int) (store.Snapshot, error) {
	if i == 0 {
		return snap, nil
	}
	return e.snapshot(ctx)
}

// effects is what an op changes, in one line: each card's new state, each
// hold (a card's is its reason: a held card waits), each answered judgement.
func effects(op store.Op) string {
	var out []string
	hold := strings.EqualFold(op.Kind, "HoldCard") || strings.EqualFold(op.Kind, "UnholdCard")
	for _, w := range op.Writes {
		switch x := w.(type) {
		case store.SetCardState:
			out = append(out, x.Card+"->"+x.State)
		case store.SetCardReason:
			if hold {
				out = append(out, x.Card+" reason="+strconv.Quote(x.Reason))
			}
		case store.SetRowHeld:
			out = append(out, fmt.Sprintf("row %s held=%t", x.Row, x.Held))
		case store.SetStreamHeld:
			out = append(out, fmt.Sprintf("stream %s held=%t", x.Stream, x.Held))
		case store.SetCardPriority:
			out = append(out, x.Card+" priority="+x.Priority)
		case store.SetCardNeeds:
			out = append(out, x.Card+" needs="+strings.Join(x.Needs, ","))
		case store.AnswerJudgement:
			out = append(out, "judgement "+x.ID+" answered "+x.Answer)
		}
	}
	if len(out) == 0 {
		return strings.ToUpper(op.Kind) + " changes nothing"
	}
	return strings.ToUpper(op.Kind) + " " + strings.Join(out, " ")
}

// runHold is `hold` and `unhold` of rows, streams and producer cards, one
// op each. Holding a card (defect 47) leaves that one producer waiting with
// the reason "held: <why>", which the deal refuses, its live consumers
// withdrawn in the same op (their pushed branches kept); unhold is its
// UnholdCard, back to plain waiting, whence it resolves and is dealt as any
// other. It is never a drop.
func runHold(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	reason := in.str("reason")
	if reason == "" {
		return "", refuse("a "+in.verb+" needs its reason", "add --reason \"<why>\"")
	}
	for _, name := range in.args {
		if _, err := holdRequest(in.verb == "hold", name, reason, snap); err != nil {
			return "", err
		}
	}
	var lines []string
	for i, name := range in.args {
		var err error
		if snap, err = e.again(ctx, snap, i); err != nil {
			return "", partial(err, lines)
		}
		r, err := holdRequest(in.verb == "hold", name, reason, snap)
		if err != nil {
			return "", partial(err, lines)
		}
		line, err := e.move(ctx, in, snap, fixed(r), name)
		if err != nil {
			return "", partial(err, lines)
		}
		lines = append(lines, line)
	}
	return done(nextAfter(in), lines...), nil
}

// partial is a multi-target verb's error after some of its moves applied:
// the applied lines print before the refusal, which says they stand, so a
// refusal never reads as nothing moved.
func partial(err error, applied []string) error {
	if len(applied) == 0 {
		return err
	}
	f := *classify(err)
	f.Detail = strings.Join(applied, "\n") + "\n" + f.Detail
	f.What += " (the moves above were applied; this one and any after it were not)"
	return &f
}

// unheldPrefix starts an unheld card's reason, by unhold or by a held
// judgement's unhold alike.
const unheldPrefix = "unheld: "

func holdRequest(hold bool, name, reason string, snap store.Snapshot) (steps.Request, error) {
	if _, ok := snap.Rows[name]; ok {
		if hold {
			return steps.Request{Row: name, Event: card.Hold{Reason: reason}}, nil
		}
		return steps.Request{Row: name, Event: card.Unhold{Reason: reason}}, nil
	}
	if _, ok := snap.Streams[name]; ok {
		if hold {
			return steps.Request{Stream: name, Event: card.HoldStream{Reason: reason}}, nil
		}
		return steps.Request{Stream: name, Event: card.UnholdStream{Reason: reason}}, nil
	}
	if c, ok := snap.Cards[name]; ok {
		if c.Kind != "producer" {
			return steps.Request{}, refuse(name+" is a "+c.Kind+" card; hold its producer", "nova-sprint hold "+cmp.Or(c.Of, name)+" --reason \"<why>\"")
		}
		if hold {
			return steps.Request{Card: name, Event: card.HoldCard{Reason: card.HeldPrefix + reason}}, nil
		}
		return steps.Request{Card: name, Event: card.UnholdCard{Reason: unheldPrefix + reason}}, nil
	}
	return steps.Request{}, refuse("no row, stream or card "+name, "nova-sprint show")
}

// nextAfter is a person's move's next line: the same command without
// --dry-run after a dry run, else the bus.
func nextAfter(in *input) string {
	if !in.has("dry-run") {
		return waitNext
	}
	return "nova-sprint " + in.verb + " " + strings.Join(in.args, " ") + " --reason \"<why>\" (without --dry-run)"
}

// runPriority is `priority`: a producer's level.
func runPriority(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	id, level, reason := in.arg(0), in.arg(1), in.str("reason")
	if !slices.Contains(card.Levels, level) {
		return "", misuse("priority", "a level is one of "+strings.Join(card.Levels, " "))
	}
	if reason == "" {
		return "", refuse("a priority needs its reason", "add --reason \"<who asked, and why>\"")
	}
	c, ok := snap.Cards[id]
	if !ok {
		return "", refuse("no card "+id, "nova-sprint queue")
	}
	if c.Of != "" {
		return "", refuse(id+" is a consumer; priority is its producer's", "nova-sprint priority "+c.Of+" "+level+" --reason \"<why>\"")
	}
	line, err := e.move(ctx, in, snap, fixed(steps.Request{Card: id, Event: card.SetPriority{Level: level, Reason: reason}}), id)
	if err != nil {
		return "", err
	}
	return done(waitNext, line), nil
}

// runRelink is `relink`: a dependant's failed need pointed at another card.
func runRelink(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	id, need, to, reason := in.arg(0), in.str("need"), in.str("to"), in.str("reason")
	if need == "" || to == "" || reason == "" {
		return "", misuse("relink", "relink takes --need <failed need> --to <card> --reason <why>")
	}
	line, err := e.move(ctx, in, snap, fixed(steps.Request{Card: id, Event: card.Relink{Need: need, To: to, Reason: reason}}), id)
	if err != nil {
		return "", err
	}
	return done(waitNext, line), nil
}
