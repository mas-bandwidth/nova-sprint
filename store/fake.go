package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
)

// Fake is an in-memory Store with the semantics of PG: the same conditions,
// writes, refusal reasons, receipts and idempotency, because both run the
// rules in apply.go and writes.go. It is for unit tests of the layers above
// only: every op clones every table and the log (O(n) per op, O(n^2) over a
// long log), which is right for a test and wrong for a sprint. Time comes
// from Op.At; the fake never reads a clock. Use NewFake.
type Fake struct {
	mu       sync.Mutex
	epoch    int
	streams  map[key]Stream
	cards    map[key]Card
	rows     map[key]Row
	policies map[int]PolicyVersion
	judges   map[key]Judgement
	cursors  map[string]int64 // global, as seq is: a clear carries them
	events   []fakeEvent
	seq0     int64          // the seq before events[0]: 0, or the snapshot's (NewFakeFrom)
	ops      map[string]int // op id -> index in events
	drops    map[string]bool
}

// key is a row's primary key: the epoch it was written in and its name.
type key struct {
	epoch int
	name  string
}

// fakeEvent is an event as PG stores it: its images as JSON, so a read returns
// exactly what PG would return, and no caller can change it.
type fakeEvent struct {
	Event
	before, after, receipt []byte
}

// NewFake is an empty store at epoch 1.
func NewFake() *Fake {
	return &Fake{epoch: 1, streams: map[key]Stream{}, cards: map[key]Card{}, rows: map[key]Row{}, policies: map[int]PolicyVersion{}, judges: map[key]Judgement{}, cursors: map[string]int64{}, ops: map[string]int{}, drops: map[string]bool{}}
}

// Drop makes the next Apply or Clear of op id report ErrUnknown, as if the
// connection dropped mid-commit; landed says whether the op lands anyway.
func (f *Fake) Drop(opID string, landed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drops[opID] = landed
}

// Apply runs op in a transaction of the fake's own (protocol, apply.go).
func (f *Fake) Apply(_ context.Context, op Op) (Receipt, error) { return applyOp(f.do, op) }

// Clear opens a new epoch, carrying the rows, and logs itself.
func (f *Fake) Clear(_ context.Context, op Op) (Receipt, error) { return clearOp(f.do, op) }

// do runs protocol against a copy of the state under the mutex, and keeps the
// copy and the event only when protocol returns no error. Drop's faults come
// first (not landed) or after the commit (landed).
func (f *Fake) do(op Op, valid func(Op) error, rule rules) (Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	landed, dropped := f.drops[op.ID]
	delete(f.drops, op.ID)
	if dropped && !landed {
		return Receipt{}, fmt.Errorf("%w: fake: connection dropped", ErrUnknown)
	}
	tx := &fakeTx{base: f, st: f.clone(), events: slices.Clone(f.events)}
	r, err := protocol(tx, f.epoch, op, valid, rule)
	if err != nil {
		return Receipt{}, err
	}
	f.epoch, f.streams, f.cards, f.rows, f.policies, f.judges, f.cursors = tx.st.epoch, tx.st.streams, tx.st.cards, tx.st.rows, tx.st.policies, tx.st.judges, tx.st.cursors
	if len(tx.events) > len(f.events) {
		f.events = tx.events
		f.ops[op.ID] = len(f.events) - 1
	}
	if dropped {
		return Receipt{}, fmt.Errorf("%w: fake: connection dropped after commit", ErrUnknown)
	}
	return decodeReceipt(encode(r)), nil
}

// Replay returns the stored receipt of an op id.
func (f *Fake) Replay(_ context.Context, opID string) (Receipt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.receipt(opID)
	return r, ok, nil
}

// receipt is the stored receipt of a committed op id; Replay and the
// transaction's replay share it.
func (f *Fake) receipt(opID string) (Receipt, bool) {
	i, ok := f.ops[opID]
	if !ok {
		return Receipt{}, false
	}
	return decodeReceipt(f.events[i].receipt), true
}

// Events returns up to max (capped at MaxEvents) events after afterSeq.
func (f *Fake) Events(_ context.Context, afterSeq int64, max int) ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	max = capEvents(max)
	var out []Event
	for _, fe := range f.events {
		if fe.Seq <= afterSeq {
			continue
		}
		if len(out) == max {
			break
		}
		e := fe.Event
		e.Before, e.After, e.Receipt = decodeState(fe.before), decodeState(fe.after), decodeReceipt(fe.receipt)
		out = append(out, e)
	}
	return out, nil
}

func capEvents(max int) int {
	if max <= 0 || max > MaxEvents {
		return MaxEvents
	}
	return max
}

// Snapshot is the current epoch's rows, every slice copied. Receipts and
// event images are decoded from JSON on every read, so they never alias.
func (f *Fake) Snapshot(_ context.Context) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := emptySnapshot(f.epoch)
	s.Seq = f.seq0 + int64(len(f.events))
	pick(s.Streams, f.streams, f.epoch)
	pick(s.Cards, f.cards, f.epoch)
	for id, c := range s.Cards {
		s.Cards[id] = cardCopy(c) // the caller's own copy: editing it never changes the store
	}
	pick(s.Rows, f.rows, f.epoch)
	for name, r := range s.Rows {
		r.Tiers = normSet(r.Tiers)
		s.Rows[name] = r
	}
	pick(s.Judgements, f.judges, f.epoch)
	maps.Copy(s.Cursors, f.cursors)
	for v, p := range f.policies {
		if p.Active {
			s.Policy = v
		}
	}
	return s, nil
}

