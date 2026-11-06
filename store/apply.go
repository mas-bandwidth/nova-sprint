package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Expect is a condition: one of CardIs, CardHolder, RowIs, StreamHeld,
// CardAbsent, EpochIs. The set is closed by the unexported method.
type Expect interface{ isExpect() }

// CardIs holds when card Card is in state State with gen Gen.
type CardIs struct {
	Card, State string
	Gen         int
}

// CardHolder holds when card Card has holder Holder ("" for none).
type CardHolder struct{ Card, Holder string }

// RowIs holds when row Row is in state State.
type RowIs struct{ Row, State string }

// StreamHeld holds when stream Stream's held flag is Held.
type StreamHeld struct {
	Stream string
	Held   bool
}

// CardAbsent holds when card Card does not exist in the current epoch.
type CardAbsent struct{ Card string }

// EpochIs holds when the current epoch is N.
type EpochIs struct{ N int }

func (CardIs) isExpect()     {}
func (CardHolder) isExpect() {}
func (RowIs) isExpect()      {}
func (StreamHeld) isExpect() {}
func (CardAbsent) isExpect() {}
func (EpochIs) isExpect()    {}

// Write is one write: one of PutStream, SetStreamHeld, PutCard, SetCardState,
// SetCardHolder, SetCardHeadPR, SetCardReason, PutRow, SetRowState, PutPolicy,
// SetActivePolicy. The set is closed by the unexported method.
type Write interface{ isWrite() }

// PutStream writes a stream whole, adding it or replacing it.
type PutStream struct{ Stream Stream }

// SetStreamHeld sets a stream's held flag.
type SetStreamHeld struct {
	Stream string
	Held   bool
}

// PutCard adds a card; it is refused when the card exists. The card starts at
// gen 0 whatever Card.Gen says; its stream, and its holder when set, must exist.
type PutCard struct{ Card Card }

// SetCardState sets a card's state and increments its gen by one.
type SetCardState struct{ Card, State string }

// SetCardHolder sets a card's holder; "" is none, else the row must exist.
type SetCardHolder struct{ Card, Holder string }

// SetCardHeadPR sets a card's head commit and pull request number.
type SetCardHeadPR struct {
	Card, Head string
	PR         int
}

// SetCardReason sets a card's reason.
type SetCardReason struct{ Card, Reason string }

// PutRow writes a row whole, adding it or replacing it.
type PutRow struct{ Row Row }

// SetRowState sets a row's state, last seen time and flap count together.
type SetRowState struct {
	Row, State string
	Seen       time.Time
	Flaps      int
}

// PutPolicy adds the next policy version, inactive; Actor and At come from the op.
type PutPolicy struct{ Kind, Name, Body string }

// SetActivePolicy makes one existing version the active one.
type SetActivePolicy struct{ Version int }

func (PutStream) isWrite()       {}
func (SetStreamHeld) isWrite()   {}
func (PutCard) isWrite()         {}
func (SetCardState) isWrite()    {}
func (SetCardHolder) isWrite()   {}
func (SetCardHeadPR) isWrite()   {}
func (SetCardReason) isWrite()   {}
func (PutRow) isWrite()          {}
func (SetRowState) isWrite()     {}
func (PutPolicy) isWrite()       {}
func (SetActivePolicy) isWrite() {}

// txn is one transaction as the shared rules see it: reads by key (locking the
// row in PostgreSQL) and whole-row upserts. PG and Fake each implement it, so
// the conditions, writes and refusal reasons are written once.
type txn interface {
	card(id string) (Card, bool, error)
	stream(name string) (Stream, bool, error)
	row(name string) (Row, bool, error)
	policy(version int) (Policy, bool, error)
	policies() (last int, active []Policy, err error)
	putCard(c Card) error
	putStream(s Stream) error
	putRow(r Row) error
	putPolicy(p Policy) error
}

// errNoID is returned, before anything is sent, for an op without an id.
var errNoID = errors.New("store: an op needs an id")

