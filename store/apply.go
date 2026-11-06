package store

import (
	"cmp"
	"fmt"
)

// txn is one transaction as the shared rules see it, run with the writer
// lock held: reads by key, whole-row upserts in its epoch, and the event log.
// PG and Fake each implement it, so the protocol, conditions, writes and
// refusal reasons are written once, here.
type txn interface {
	card(id string) (Card, bool, error)
	stream(name string) (Stream, bool, error)
	row(name string) (Row, bool, error)
	policy(version int) (PolicyVersion, bool, error)
	policies() (last int, active []PolicyVersion, err error)
	held(row string) (int, error) // live cards (ready or working) whose holder is row
	lastSeq() (int64, error)
	rows() ([]Row, error) // every row of the epoch
	open() error          // epoch n = n + 1; later puts go to the new epoch
	putCard(c Card) error
	putStream(s Stream) error
	putRow(r Row) error
	putPolicy(p PolicyVersion) error
	replay(opID string) (Receipt, bool, error) // the stored receipt of an op id
	mark() error                               // remember the state before the rules
	discard() error                            // back to the mark: a refusal changes nothing
	appendEvent(e Event) error                 // the op's event, at lastSeq + 1
}

// rules are the checks and writes of one op against tx in the store's epoch:
// a refusal reason ("" when accepted) and the touched rows before and after.
type rules func(tx txn, epoch int) (reason string, before, after *State, err error)

// protocol is what Apply and Clear share, inside the writer lock: the
// stored receipt for a known id whatever the body says; else the body is
// checked (ErrInvalid, nothing logged), the rules run, a refusal is
// discarded, and the event is appended with the receipt.
func protocol(tx txn, epoch int, op Op, valid func(Op) error, rule rules) (Receipt, error) {
	if r, ok, err := tx.replay(op.ID); err != nil || ok {
		return r, err
	}
	if err := valid(op); err != nil {
		return Receipt{}, err
	}
	if err := tx.mark(); err != nil {
		return Receipt{}, err
	}
	reason, before, after, err := rule(tx, epoch)
	if err != nil {
		return Receipt{}, err
	}
	if reason != "" {
		before, after = nil, nil
		if err := tx.discard(); err != nil {
			return Receipt{}, err
		}
	}
	seq, err := tx.lastSeq()
	if err != nil {
		return Receipt{}, err
	}
	r := receipt(op, reason, after, seq+1)
	e := Event{Seq: r.Seq, Op: op.ID, Actor: op.Actor, Kind: op.Kind, Accepted: r.Accepted, Reason: reason, Before: before, After: after, Receipt: r, At: op.At, Epoch: epoch}
	return r, tx.appendEvent(e)
}

// receipt is the receipt of an op that was logged at seq.
func receipt(op Op, reason string, after *State, seq int64) Receipt {
	r := Receipt{Op: op.ID, Kind: op.Kind, Actor: op.Actor, Epoch: op.Epoch, At: op.At, Note: op.Note, Accepted: reason == "", Reason: reason, Seq: seq, After: after}
	if after != nil {
		r.NewEpoch = after.Epoch
	}
	return r
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
// failure of the txn, or ErrInvalid for an unknown condition.
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
// epoch with every row carried into it, down and with no flaps; the clear
// writes the row, so its gen rises by one.
func runClear(tx txn, epoch int, op Op) (string, *State, *State, error) {
	r, _, _, err := run(tx, epoch, Op{Epoch: op.Epoch, Expect: op.Expect})
	if r != "" || err != nil {
		return r, nil, nil, err
	}
	rows, err := tx.rows()
	if err != nil {
		return "", nil, nil, err
	}
	if err := tx.open(); err != nil {
		return "", nil, nil, err
	}
	after := &State{Epoch: epoch + 1}
	for _, row := range rows {
		row.State, row.Flaps, row.Gen = "down", 0, row.Gen+1
		if err := tx.putRow(row); err != nil {
			return "", nil, nil, err
		}
		if after.Rows == nil {
			after.Rows = map[string]Row{}
		}
		after.Rows[row.Name] = row
	}
	return "", nil, after, nil
}

func noneIfEmpty(holder string) string { return cmp.Or(holder, "none") }

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
		return cmp.Or(r, when(err == nil && row.State != c.State, "row %s is %s, expected %s", c.Row, row.State, c.State)), err
	case RowGen:
		row, r, err := w.mustRow(c.Row)
		return cmp.Or(r, when(err == nil && row.Gen != c.Gen, "row %s is at gen %d, expected gen %d", c.Row, row.Gen, c.Gen)), err
	case StreamHeld:
		s, r, err := w.mustStream(c.Stream)
		return cmp.Or(r, when(err == nil && s.Held != c.Held, "stream %s is %s, expected %s", c.Stream, heldWord(s.Held), heldWord(c.Held))), err
	case CardAbsent:
		_, ok, err := w.tx.card(c.Card)
		return when(ok, "card %s exists", c.Card), err
	case AtSeq:
		seq, err := w.tx.lastSeq()
		return when(seq != c.Seq, "the store is at seq %d, expected seq %d", seq, c.Seq), err
	default:
		return "", invalid("op %s: unknown condition %T", w.op.ID, e)
	}
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

// needsLanded reads the needs from the card itself. Needs always name cards
// of the epoch (put card and set needs check it, nothing is deleted).
func (w *work) needsLanded(id string) (string, error) {
	c, r, err := w.mustCard(id)
	for _, n := range c.Needs {
		if r != "" || err != nil {
			break
		}
		var need Card
		need, _, err = w.tx.card(n)
		r = when(err == nil && need.State != "landed", "need %s of card %s is %s, not landed", n, id, need.State)
	}
	return r, err
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

// putRow also sets the gen, once per op, as putCard does.
func (w *work) putRow(old Row, existed bool, r Row) error {
	w.before.Rows, w.after.Rows = touch(w.before.Rows, w.after.Rows, r.Name, old, existed, r)
	r.Gen = 0
	if b, ok := w.before.Rows[r.Name]; ok {
		r.Gen = b.Gen + 1
	}
	w.after.Rows[r.Name] = r
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
