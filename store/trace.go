package store

import (
	"fmt"
	"slices"
)

// Trace validation (tla/README.md, "Trace validation"): a walk of the store
// records, after every step, the store's state projected onto the variables
// of tla/Store.tla, and tla/StoreTrace.tla checks the recorded steps are a
// behaviour of the model. TraceState and TraceOp are the refinement mapping:
// the one place the code's state and ops are read in the model's terms.
//
// Every variable of Store.tla is accounted for:
//
//   - epoch, cards, rows, judgements, cursors, policies: ModelState, below.
//   - events and ledger: not projected; the trace records each step's op
//     and whether it was accepted, and the model builds its own log from
//     them (one event per op that entered the store, refused ones included,
//     as the code's seq counts them).
//   - applied, phase, receipt, lost: the caller's side; the trace records
//     the step kind (Apply, LoseRequest, Replay) and whether the caller
//     learned the outcome, from which the model moves them.
//
// What the code has that the model does not (the abstractions):
//
//   - Streams and every stream write (PutStream, SetStreamHeld,
//     SetStreamMode, SetStreamForge; mode and forge are stream state) and the
//     StreamHeld condition: the model has no streams. PutStream is dropped
//     from a projected op (it is never refused for a value it carries); an
//     op with another stream write or StreamHeld is not projected (TraceOp
//     refuses it), since each can be refused as unchanged.
//   - The AtSeq condition, SetActivePolicy, the policy's active flag, kind
//     and name: not in the model; TraceOp refuses the first two.
//   - Every card column no rule of the model reads (attempt, score, branch,
//     head, pr, brief, prose, paths, done_when, reason, author, base_sha,
//     prior_head, result, bounces, finding, review, cost, priced, usage, priority,
//     after) and the set writes on them: TraceOp refuses those writes, and a
//     PutCard that sets one of the columns the store's rules read (after,
//     attempt, review, finding, priority).
//   - A consumer's tier: the model takes its producer's at add and ignores
//     the write's; the store refuses one that differs. A walk sends the
//     producer's.
//   - Row load, cores, slots and advertised tiers (the heartbeat,
//     SetRowLoad): a write TraceOp refuses; it moves no gen and no store
//     rule reads it (the roster reads slots and tiers, outside the store).
//   - Row flaps and reason, and the write on them (SetRowFlaps): TraceOp
//     refuses it. A row's session and role (the hook, PutRow) are projected
//     as sess and role; a walk's sessions are the model's (x, y).
//   - The sprint's commit identity (SetIdentity): TraceOp refuses
//     it; no store rule reads it, and a clear keeps it as it keeps the stop.
//   - The stop's reason: only Stopped is projected (SetStopped's flag). A row has no kind, width, tiers, model or harness in the
//     store or the model: they are the roster's (the tiers
//     a worker advertises in its heartbeat are telemetry, above).
//   - Judgement kind, about, question, command, group, deadline, answer,
//     actor and times: only the state is projected; a close before
//     opened_at is outside the model (a walk's At only rises).
//   - Op actor, kind, at and note, and the receipt's reason: not projected.
//   - The seq: the model's is the length of its own log.

// ModelState is the store's state in Store.tla's terms. A card, row,
// judgement, cursor or policy version not in its map is absent (NoCard,
// NoRow, Absent, 0, Absent in the model).
type ModelState struct {
	Epoch      int
	Cards      map[string]ModelCard
	Rows       map[string]ModelRow
	Judgements map[string]string // open, answered or expired
	Cursors    map[string]int64
	Policies   map[int]string // version -> body
	Stopped    bool           // the sprint's stop (its reason is not projected)
}

// ModelCard is Store.tla's card record: [state, gen, holder, needs, kind,
// prod, tier, red]. Holder is "none" and Prod "-" when the card has none.
type ModelCard struct {
	State, Holder, Kind, Prod, Tier string
	Gen                             int
	Needs                           []string
	Red                             bool
}

// ModelRow is Store.tla's row record: [state, gen, held, sess, role]. Sess
// and Role are "-" (None) when the row is not hooked.
type ModelRow struct {
	State      string
	Gen        int
	Held       bool
	Sess, Role string
}

// TraceState is the refinement mapping of the store's state: the snapshot
// (the current epoch, every cursor) and every policy version, projected onto
// Store.tla's store variables.
func TraceState(s Snapshot, policies []PolicyVersion) ModelState {
	m := ModelState{Epoch: s.Epoch, Cards: map[string]ModelCard{}, Rows: map[string]ModelRow{},
		Judgements: map[string]string{}, Cursors: map[string]int64{}, Policies: map[int]string{}}
	for id, c := range s.Cards {
		m.Cards[id] = ModelCard{State: c.State, Gen: c.Gen, Holder: orDash(c.Holder, "none"), Needs: slices.Clone(c.Needs),
			Kind: c.Kind, Prod: orDash(c.Of, "-"), Tier: c.Tier, Red: c.Red}
	}
	for n, r := range s.Rows {
		m.Rows[n] = ModelRow{State: r.State, Gen: r.Gen, Held: r.Held, Sess: orDash(r.Session, "-"), Role: orDash(r.Role, "-")}
	}
	for id, j := range s.Judgements {
		m.Judgements[id] = j.State
	}
	for n, seq := range s.Cursors {
		m.Cursors[n] = seq
	}
	for _, p := range policies {
		m.Policies[p.Version] = p.Body
	}
	m.Stopped = s.Stopped
	return m
}