// receipt is the receipt of an op that ran in epoch and was logged at seq.
func receipt(op Op, reason string, after *State, seq int64, epoch int) Receipt {
	return Receipt{Op: op.ID, Kind: op.Kind, Accepted: reason == "", Reason: reason, Seq: seq, Epoch: epoch, At: op.At, Note: op.Note, After: after}
}

// clearable refuses, before anything is sent, a clear it cannot run.
func clearable(op Op) error {
	if op.ID == "" {
		return errNoID
	}
	if len(op.Expect) > 0 || len(op.Writes) > 0 {
		return errors.New("store: a clear takes no conditions or writes")
	}
	return nil
}

// clearOp is a clear's op as it is logged: kind clear unless the caller named one.
func clearOp(op Op) Op {
	op.Kind = cmpOr(op.Kind, "clear")
	return op
}

// normalize is an op with its times as the store keeps them.
func normalize(op Op) Op {
	op.At = at(op.At)
	op.Writes = slices.Clone(op.Writes)
	for i, w := range op.Writes {
		switch v := w.(type) {
		case SetRowState:
			v.Seen = at(v.Seen)
			op.Writes[i] = v
		case PutRow:
			v.Row.Seen = at(v.Row.Seen)
			op.Writes[i] = v
		case PutCard:
			v.Card.Gen, v.Card.Needs = 0, needs(v.Card.Needs)
			op.Writes[i] = v
		}
	}
	return op
}

func oneOf(what, v string, set []string) string {
	if slices.Contains(set, v) {
		return ""
	}
	return fmt.Sprintf("%s %q is not one of %s", what, v, strings.Join(set, ", "))
}

// validate refuses, before any SQL runs, a condition or write outside the
// closed sets and a value the schema would refuse.
func validate(op Op) string {
	for _, e := range op.Expect {
		switch e.(type) {
		case CardIs, CardHolder, RowIs, StreamHeld, CardAbsent, EpochIs:
		default:
			return fmt.Sprintf("unknown condition %T", e)
		}
	}
	for _, w := range op.Writes {
		if r := validateWrite(w); r != "" {
			return r
		}
	}
	return ""
}

