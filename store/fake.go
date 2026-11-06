package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
)

// Fake is an in-memory Store with the semantics of PG: the same conditions,
// writes, refusal reasons, receipts and idempotency, because both run the
// rules in apply.go and writes.go. It is for unit tests of the layers above
// only. Time comes from Op.At; the fake never reads a clock. Use NewFake.
type Fake struct {
	mu       sync.Mutex
	epoch    int
	streams  map[key]Stream
	cards    map[key]Card
	rows     map[key]Row
	policies map[int]PolicyVersion
	events   []fakeEvent
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
	return &Fake{epoch: 1, streams: map[key]Stream{}, cards: map[key]Card{}, rows: map[key]Row{}, policies: map[int]PolicyVersion{}, ops: map[string]int{}, drops: map[string]bool{}}
}

// Drop makes the next Apply or Clear of op id report ErrUnknown, as if the
// connection dropped mid-commit; landed says whether the op lands anyway.
func (f *Fake) Drop(opID string, landed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drops[opID] = landed
}

// Apply runs op against a copy of the state and keeps the copy only when it is
// accepted; the event is appended either way.
func (f *Fake) Apply(_ context.Context, op Op) (Receipt, error) {
	if err := check(op); err != nil {
		return Receipt{}, err
	}
	op = normalize(op)
	return f.do(op, func(tx *fakeTx) (string, *State, *State, error) { return run(tx, f.epoch, op) })
}

// Clear opens a new epoch, carrying the rows, and logs itself.
func (f *Fake) Clear(_ context.Context, op Op) (Receipt, error) {
	op, err := checkClear(op)
	if err != nil {
		return Receipt{}, err
	}
	op = normalize(op)
	return f.do(op, func(tx *fakeTx) (string, *State, *State, error) {
		r, after, err := runClear(tx, f.epoch, op)
		return r, nil, after, err
	})
}

// do is the protocol both share: the stored receipt for a known id, else the
// rules against a copy, kept when accepted, and the event.
func (f *Fake) do(op Op, rules func(*fakeTx) (string, *State, *State, error)) (Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.ops[op.ID]; ok {
		return decodeReceipt(f.events[i].receipt), nil
	}
	landed, dropped := f.drops[op.ID]
	delete(f.drops, op.ID)
	if dropped && !landed {
		return Receipt{}, fmt.Errorf("%w: fake: connection dropped", ErrUnknown)
	}
	next := f.clone()
	reason, before, after, err := rules(&fakeTx{f: next, events: len(f.events)})
	if err != nil {
		return Receipt{}, err
	}
	epoch := f.epoch
	if reason == "" {
		f.epoch, f.streams, f.cards, f.rows, f.policies = next.epoch, next.streams, next.cards, next.rows, next.policies
	} else {
		before, after = nil, nil
	}
	r := f.log(op, reason, before, after, epoch)
	if dropped {
		return Receipt{}, fmt.Errorf("%w: fake: connection dropped after commit", ErrUnknown)
	}
	return r, nil
}

func (f *Fake) log(op Op, reason string, before, after *State, epoch int) Receipt {
	seq := int64(len(f.events)) + 1
	r := receipt(op, reason, after, seq)
	e := Event{Seq: seq, Op: op.ID, Actor: op.Actor, Kind: op.Kind, Accepted: reason == "", Reason: reason, At: op.At, Epoch: epoch}
	f.events = append(f.events, fakeEvent{Event: e, before: encode(before), after: encode(after), receipt: encode(r)})
	f.ops[op.ID] = len(f.events) - 1
	return decodeReceipt(encode(r))
}

// Replay returns the stored receipt of an op id.
func (f *Fake) Replay(_ context.Context, opID string) (Receipt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.ops[opID]
	if !ok {
		return Receipt{}, false, nil
	}
	return decodeReceipt(f.events[i].receipt), true, nil
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
	s.Seq = int64(len(f.events))
	pick(s.Streams, f.streams, f.epoch)
	pick(s.Cards, f.cards, f.epoch)
	for id, c := range s.Cards {
		c.Needs = needs(c.Needs) // the caller's own copy: editing it never changes the store
		s.Cards[id] = c
	}
	pick(s.Rows, f.rows, f.epoch)
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
	return &Fake{epoch: f.epoch, streams: maps.Clone(f.streams), cards: maps.Clone(f.cards), rows: maps.Clone(f.rows), policies: maps.Clone(f.policies)}
}

// fakeTx is the txn of a Fake's copy; events is the count of events before it.
type fakeTx struct {
	f      *Fake
	events int
}

func (t *fakeTx) k(name string) key { return key{t.f.epoch, name} }

func (t *fakeTx) card(id string) (Card, bool, error) {
	c, ok := t.f.cards[t.k(id)]
	c.Needs = needs(c.Needs)
	return c, ok, nil
}

func (t *fakeTx) stream(name string) (Stream, bool, error) {
	s, ok := t.f.streams[t.k(name)]
	return s, ok, nil
}

func (t *fakeTx) row(name string) (Row, bool, error) {
	r, ok := t.f.rows[t.k(name)]
	return r, ok, nil
}

func (t *fakeTx) policy(v int) (PolicyVersion, bool, error) {
	p, ok := t.f.policies[v]
	return p, ok, nil
}

func (t *fakeTx) policies() (int, []PolicyVersion, error) {
	last, active := 0, []PolicyVersion(nil)
	for v, p := range t.f.policies {
		last = max(last, v)
		if p.Active {
			active = append(active, p)
		}
	}
	return last, active, nil
}

func (t *fakeTx) held(row string) (int, error) {
	n := 0
	for k, c := range t.f.cards {
		if k.epoch == t.f.epoch && c.Holder == row {
			n++
		}
	}
	return n, nil
}

func (t *fakeTx) lastSeq() (int64, error) { return int64(t.events), nil }

func (t *fakeTx) rows() ([]Row, error) {
	var out []Row
	for k, r := range t.f.rows {
		if k.epoch == t.f.epoch {
			out = append(out, r)
		}
	}
	return out, nil
}

func (t *fakeTx) open() error { t.f.epoch++; return nil }

func (t *fakeTx) putCard(c Card) error {
	c.Needs = needs(c.Needs)
	t.f.cards[t.k(c.ID)] = c
	return nil
}

func (t *fakeTx) putStream(s Stream) error        { t.f.streams[t.k(s.Name)] = s; return nil }
func (t *fakeTx) putRow(r Row) error              { t.f.rows[t.k(r.Name)] = r; return nil }
func (t *fakeTx) putPolicy(p PolicyVersion) error { t.f.policies[p.Version] = p; return nil }

// encode is a value as JSON, or nil for a nil *State.
func encode(v any) []byte {
	if s, ok := v.(*State); ok && s == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // the store's own types always encode
	}
	return b
}

func decodeState(b []byte) *State {
	if len(b) == 0 {
		return nil
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		panic(err) // written by encode
	}
	return &s
}

func decodeReceipt(b []byte) Receipt {
	var r Receipt
	if err := json.Unmarshal(b, &r); err != nil {
		panic(err) // written by encode
	}
	return r
}