func orDash(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// ModelOp is Store.tla's op record: kind "op" or "clear", the epoch it was
// planned against, its conditions and its writes in order.
type ModelOp struct {
	Clear  bool
	Epoch  int
	Expect []ModelCond
	Writes []ModelWrite
}

// ModelCond is Store.tla's condition record (Cnd): k, c, r, s, g.
type ModelCond struct {
	K, C, R, S string
	G          int
}

// ModelWrite is Store.tla's write record (Wr): k, c, r, s, n, w, kd, p.
type ModelWrite struct {
	K, C, R, S string
	N          []string
	W          int
	Kd, P      string
}

// TraceOp is the refinement mapping of an op: the op in Store.tla's terms,
// or an error naming the part the model does not have.
func TraceOp(op Op, clear bool) (ModelOp, error) {
	m := ModelOp{Clear: clear, Epoch: op.Epoch}
	for _, e := range op.Expect {
		c, err := traceCond(e)
		if err != nil {
			return ModelOp{}, err
		}
		m.Expect = append(m.Expect, c)
	}
	for _, w := range op.Writes {
		if _, ok := w.(PutStream); ok {
			continue // streams are outside the model
		}
		x, err := traceWrite(w)
		if err != nil {
			return ModelOp{}, err
		}
		m.Writes = append(m.Writes, x)
	}
	return m, nil
}

func traceCond(e Expect) (ModelCond, error) {
	const none = "-"
	switch v := e.(type) {
	case CardGen:
		return ModelCond{"cardGen", v.Card, none, none, v.Gen}, nil
	case CardState:
		return ModelCond{"cardState", v.Card, none, v.State, 0}, nil
	case CardHolder:
		return ModelCond{"holderIs", v.Card, orDash(v.Holder, "none"), none, 0}, nil
	case NeedsLanded:
		return ModelCond{"needsLanded", v.Card, none, none, 0}, nil
	case CardAbsent:
		return ModelCond{"cardAbsent", v.Card, none, none, 0}, nil
	case RowIs:
		return ModelCond{"rowIs", none, v.Row, v.State, 0}, nil
	case RowGen:
		return ModelCond{"rowGen", none, v.Row, none, v.Gen}, nil
	case RowHeld:
		return ModelCond{"rowHeld", none, v.Row, none, flag(v.Held)}, nil
	case JudgementOpen:
		return ModelCond{"judgeOpen", v.ID, none, none, 0}, nil
	case CardRed:
		return ModelCond{"cardRed", v.Card, none, none, flag(v.Red)}, nil
	case Running:
		return ModelCond{"running", none, none, none, 0}, nil
	}
	return ModelCond{}, fmt.Errorf("store: trace: condition %T is not in Store.tla", e)
}

func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

func traceWrite(w Write) (ModelWrite, error) {
	x := ModelWrite{C: "-", R: "-", S: "-", Kd: "-", P: "-"}
	switch v := w.(type) {
	case PutCard:
		return tracePutCard(v)
	case SetCardState:
		x.K, x.C, x.S = "setState", v.Card, v.State
	case SetCardRed:
		x.K, x.C, x.W = "setRed", v.Card, flag(v.Red)
	case PutRow:
		x.K, x.R, x.S, x.Kd = "putRow", v.Name, orDash(v.Session, "-"), orDash(v.Role, "-")
	case SetStopped:
		x.K, x.W = "setStopped", flag(v.Stopped)
	case SetRowState:
		x.K, x.R, x.S = "setRowState", v.Row, v.State
	case SetRowHeld:
		x.K, x.R, x.W = "setRowHeld", v.Row, flag(v.Held)
	case PutJudgement:
		x.K, x.C = "putJudge", v.ID
	case AnswerJudgement:
		x.K, x.C, x.S = "closeJudge", v.ID, "answered"
	case ExpireJudgement:
		x.K, x.C, x.S = "closeJudge", v.ID, "expired"
	case SetCursor:
		x.K, x.C, x.W = "setCursor", v.Name, int(v.Seq)
	case PutPolicy:
		x.K, x.S = "putPolicy", v.Body
	default:
		return ModelWrite{}, fmt.Errorf("store: trace: write %T is not in Store.tla", w)
	}
	return x, nil
}

// tracePutCard is AddCard(c, kd, p, n, h, t): the holder in r ("none" for
// none) and the tier in s.
func tracePutCard(v PutCard) (ModelWrite, error) {
	if len(v.After) > 0 || v.Attempt != 0 || v.Review != 0 || v.Finding != "" || (v.Priority != "" && v.Priority != "normal") {
		return ModelWrite{}, fmt.Errorf("store: trace: put card %s sets a column Store.tla leaves out", v.ID)
	}
	if len(normSet(v.Needs)) != len(v.Needs) {
		return ModelWrite{}, fmt.Errorf("store: trace: put card %s names a need twice; Store.tla's needs are a set", v.ID)
	}
	return ModelWrite{K: "addCard", C: v.ID, R: orDash(v.Holder, "none"), S: v.Tier, N: normSet(v.Needs), Kd: v.Kind, P: orDash(v.Of, "-")}, nil
}