func validateWrite(w Write) string {
	switch v := w.(type) {
	case PutStream:
		if v.Stream.Name == "" {
			return "a stream needs a name"
		}
	case PutCard:
		if v.Card.ID == "" {
			return "a card needs an id"
		}
		return oneOf("card state", v.Card.State, CardStates)
	case SetCardState:
		return oneOf("card state", v.State, CardStates)
	case PutRow:
		if v.Row.Name == "" {
			return "a row needs a name"
		}
		return cmpOr(oneOf("row kind", v.Row.Kind, RowKinds), oneOf("row state", v.Row.State, RowStates))
	case SetRowState:
		return oneOf("row state", v.State, RowStates)
	case PutPolicy:
		return oneOf("policy kind", v.Kind, PolicyKind)
	case SetStreamHeld, SetCardHolder, SetCardHeadPR, SetCardReason, SetActivePolicy:
	default:
		return fmt.Sprintf("unknown write %T", w)
	}
	return ""
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// work is one operation being run against a txn: the touched rows before and
// after, kept for the event and the receipt.
type work struct {
	tx            txn
	epoch         int
	op            Op
	before, after State
}

// run checks every condition, then performs every write, in order. It returns
// a refusal reason ("" when accepted); an error is a transport failure. On a
// refusal the caller discards whatever the txn did.
func run(tx txn, epoch int, op Op) (string, *State, *State, error) {
	w := &work{tx: tx, epoch: epoch, op: op}
	if r := validate(op); r != "" {
		return r, nil, nil, nil
	}
	for _, e := range op.Expect {
		if r, err := w.expect(e); r != "" || err != nil {
			return r, nil, nil, err
		}
	}
	for _, wr := range op.Writes {
		if r, err := w.write(wr); r != "" || err != nil {
			return r, nil, nil, err
		}
	}
	return "", w.before.compact(), w.after.compact(), nil
}

func none(holder string) string { return cmpOr(holder, "none") }

func heldWord(held bool) string {
	if held {
		return "held"
	}
	return "not held"
}

func (w *work) expect(e Expect) (string, error) {
	switch c := e.(type) {
	case CardIs:
		card, r, err := w.mustCard(c.Card)
		switch {
		case r != "" || err != nil:
			return r, err
		case card.Gen != c.Gen:
			return fmt.Sprintf("card %s is at gen %d, expected gen %d", c.Card, card.Gen, c.Gen), nil
		case card.State != c.State:
			return fmt.Sprintf("card %s is %s, expected %s", c.Card, card.State, c.State), nil
		}
	case CardHolder:
		card, r, err := w.mustCard(c.Card)
		if r == "" && err == nil && card.Holder != c.Holder {
			r = fmt.Sprintf("card %s has holder %s, expected %s", c.Card, none(card.Holder), none(c.Holder))
		}
		return r, err
	case RowIs:
		row, r, err := w.mustRow(c.Row)
		if r == "" && err == nil && row.State != c.State {
			r = fmt.Sprintf("row %s is %s, expected %s", c.Row, row.State, c.State)
		}
		return r, err
	case StreamHeld:
		s, r, err := w.mustStream(c.Stream)
		if r == "" && err == nil && s.Held != c.Held {
			r = fmt.Sprintf("stream %s is %s, expected %s", c.Stream, heldWord(s.Held), heldWord(c.Held))
		}
		return r, err
	case CardAbsent:
		_, ok, err := w.tx.card(c.Card)
		if ok {
			return fmt.Sprintf("card %s exists", c.Card), err
		}
		return "", err
	case EpochIs:
		if w.epoch != c.N {
			return fmt.Sprintf("the epoch is %d, expected %d", w.epoch, c.N), nil
		}
	}
	return "", nil
}

func (w *work) mustCard(id string) (Card, string, error) {
	c, ok, err := w.tx.card(id)
	if err == nil && !ok {
		return c, fmt.Sprintf("card %s does not exist", id), nil
	}
	return c, "", err
}

func (w *work) mustRow(name string) (Row, string, error) {
	r, ok, err := w.tx.row(name)
	if err == nil && !ok {
		return r, fmt.Sprintf("row %s does not exist", name), nil
	}
	return r, "", err
}

func (w *work) mustStream(name string) (Stream, string, error) {
	s, ok, err := w.tx.stream(name)
	if err == nil && !ok {
		return s, fmt.Sprintf("stream %s does not exist", name), nil
	}
	return s, "", err
}

func (w *work) write(wr Write) (string, error) {
	switch v := wr.(type) {
	case PutStream:
		old, ok, err := w.tx.stream(v.Stream.Name)
		if err != nil {
			return "", err
		}
		return "", w.putStream(old, ok, v.Stream)
	case SetStreamHeld:
		s, r, err := w.mustStream(v.Stream)
		if r != "" || err != nil {
			return r, err
		}
		next := s
		next.Held = v.Held
		return "", w.putStream(s, true, next)
	case PutCard:
		return w.addCard(v.Card)
	case PutRow:
		old, ok, err := w.tx.row(v.Row.Name)
		if err != nil {
			return "", err
		}
		return "", w.putRow(old, ok, v.Row)
	case SetRowState:
		row, r, err := w.mustRow(v.Row)
		if r != "" || err != nil {
			return r, err
		}
		next := row
		next.State, next.Seen, next.Flaps = v.State, v.Seen, v.Flaps
		return "", w.putRow(row, true, next)
	case PutPolicy, SetActivePolicy:
		return w.writePolicy(wr)
	}
	return w.writeCard(wr)
}

// writeCard is the writes that change one field set of an existing card.
func (w *work) writeCard(wr Write) (string, error) {
	var id string
	switch v := wr.(type) {
	case SetCardState:
		id = v.Card
	case SetCardHolder:
		id = v.Card
	case SetCardHeadPR:
		id = v.Card
	case SetCardReason:
		id = v.Card
	}
	card, r, err := w.mustCard(id)
	if r != "" || err != nil {
		return r, err
	}
	next := card
	switch v := wr.(type) {
	case SetCardState:
		next.State, next.Gen = v.State, card.Gen+1
	case SetCardHolder:
		if r, err := w.holderExists(v.Holder); r != "" || err != nil {
			return r, err
		}
		next.Holder = v.Holder
	case SetCardHeadPR:
		next.Head, next.PR = v.Head, v.PR
	case SetCardReason:
		next.Reason = v.Reason
	}
	return "", w.putCard(card, true, next)
}

func (w *work) holderExists(holder string) (string, error) {
	if holder == "" {
		return "", nil
	}
	_, r, err := w.mustRow(holder)
	return r, err
}

func (w *work) addCard(c Card) (string, error) {
	if _, ok, err := w.tx.card(c.ID); ok || err != nil {
		return fmt.Sprintf("card %s exists", c.ID), err
	}
	if _, r, err := w.mustStream(c.Stream); r != "" || err != nil {
		return r, err
	}
	if r, err := w.holderExists(c.Holder); r != "" || err != nil {
		return r, err
	}
	return "", w.putCard(Card{}, false, c)
}

func (w *work) writePolicy(wr Write) (string, error) {
	last, active, err := w.tx.policies()
	if err != nil {
		return "", err
	}
	switch v := wr.(type) {
	case PutPolicy:
		p := Policy{Version: last + 1, Kind: v.Kind, Name: v.Name, Body: v.Body, Actor: w.op.Actor, At: w.op.At}
		return "", w.putPolicy(Policy{}, false, p)
	case SetActivePolicy:
		p, ok, err := w.tx.policy(v.Version)
		if err != nil || !ok {
			return fmt.Sprintf("policy version %d does not exist", v.Version), err
		}
		for _, a := range active {
			if a.Version != p.Version {
				off := a
				off.Active = false
				if err := w.putPolicy(a, true, off); err != nil {
					return "", err
				}
			}
		}
		on := p
		on.Active = true
		return "", w.putPolicy(p, true, on)
	}
	return "", nil
}

// The put helpers record a row's before-state the first time it is touched
// (only when it existed) and its after-state every time.

func (w *work) putStream(old Stream, existed bool, s Stream) error {
	w.before.Streams, w.after.Streams = touch(w.before.Streams, w.after.Streams, s.Name, old, existed, s)
	return w.tx.putStream(s)
}

func (w *work) putCard(old Card, existed bool, c Card) error {
	w.before.Cards, w.after.Cards = touch(w.before.Cards, w.after.Cards, c.ID, old, existed, c)
	return w.tx.putCard(c)
}

func (w *work) putRow(old Row, existed bool, r Row) error {
	w.before.Rows, w.after.Rows = touch(w.before.Rows, w.after.Rows, r.Name, old, existed, r)
	return w.tx.putRow(r)
}

func (w *work) putPolicy(old Policy, existed bool, p Policy) error {
	w.before.Policies, w.after.Policies = touch(w.before.Policies, w.after.Policies, p.Version, old, existed, p)
	return w.tx.putPolicy(p)
}

func touch[K comparable, V any](before, after map[K]V, k K, old V, existed bool, v V) (map[K]V, map[K]V) {
	if before == nil {
		before, after = map[K]V{}, map[K]V{}
	}
	if _, seen := after[k]; !seen && existed {
		before[k] = old
	}
	after[k] = v
	return before, after
}

// compact is the state with empty maps dropped, or nil when nothing is in it.
func (s State) compact() *State {
	if len(s.Streams) == 0 {
		s.Streams = nil
	}
	if len(s.Cards) == 0 {
		s.Cards = nil
	}
	if len(s.Rows) == 0 {
		s.Rows = nil
	}
	if len(s.Policies) == 0 {
		s.Policies = nil
	}
	if s.Epoch == 0 && s.Streams == nil && s.Cards == nil && s.Rows == nil && s.Policies == nil {
		return nil
	}
	return &s
}
