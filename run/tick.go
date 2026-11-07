package run

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Health is the chooser's check: the advisory lock's session and the
// sprint hook are both alive.
type Health interface{ Check(context.Context) error }

// Asking is layer 5 as run calls it (policy.Engine).
type Asking interface {
	AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure)
}

// TickCounts are one tick's numbers, for the tick line.
type TickCounts struct{ Folded, Applied, Refused, Edges int }

// Ticker is run's tick (LAYER-6 section 2): one steps View built once at
// the cold start and folded after, the folded snapshot beside it for the
// pusher and the board, and what layer 3 needs between ticks. Ticks are
// serial: Tick is called by one goroutine.
type Ticker struct {
	store    store.Store
	health   Health
	policy   Asking
	view     *steps.View
	snap     store.Snapshot
	inbox    []steps.InboxItem
	inflight map[string]steps.EdgeRequest
	refused  []steps.RefusedOp
	seen     map[string]time.Time
	prev     int
	counts   TickCounts
	push     *Pusher
	limits   policy.Limits
	timeouts policy.Timeouts
}

// Limits and Timeouts are the last answered tick's (the active policy's):
// run reads the roster at Limits.FleetWidthCap and bounds a tick by
// Timeouts.Tick. Before the first answer they are zero; Serve seeds them.
func (t *Ticker) Limits() policy.Limits     { return t.limits }
func (t *Ticker) Timeouts() policy.Timeouts { return t.timeouts }

// Push sends this tick's envelopes (Pusher.Push) and returns the
// recipients whose send failed.
func (t *Ticker) Push(ctx context.Context, b bus.Bus, now time.Time) []PushFailure {
	return t.push.Push(ctx, b, t.store, t.snap.Epoch, now)
}

// judgementOp is the tick's policy judgements (policy.Judgements): one per
// failing version (a row the roster does not name is the steps'), as one op on
// the snapshot the questions were asked on; its id is that seq's, so a
// replay applies it once.
func judgementOp(a *asker, o Observed) (store.Op, bool) {
	js := policy.Judgements(a.snap, o.Roster, a.failures)
	if len(js) == 0 {
		return store.Op{}, false
	}
	op := store.Op{ID: fmt.Sprintf("judge.policy.%d.%d", a.snap.Epoch, a.snap.Seq), Epoch: a.snap.Epoch, Actor: "sprint", Kind: "judgement", At: o.Now}
	for _, j := range js {
		op.Writes = append(op.Writes, j)
	}
	return op, true
}

// NewTicker reads the one cold snapshot; later reads are Events only
// (LAYER-1 "Scale"; no walk from seq 0, drift row 23).
func NewTicker(ctx context.Context, st store.Store, h Health, p Asking, now time.Time) (*Ticker, error) {
	if err := h.Check(ctx); err != nil {
		return nil, err
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: cold snapshot: %w", err)
	}
	return &Ticker{store: st, health: h, policy: p, view: steps.Build(snap, now), snap: snapshotCopy(snap),
		inflight: map[string]steps.EdgeRequest{}, seen: map[string]time.Time{}, push: NewPusher()}, nil
}

// Snapshot is run's folded copy, the caller's to keep.
func (t *Ticker) Snapshot() store.Snapshot { return snapshotCopy(t.snap) }

// Counts are the last tick's numbers.
func (t *Ticker) Counts() TickCounts { return t.counts }

// Fold reads the events since the last fold, in pages, into the View and
// the copy. A clear (a new epoch) drops everything layer 3 carried for the
// old one: the inbox, the requests in flight and the refusals (Stella's
// finding 2: an old effect never reaches the new epoch).
func (t *Ticker) Fold(ctx context.Context) error {
	for {
		events, err := t.store.Events(ctx, t.snap.Seq, store.MaxEvents)
		if err != nil {
			return fmt.Errorf("run: fold: %w", err)
		}
		if len(events) == 0 {
			return nil
		}
		epoch := t.snap.Epoch
		t.view.Fold(events)
		for _, e := range events {
			t.snap = store.Fold(t.snap, []store.Event{e})
			t.push.Observe(e, t.snap)
			if p, ok := policy.PrevOf(e); ok {
				t.prev = p
			}
		}
		if t.snap.Epoch != epoch {
			t.inbox, t.refused, t.inflight = nil, nil, map[string]steps.EdgeRequest{}
		}
		t.counts.Folded += len(events)
		if len(events) < store.MaxEvents {
			return nil
		}
	}
}

// Observed is what run read outside the store this tick: who is on the bus
// (as row names), the roster, the stream heads, and the edge results that
// finished since the last tick.
type Observed struct {
	Now     time.Time
	Present []string
	Roster  roster.Roster
	Heads   map[string]string
	Results []steps.InboxItem
}

