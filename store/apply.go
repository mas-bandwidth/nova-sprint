package store

import (
	"cmp"
	"fmt"
	"math"
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
	working(row string) (int, error)                       // cards in working whose holder is row: what width counts
	needed(row string, tiers []string) (Card, bool, error) // the live (ready or working) consumer held by row, lowest id, whose tier is not in tiers
	lastSeq() (int64, error)
	rows() ([]Row, error) // every row of the epoch
	open() error          // epoch n = n + 1; later puts go to the new epoch
	putCard(c Card) error
	putStream(s Stream) error
	putRow(r Row) error
	putPolicy(p PolicyVersion) error
	judgement(id string) (Judgement, bool, error)
	putJudgement(j Judgement) error
	cursor(name string) (int64, bool, error)
	putCursor(name string, seq int64) error
	replay(opID string) (Receipt, bool, error) // the stored receipt of an op id
	discard() error                            // back to the state the transaction began with: a refusal changes nothing
	appendEvent(e Event) error                 // the op's event, at lastSeq + 1
}

// rules are the checks and writes of one op against tx in the store's epoch:
// a refusal reason ("" when accepted) and the touched rows before and after.
type rules func(tx txn, epoch int) (reason string, before, after *State, err error)

// writer runs protocol in one transaction of a store's own: Fake.do, PG.write.
type writer func(op Op, valid func(Op) error, rule rules) (Receipt, error)

// applyOp is the Apply both stores share: the id checked, the op normalized,
// then one transaction of the store's own.
func applyOp(do writer, op Op) (Receipt, error) {
	if err := checkID(op); err != nil {
		return Receipt{}, err
	}
	op.At = at(op.At)
	return do(op, check, func(tx txn, epoch int) (string, *State, *State, error) { return run(tx, epoch, op) })
}

// clearOp is the Clear both stores share, as applyOp.
func clearOp(do writer, op Op) (Receipt, error) {
	if err := checkID(op); err != nil {
		return Receipt{}, err
	}
	op.At = at(op.At)
	op.Kind = cmp.Or(op.Kind, "clear")
	return do(op, checkClear, func(tx txn, epoch int) (string, *State, *State, error) { return runClear(tx, epoch, op) })
}

