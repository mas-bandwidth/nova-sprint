package store

import (
	"fmt"
	"slices"
	"strings"
)

// txn is one transaction as the shared rules see it: reads by key and
// whole-row upserts in its epoch. PG and Fake each implement it, so the
// conditions, writes and refusal reasons are written once, here.
type txn interface {
	card(id string) (Card, bool, error)
	stream(name string) (Stream, bool, error)
	row(name string) (Row, bool, error)
	policy(version int) (PolicyVersion, bool, error)
	policies() (last int, active []PolicyVersion, err error)
	held(row string) (int, error) // cards whose holder is row
	lastSeq() (int64, error)
	rows() ([]Row, error) // every row of the epoch
	open() error          // epoch n = n + 1; later puts go to the new epoch
	putCard(c Card) error
	putStream(s Stream) error
	putRow(r Row) error
	putPolicy(p PolicyVersion) error
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// check refuses with ErrInvalid, before the store is touched, an op that can
// never enter it: no id, no epoch, a condition or write outside the closed
// sets, two writes to one field.
func check(op Op) error {
	switch {
	case op.ID == "":
		return invalid("an op needs an id")
	case op.Epoch < 1:
		return invalid("op %s needs an epoch", op.ID)
	}
	for _, e := range op.Expect {
		switch e.(type) {
		case CardGen, CardState, CardHolder, NeedsLanded, RowIs, RowUnder, StreamHeld, CardAbsent, AtSeq:
		default:
			return invalid("op %s: unknown condition %T", op.ID, e)
		}
	}
	seen := map[field]bool{}
	for _, w := range op.Writes {
		fs, ok := fields(w)
		if !ok {
			return invalid("op %s: unknown write %T", op.ID, w)
		}
		for _, f := range fs {
			if seen[f] {
				return invalid("op %s: two writes to %s %s field %s", op.ID, f.table, f.key, f.name)
			}
			seen[f] = true
		}
	}
	return nil
}

// checkClear is check for a clear: kind clear, no writes, at most AtSeq.
func checkClear(op Op) (Op, error) {
	op.Kind = cmpOr(op.Kind, "clear")
	if err := check(op); err != nil {
		return op, err
	}
	if op.Kind != "clear" || len(op.Writes) > 0 {
		return op, invalid("op %s: a clear is kind clear with no writes", op.ID)
	}
	for _, e := range op.Expect {
		if _, ok := e.(AtSeq); !ok {
			return op, invalid("op %s: a clear takes only an AtSeq condition, not %T", op.ID, e)
		}
	}
	return op, nil
}

// receipt is the receipt of an op that was logged at seq.
func receipt(op Op, reason string, after *State, seq int64) Receipt {
	r := Receipt{Op: op.ID, Kind: op.Kind, Actor: op.Actor, Epoch: op.Epoch, At: op.At, Note: op.Note, Accepted: reason == "", Reason: reason, Seq: seq, After: after}
	if after != nil {
		r.NewEpoch = after.Epoch
	}
	return r
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func oneOf(what, v string, set []string) string {
	if slices.Contains(set, v) {
		return ""
	}
	return fmt.Sprintf("%s %q is not one of %s", what, v, strings.Join(set, ", "))
}

// work is one operation being run against a txn: the touched rows before and
// after, kept for the event and the receipt.
type work struct {
	tx            txn
	op            Op
	before, after State
}

// run checks the epoch and every condition, then performs every write in
// order. It returns a refusal reason ("" when accepted); an error is a
// failure of the txn. On a refusal the caller discards whatever the txn did.
func run(tx txn, epoch int, op Op) (string, *State, *State, error) {
	w := &work{tx: tx, op: op}
	if op.Epoch != epoch {
		return fmt.Sprintf("the op is for epoch %d, the store is at epoch %d", op.Epoch, epoch), nil, nil, nil
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

// runClear is run for a clear: the epoch and the seq fence, then the new
// epoch with every row carried into it, down and with no flaps.
func runClear(tx txn, epoch int, op Op) (string, *State, error) {
	r, _, _, err := run(tx, epoch, Op{Epoch: op.Epoch, Expect: op.Expect})
	if r != "" || err != nil {
		return r, nil, err
	}
	rows, err := tx.rows()
	if err != nil {
		return "", nil, err
	}
	if err := tx.open(); err != nil {
		return "", nil, err
	}
	after := &State{Epoch: epoch + 1}
	for _, row := range rows {
		row.State, row.Flaps = "down", 0
		if err := tx.putRow(row); err != nil {
			return "", nil, err
		}
		if after.Rows == nil {
			after.Rows = map[string]Row{}
		}
		after.Rows[row.Name] = row
	}
	return "", after, nil
}

func noneIfEmpty(holder string) string { return cmpOr(holder, "none") }

func heldWord(held bool) string {
	if held {
		return "held"
	}
	return "not held"
}

func (w *work) expect(e Expect) (string, error) {
	switch c := e.(type) {
	case CardGen:
		return w.cardIs(c.Card, func(k Card) string {
			return when(k.Gen != c.Gen, "card %s is at gen %d, expected gen %d", c.Card, k.Gen, c.Gen)
		})
	case CardState:
		return w.cardIs(c.Card, func(k Card) string {
			return when(k.State != c.State, "card %s is %s, expected %s", c.Card, k.State, c.State)
		})
	case CardHolder:
		return w.cardIs(c.Card, func(k Card) string {
			return when(k.Holder != c.Holder, "card %s has holder %s, expected %s", c.Card, noneIfEmpty(k.Holder), noneIfEmpty(c.Holder))
		})
	case NeedsLanded:
		return w.needsLanded(c.Card)
	case RowIs:
		row, r, err := w.mustRow(c.Row)
		if r != "" || err != nil {
			return r, err
		}
		return when(row.State != c.State, "row %s is %s, expected %s", c.Row, row.State, c.State), nil
	case RowUnder:
		return w.rowUnder(c)
	case StreamHeld:
		s, r, err := w.mustStream(c.Stream)
		if r != "" || err != nil {
			return r, err
		}
		return when(s.Held != c.Held, "stream %s is %s, expected %s", c.Stream, heldWord(s.Held), heldWord(c.Held)), nil
	case CardAbsent:
		_, ok, err := w.tx.card(c.Card)
		return when(ok, "card %s exists", c.Card), err
	case AtSeq:
		seq, err := w.tx.lastSeq()
		return when(seq != c.Seq, "the store is at seq %d, expected seq %d", seq, c.Seq), err
	}
	return "", nil
}

// when is the reason when cond holds, else "".
func when(cond bool, format string, args ...any) string {
	if !cond {
		return ""
	}
	return fmt.Sprintf(format, args...)
}

func (w *work) cardIs(id string, test func(Card) string) (string, error) {
	c, r, err := w.mustCard(id)
	if r != "" || err != nil {
		return r, err
	}
	return test(c), nil
}

func (w *work) needsLanded(id string) (string, error) {
	c, r, err := w.mustCard(id)
	if r != "" || err != nil {
		return r, err
	}
	for _, n := range c.Needs {
		need, ok, err := w.tx.card(n)
		switch {
		case err != nil:
			return "", err
		case !ok:
			return fmt.Sprintf("need %s of card %s does not exist", n, id), nil
		case need.State != "landed":
			return fmt.Sprintf("need %s of card %s is %s, not landed", n, id, need.State), nil
		}
	}
	return "", nil
}

func (w *work) rowUnder(c RowUnder) (string, error) {
	row, r, err := w.mustRow(c.Row)
	if r != "" || err != nil {
		return r, err
	}
	n, err := w.tx.held(c.Row)
	return when(n >= row.Width, "row %s holds %d cards, its width is %d", c.Row, n, row.Width), err
}

func (w *work) mustCard(id string) (Card, string, error) {
	c, ok, err := w.tx.card(id)
	return c, when(err == nil && !ok, "card %s does not exist", id), err
}

func (w *work) mustRow(name string) (Row, string, error) {
	r, ok, err := w.tx.row(name)
	return r, when(err == nil && !ok, "row %s does not exist", name), err
}

func (w *work) mustStream(name string) (Stream, string, error) {
	s, ok, err := w.tx.stream(name)
	return s, when(err == nil && !ok, "stream %s does not exist", name), err
}

// The put helpers record a row's before-image the first time it is touched
// (only when it existed) and its after-image every time.

func (w *work) putStream(old Stream, existed bool, s Stream) error {
	w.before.Streams, w.after.Streams = touch(w.before.Streams, w.after.Streams, s.Name, old, existed, s)
	return w.tx.putStream(s)
}

// putCard also sets the gen: one more than before the op for a card that
// existed, however many of the op's writes touch it; 0 for a card the op adds.
func (w *work) putCard(old Card, existed bool, c Card) error {
	w.before.Cards, w.after.Cards = touch(w.before.Cards, w.after.Cards, c.ID, old, existed, c)
	c.Gen = 0
	if b, ok := w.before.Cards[c.ID]; ok {
		c.Gen = b.Gen + 1
	}
	w.after.Cards[c.ID] = c
	return w.tx.putCard(c)
}

func (w *work) putRow(old Row, existed bool, r Row) error {
	w.before.Rows, w.after.Rows = touch(w.before.Rows, w.after.Rows, r.Name, old, existed, r)
	return w.tx.putRow(r)
}

func (w *work) putPolicy(old PolicyVersion, existed bool, p PolicyVersion) error {
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
	s.Streams, s.Cards, s.Rows, s.Policies = nilIfEmpty(s.Streams), nilIfEmpty(s.Cards), nilIfEmpty(s.Rows), nilIfEmpty(s.Policies)
	if s.Epoch == 0 && s.Streams == nil && s.Cards == nil && s.Rows == nil && s.Policies == nil {
		return nil
	}
	return &s
}

func nilIfEmpty[K comparable, V any](m map[K]V) map[K]V {
	if len(m) == 0 {
		return nil
	}
	return m
}