// Tick is one tick: check the lock, fold, and, unless the sprint is stopped,
// plan with layer 3 and apply the plan's ops in order (LAYER-6 section 2).
// It returns the edge requests to dispatch: those whose After op was
// accepted. While stopped it plans and applies nothing and returns none
// (drift row 4; Run.tla NoTickAfterObservedStop): the results that arrive
// wait in the inbox for the start.
func (t *Ticker) Tick(ctx context.Context, o Observed) ([]steps.EdgeRequest, error) {
	t.counts = TickCounts{}
	t.inbox = append(t.inbox, t.fresh(o.Results)...) // kept even if this tick ends early
	if err := t.health.Check(ctx); err != nil {
		return nil, err
	}
	if err := t.Fold(ctx); err != nil {
		return nil, err
	}
	for _, name := range o.Present {
		t.seen[name] = o.Now
	}
	if t.snap.Stopped {
		return nil, nil
	}
	if err := t.raiseSeen(ctx, o); err != nil {
		return nil, err
	}
	o.Roster = o.Roster.Advertised(t.snap.Rows) // a worker's own width and tiers (defect 74)
	in := steps.Inputs{Now: o.Now, Inbox: t.inbox, Inflight: t.requests(), Heads: maps.Clone(o.Heads),
		Seen: maps.Clone(t.seen), Refused: t.refused, Roster: o.Roster}
	a := t.asker(ctx, o.Roster)
	plan := steps.Tick(t.view, in, a)
	if a.asked {
		t.limits, t.timeouts = a.answers.Limits, a.answers.Timeouts
	}
	ops := plan.Ops
	if j, ok := judgementOp(a, o); ok {
		ops = append(ops, j)
	}
	accepted, err := t.apply(ctx, ops)
	if err != nil {
		return nil, err
	}
	t.retire(plan, accepted)
	return t.dispatch(plan.Edges, accepted), nil
}

// raiseSeen is the run loop's Seen (steps' runEvents: presence is the bus's
// fact; Steps.tla EnvMove's flip up): each row down in the store and present
// on the bus this tick comes up, one op each, applied before the plan so a
// row that has just hooked in is open for this tick's deal. Gone, the flip
// down, is the steps' silence rule. Without this a hooked row stayed down
// and nothing was ever dealt (the end-to-end test, 2026-11-07).
func (t *Ticker) raiseSeen(ctx context.Context, o Observed) error {
	var ops []store.Op
	for _, name := range o.Present {
		if r, ok := t.snap.Rows[name]; !ok || r.State != card.Down {
			continue
		}
		op, why := steps.Verb(t.view, steps.Inputs{Now: o.Now}, steps.Request{Actor: steps.Actor, Row: name, Event: card.Seen{}})
		if why != nil {
			continue
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return nil
	}
	_, err := t.apply(ctx, ops)
	return err
}

// fresh is results not already in the inbox and for a request in flight
// (dedup by id within the epoch; a result for no request is a repeat).
func (t *Ticker) fresh(results []steps.InboxItem) []steps.InboxItem {
	var out []steps.InboxItem
	for _, r := range results {
		dup := slices.ContainsFunc(t.inbox, func(i steps.InboxItem) bool { return i.ID == r.ID })
		if _, ok := t.inflight[r.Request]; ok && !dup {
			out = append(out, r)
		}
	}
	return out
}

func (t *Ticker) requests() []steps.EdgeRequest {
	out := make([]steps.EdgeRequest, 0, len(t.inflight))
	for _, k := range slices.Sorted(maps.Keys(t.inflight)) {
		out = append(out, t.inflight[k])
	}
	return out
}

// apply applies ops in order, one Apply each, folding after each. A refused
// op is kept for the next tick's Inputs.Refused. An unknown outcome is
// replayed by id before anything else (layer 1: an op id applies once).
func (t *Ticker) apply(ctx context.Context, ops []store.Op) (map[string]bool, error) {
	t.refused = nil
	accepted := map[string]bool{}
	for _, op := range ops {
		if err := t.health.Check(ctx); err != nil {
			return accepted, err
		}
		// A stop that commits after the fold refuses the op (store.Running;
		// Run.tla NoAcceptedTickWhileStopped).
		op.Expect = append(slices.Clone(op.Expect), store.Running{})
		r, err := t.store.Apply(ctx, op)
		if errors.Is(err, store.ErrUnknown) {
			var found bool
			r, found, err = t.store.Replay(ctx, op.ID)
			if err == nil && !found {
				err = fmt.Errorf("run: op %s: outcome unknown and not in the log: %w", op.ID, store.ErrUnknown)
			}
		}
		if err != nil {
			return accepted, fmt.Errorf("run: apply %s: %w", op.ID, err)
		}
		if r.Accepted {
			accepted[op.ID] = true
			t.counts.Applied++
		} else {
			t.counts.Refused++
			t.refused = append(t.refused, steps.RefusedOp{ID: op.ID, Reason: r.Reason, Groups: steps.OpGroups(op)})
		}
		if err := t.Fold(ctx); err != nil {
			return accepted, err
		}
	}
	return accepted, nil
}

// retire sets the next inbox: the plan's carry first, then each item the
// plan grouped into a refused op (it is answered again next tick); an item
// neither carried nor in a refused op was consumed, and its request leaves
// flight.
func (t *Ticker) retire(plan steps.Plan, accepted map[string]bool) {
	refusedItem := map[string]bool{}
	for _, op := range plan.Ops {
		if accepted[op.ID] {
			continue
		}
		for _, g := range steps.OpGroups(op) {
			refusedItem[g.Item] = g.Item != ""
		}
	}
	carried := map[string]bool{}
	next := slices.Clone(plan.Carry)
	for _, c := range plan.Carry {
		carried[c.ID] = true
	}
	for _, it := range t.inbox {
		switch {
		case carried[it.ID]:
		case refusedItem[it.ID]:
			next = append(next, it)
		default:
			delete(t.inflight, it.Request)
		}
	}
	t.inbox = next
}

// dispatch is the plan's edge requests whose After op was accepted, now in
// flight; one not sent is not kept (steps reissues it by the same id).
func (t *Ticker) dispatch(edges []steps.EdgeRequest, accepted map[string]bool) []steps.EdgeRequest {
	var out []steps.EdgeRequest
	for _, e := range edges {
		if _, busy := t.inflight[e.ID]; busy || (e.After != "" && !accepted[e.After]) {
			continue
		}
		t.inflight[e.ID] = e
		out = append(out, e)
	}
	t.counts.Edges = len(out)
	return out
}