// protocol is what Apply and Clear share, inside the writer lock: the
// stored receipt for a known id whatever the body says; else the whole body
// is checked (ErrInvalid, nothing logged) before the epoch or any condition,
// the active policy version is read (the rules in force when the op is
// decided, kept on its event), the rules run, a refusal is discarded, and
// the event is appended with the receipt.
func protocol(tx txn, epoch int, op Op, valid func(Op) error, rule rules) (Receipt, error) {
	if r, ok, err := tx.replay(op.ID); err != nil || ok {
		return r, err
	}
	if err := valid(op); err != nil {
		return Receipt{}, err
	}
	_, active, err := tx.policies()
	if err != nil {
		return Receipt{}, err
	}
	policy := 0
	for _, p := range active {
		policy = p.Version
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
	e := Event{Seq: r.Seq, Op: op.ID, Actor: op.Actor, Kind: op.Kind, Accepted: r.Accepted, Reason: reason, Before: before, After: after, Receipt: r, At: op.At, Epoch: epoch, Policy: policy}
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
// failure of the txn (check has already refused anything outside the sets).
func run(tx txn, epoch int, op Op) (string, *State, *State, error) {
	w := &work{tx: tx, op: op}
	if op.Epoch != epoch {
		return fmt.Sprintf("the op is for epoch %d, the store is at epoch %d: take a new snapshot", op.Epoch, epoch), nil, nil, nil
	}
	for _, e := range op.Expect {
		if r, err := condOf(e).holds(w); r != "" || err != nil {
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
	n, err := next("epoch", epoch)
	if err != nil {
		return "", nil, nil, err
	}
	if err := tx.open(); err != nil {
		return "", nil, nil, err
	}
	after := &State{Epoch: n}
	for _, row := range rows {
		row.State, row.Flaps = "down", 0
		if row.Gen, err = next("row "+row.Name+" gen", row.Gen); err != nil {
			return "", nil, nil, err
		}
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

// next is n + 1 for a counter kept in a PostgreSQL integer (a gen, the
// epoch, a policy version), or a plain error naming it when n + 1 would not
// fit: the op fails, nothing lands, and the store says which counter is full.
func next(what string, n int) (int, error) {
	if n >= math.MaxInt32 {
		return 0, fmt.Errorf("store: %s is %d, the most its column holds; it cannot rise", what, n)
	}
	return n + 1, nil
}

func noneIfEmpty(holder string) string { return cmp.Or(holder, "none") }

func heldWord(held bool) string {
	if held {
		return "held"
	}
	return "not held"
}

// cond is one condition's entry in the one table of the conditions: the
// check of its body (keys, ranges and closed sets, by the writes' rules) and
// its test inside the transaction. The zero cond is a value outside the
// closed set.
type cond struct {
	check func(c *checker)
	holds func(w *work) (string, error)
}

// condOf is the one table of the conditions, keyed by type. A new condition
// is one entry here.
func condOf(e Expect) cond {
	switch v := e.(type) {
	case CardGen:
		return cardCond(v.Card, func(c *checker) { c.num("gen", v.Gen) }, func(k Card) string {
			return when(k.Gen != v.Gen, "card %s is at gen %d, expected gen %d", v.Card, k.Gen, v.Gen)
		})
	case CardState:
		return cardCond(v.Card, func(c *checker) { c.oneOf("card state", v.State, CardStates) }, func(k Card) string {
			return when(k.State != v.State, "card %s is %s, expected %s", v.Card, k.State, v.State)
		})
	case CardHolder: // holder "" is none, not an empty key
		return cardCond(v.Card, func(c *checker) { c.name(v.Holder) }, func(k Card) string {
			return when(k.Holder != v.Holder, "card %s has holder %s, expected %s", v.Card, noneIfEmpty(k.Holder), noneIfEmpty(v.Holder))
		})
	case NeedsLanded:
		return cond{func(c *checker) { c.key("card", v.Card) }, func(w *work) (string, error) { return w.needsLanded(v.Card) }}
	case CardAbsent:
		return cond{func(c *checker) { c.key("card", v.Card) }, func(w *work) (string, error) {
			_, ok, err := w.tx.card(v.Card)
			return when(ok, "card %s exists", v.Card), err
		}}
	case RowIs:
		return rowCond(v.Row, func(c *checker) { c.oneOf("row state", v.State, RowStates) }, func(r Row) string {
			return when(r.State != v.State, "row %s is %s, expected %s", v.Row, r.State, v.State)
		})
	case RowGen:
		return rowCond(v.Row, func(c *checker) { c.num("gen", v.Gen) }, func(r Row) string {
			return when(r.Gen != v.Gen, "row %s is at gen %d, expected gen %d", v.Row, r.Gen, v.Gen)
		})
	case RowHeld:
		return rowCond(v.Row, func(*checker) {}, func(r Row) string {
			return when(r.Held != v.Held, "row %s is %s, expected %s", v.Row, heldWord(r.Held), heldWord(v.Held))
		})
	case StreamHeld:
		return cond{func(c *checker) { c.key("stream", v.Stream) }, func(w *work) (string, error) {
			s, r, err := w.mustStream(v.Stream)
			return cmp.Or(r, when(err == nil && s.Held != v.Held, "stream %s is %s, expected %s", v.Stream, heldWord(s.Held), heldWord(v.Held))), err
		}}
	case JudgementOpen:
		return cond{func(c *checker) { c.key("judgement", v.ID) }, func(w *work) (string, error) {
			j, r, err := w.mustJudgement(v.ID)
			return cmp.Or(r, when(err == nil && j.State != "open", "judgement %s is %s, expected open", v.ID, j.State)), err
		}}
	case AtSeq:
		return cond{func(c *checker) { c.num64("seq", v.Seq) }, func(w *work) (string, error) {
			seq, err := w.tx.lastSeq()
			return when(seq != v.Seq, "the store is at seq %d, expected seq %d", seq, v.Seq), err
		}}
	}
	return cond{}
}

// cardCond is a condition on card id: its key and check, then test on the
// card, which must exist.
func cardCond(id string, check func(*checker), test func(Card) string) cond {
	return cond{func(c *checker) { check(c.key("card", id)) }, func(w *work) (string, error) { return w.cardIs(id, test) }}
}

// rowCond is cardCond for a row.
func rowCond(name string, check func(*checker), test func(Row) string) cond {
	return cond{func(c *checker) { check(c.key("row", name)) }, func(w *work) (string, error) {
		row, r, err := w.mustRow(name)
		if r != "" || err != nil {
			return r, err
		}
		return test(row), nil
	}}
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

func (w *work) mustJudgement(id string) (Judgement, string, error) {
	j, ok, err := w.tx.judgement(id)
	return j, when(err == nil && !ok, "judgement %s does not exist", id), err
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
		var err error
		if c.Gen, err = next("card "+c.ID+" gen", b.Gen); err != nil {
			return err
		}
	}
	w.after.Cards[c.ID] = c
	return w.tx.putCard(c)
}

// putRow also sets the gen, once per op, as putCard does.
func (w *work) putRow(old Row, existed bool, r Row) error {
	w.before.Rows, w.after.Rows = touch(w.before.Rows, w.after.Rows, r.Name, old, existed, r)
	r.Gen = 0
	if b, ok := w.before.Rows[r.Name]; ok {
		var err error
		if r.Gen, err = next("row "+r.Name+" gen", b.Gen); err != nil {
			return err
		}
	}
	w.after.Rows[r.Name] = r
	return w.tx.putRow(r)
}

func (w *work) putPolicy(old PolicyVersion, existed bool, p PolicyVersion) error {
	w.before.Policies, w.after.Policies = touch(w.before.Policies, w.after.Policies, p.Version, old, existed, p)
	return w.tx.putPolicy(p)
}

func (w *work) putJudgement(old Judgement, existed bool, j Judgement) error {
	w.before.Judgements, w.after.Judgements = touch(w.before.Judgements, w.after.Judgements, j.ID, old, existed, j)
	return w.tx.putJudgement(j)
}

func (w *work) putCursor(name string, old int64, existed bool, seq int64) error {
	w.before.Cursors, w.after.Cursors = touch(w.before.Cursors, w.after.Cursors, name, old, existed, seq)
	return w.tx.putCursor(name, seq)
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
	s.Judgements, s.Cursors = nilIfEmpty(s.Judgements), nilIfEmpty(s.Cursors)
	if s.Epoch == 0 && s.Streams == nil && s.Cards == nil && s.Rows == nil && s.Policies == nil && s.Judgements == nil && s.Cursors == nil {
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