func pick[V any](dst map[string]V, src map[key]V, epoch int) {
	for k, v := range src {
		if k.epoch == epoch {
			dst[k.name] = v
		}
	}
}

// Policy returns one policy version.
func (f *Fake) Policy(_ context.Context, version int) (PolicyVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.policies[version]
	if !ok {
		return p, fmt.Errorf("%w: policy version %d", ErrNotFound, version)
	}
	return p, nil
}

func (f *Fake) clone() *Fake {
	return &Fake{epoch: f.epoch, streams: maps.Clone(f.streams), cards: maps.Clone(f.cards), rows: maps.Clone(f.rows), policies: maps.Clone(f.policies), judges: maps.Clone(f.judges), cursors: maps.Clone(f.cursors)}
}

// fakeTx is the txn of one Fake transaction: st is the working copy of the
// state, events a copy of the log; do keeps both only when protocol succeeds.
type fakeTx struct {
	base   *Fake // the committed store, read only
	st     *Fake
	events []fakeEvent
}

func (t *fakeTx) k(name string) key { return key{t.st.epoch, name} }

func (t *fakeTx) card(id string) (Card, bool, error) {
	c, ok := t.st.cards[t.k(id)]
	return cardCopy(c), ok, nil
}

// cardCopy is a card with every list its own.
func cardCopy(c Card) Card {
	c.Needs, c.After, c.Paths, c.DoneWhen = normSet(c.Needs), normSet(c.After), normList(c.Paths), normList(c.DoneWhen)
	return c
}

func (t *fakeTx) judgement(id string) (Judgement, bool, error) {
	j, ok := t.st.judges[t.k(id)]
	return j, ok, nil
}

func (t *fakeTx) putJudgement(j Judgement) error { t.st.judges[t.k(j.ID)] = j; return nil }

func (t *fakeTx) cursor(name string) (int64, bool, error) {
	seq, ok := t.st.cursors[name]
	return seq, ok, nil
}

func (t *fakeTx) putCursor(name string, seq int64) error { t.st.cursors[name] = seq; return nil }

func (t *fakeTx) stream(name string) (Stream, bool, error) {
	s, ok := t.st.streams[t.k(name)]
	return s, ok, nil
}

func (t *fakeTx) row(name string) (Row, bool, error) {
	r, ok := t.st.rows[t.k(name)]
	r.Tiers = normSet(r.Tiers)
	return r, ok, nil
}

func (t *fakeTx) policy(v int) (PolicyVersion, bool, error) {
	p, ok := t.st.policies[v]
	return p, ok, nil
}

func (t *fakeTx) policies() (int, []PolicyVersion, error) {
	last, active := 0, []PolicyVersion(nil)
	for v, p := range t.st.policies {
		last = max(last, v)
		if p.Active {
			active = append(active, p)
		}
	}
	return last, active, nil
}

func (t *fakeTx) working(row string) (int, error) {
	n := 0
	for k, c := range t.st.cards {
		if k.epoch == t.st.epoch && c.Holder == row && c.State == "working" {
			n++
		}
	}
	return n, nil
}

func (t *fakeTx) needed(row string, tiers []string) (Card, bool, error) {
	var found Card
	for k, c := range t.st.cards {
		if k.epoch == t.st.epoch && c.Holder == row && (c.State == "ready" || c.State == "working") && !slices.Contains(tiers, c.Tier) && (found.ID == "" || c.ID < found.ID) {
			found = c
		}
	}
	return found, found.ID != "", nil
}

func (t *fakeTx) lastSeq() (int64, error) { return t.base.seq0 + int64(len(t.events)), nil }

func (t *fakeTx) rows() ([]Row, error) {
	var out []Row
	for k, r := range t.st.rows {
		if k.epoch == t.st.epoch {
			out = append(out, r)
		}
	}
	return out, nil
}

func (t *fakeTx) open() error { t.st.epoch++; return nil }

func (t *fakeTx) putCard(c Card) error { t.st.cards[t.k(c.ID)] = cardCopy(c); return nil }

func (t *fakeTx) putStream(s Stream) error { t.st.streams[t.k(s.Name)] = s; return nil }
func (t *fakeTx) putRow(r Row) error {
	r.Tiers = normSet(r.Tiers)
	t.st.rows[t.k(r.Name)] = r
	return nil
}
func (t *fakeTx) putPolicy(p PolicyVersion) error { t.st.policies[p.Version] = p; return nil }

func (t *fakeTx) replay(opID string) (Receipt, bool, error) {
	r, ok := t.base.receipt(opID)
	return r, ok, nil
}

func (t *fakeTx) discard() error { t.st = t.base.clone(); return nil }

func (t *fakeTx) appendEvent(e Event) error {
	before, after, rec := encode(e.Before), encode(e.After), encode(e.Receipt)
	e.Before, e.After, e.Receipt = nil, nil, Receipt{}
	t.events = append(t.events, fakeEvent{Event: e, before: before, after: after, receipt: rec})
	return nil
}

// encode is a value as JSON, or nil for a nil *State. The store's own types
// always encode: every time a receipt carries has passed check (years 0..9999).
func encode(v any) []byte {
	if s, ok := v.(*State); ok && s == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func decodeState(b []byte) *State {
	s, err := readState(b)
	if err != nil {
		panic(err) // the fake's images are written by encode
	}
	return s
}

// readState is an image as stored, or nil for none; an error for bytes that
// are not one (PG's events read it and return the error, never panic).
func readState(b []byte) (*State, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func decodeReceipt(b []byte) Receipt {
	var r Receipt
	if err := json.Unmarshal(b, &r); err != nil {
		panic(err) // written by encode
	}
	return r
}
