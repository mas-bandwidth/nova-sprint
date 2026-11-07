package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// EdgeResult is an outcome received from an asynchronous edge. Layer 3 owns
// its contents and the order of any results it carries to a later tick.
type EdgeResult struct {
	ID, Kind string
	Epoch    int
	Payload  any
}
type EdgeRequest struct {
	ID, Kind string
	Payload  any
	After    []string
}
type RowAnswer struct {
	ID, Text string
	Row      string
	Refused  bool
	After    []string
}
type RefusedOp struct {
	Op      store.Op
	Receipt store.Receipt
}
type TickInput struct {
	Inbox    []EdgeResult
	Refused  []RefusedOp
	At       time.Time
	Presence []string
	Policy   *store.PolicyVersion
	Streams  map[string]StreamSettings
	Roster   roster.Roster
}
type TickPlan struct {
	Ops     []store.Op
	Edges   []EdgeRequest
	Answers []RowAnswer
	Carry   []EdgeResult
}

// Steps owns ordering, budgeting, packing and carry, as ruled for layer 3.
type Steps interface {
	Tick(context.Context, store.Snapshot, TickInput) (TickPlan, error)
}

// StepEvents observes the initial snapshot and each validated event batch.
type StepEvents interface {
	Init(store.Snapshot)
	Fold([]store.Event)
}

// TickEdges enqueues all committed effects atomically, without blocking on
// external work. An error means none were enqueued; keyed requests may be
// rederived by later steps.
type TickEdges interface {
	Committed(context.Context, TickPlan, []store.Receipt) error
}
type chooserHealth interface{ Check(context.Context) error }

type pendingTick struct {
	plan      TickPlan
	epoch     int
	next      int
	receipts  []store.Receipt
	uncertain bool
	fenced    bool
	queued    bool
	input     []EdgeResult
}

// Engine owns a single cold snapshot and incrementally folds committed events.
type Engine struct {
	mu                sync.Mutex
	store             store.Store
	guard             chooserHealth
	steps             Steps
	edges             TickEdges
	state             store.Snapshot
	inbox             []EdgeResult
	refused           []RefusedOp
	pending           *pendingTick
	policy            map[int]store.PolicyVersion
	streamPolicies    map[string]streamPolicy
	lastPlannedPolicy int
	folded            bool
	counts            TickCounts
}

// NewEngine reads one initial snapshot; later reads use Events only.
func NewEngine(ctx context.Context, s store.Store, guard chooserHealth, steps Steps, edges TickEdges) (*Engine, error) {
	if v, ok := steps.(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil { return nil, err }
	}
	if err := guard.Check(ctx); err != nil {
		return nil, err
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: snapshot: %w", err)
	}
	streamPolicies, err := loadStreamPolicies(ctx, s, snap.Seq)
	if err != nil {
		return nil, err
	}
	if observer, ok := steps.(StepEvents); ok {
		observer.Init(snapshotCopy(snap))
	}
	return &Engine{store: s, guard: guard, steps: steps, edges: edges, state: snap, policy: map[int]store.PolicyVersion{}, streamPolicies: streamPolicies}, nil
}

// Tick processes one scheduled tick. An unknown operation remains pinned until
// Replay or a same-ID retry resolves it; no new planning occurs in between.
func (e *Engine) Tick(ctx context.Context, incoming []EdgeResult) error {
	return e.tick(ctx, incoming, observation{})
}

// refresh folds a committed prefix without planning or dispatching work.
func (e *Engine) refresh(ctx context.Context) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.folded = false
	e.counts = TickCounts{}
	if err := e.guard.Check(ctx); err != nil {
		return false, err
	}
	if err := e.fold(ctx); err != nil {
		return false, err
	}
	return e.state.Stopped, nil
}

func (e *Engine) tick(ctx context.Context, incoming []EdgeResult, obs observation) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.folded = false
	e.counts = TickCounts{}
	e.inbox = mergeInbox(e.inbox, incoming)
	if err := e.guard.Check(ctx); err != nil {
		return err
	}
	if err := e.fold(ctx); err != nil {
		return err
	}
	e.discardStalePending()
	if e.state.Stopped {
		return nil
	}
	if e.pending != nil {
		return e.applyPending(ctx)
	}
	policy, err := e.activePolicy(ctx)
	if err != nil {
		return err
	}
	streams, err := streamSettingsFor(e.state, e.streamPolicies)
	if err != nil {
		return err
	}
	plan, err := e.steps.Tick(ctx, snapshotCopy(e.state), TickInput{Inbox: e.inbox, Refused: e.refused, At: obs.at, Presence: append([]string(nil), obs.presence...), Policy: policy, Streams: streams, Roster: copyRunRoster(obs.roster)})
	if err != nil {
		return fmt.Errorf("run: steps: %w", err)
	}
	e.lastPlannedPolicy = e.state.Policy
	saved := append([]EdgeResult(nil), e.inbox...)
	e.inbox, e.refused = nil, nil
	e.pending = &pendingTick{plan: plan, epoch: e.state.Epoch, input: saved}
	return e.applyPending(ctx)
}

func (e *Engine) applyPending(ctx context.Context) error {
	p := e.pending
	for p.next < len(p.plan.Ops) {
		if err := e.guard.Check(ctx); err != nil {
			return err
		}
		if e.state.Stopped {
			return nil
		}
		if !p.fenced {
			op := &p.plan.Ops[p.next]
			op.Expect = append(op.Expect, store.AtSeq{Seq: e.state.Seq})
			p.fenced = true
		}
		op := p.plan.Ops[p.next]
		receipt, err := e.applyOne(ctx, op, p.uncertain)
		if err != nil {
			p.uncertain = errors.Is(err, store.ErrUnknown)
			return err
		}
		p.uncertain = false
		p.fenced = false
		p.receipts = append(p.receipts, receipt)
		if receipt.Accepted {
			e.counts.Applied++
		} else {
			e.counts.Refused++
		}
		p.next++
		if !receipt.Accepted {
			e.refused = append(e.refused, RefusedOp{Op: op, Receipt: receipt})
		}
		if err := e.fold(ctx); err != nil {
			return err
		}
		if e.discardStalePending() { return nil }
	}
	if !p.queued {
		if len(e.refused) > 0 {
			e.inbox = mergeInbox(p.plan.Carry, p.input, e.inbox)
		} else {
			e.inbox = mergeInbox(p.plan.Carry, e.inbox)
		}
		p.queued = true
	}
	if err := e.fold(ctx); err != nil {
		return err
	}
	if e.discardStalePending() { return nil }
	if e.state.Stopped {
		return nil
	}
	e.pending = nil
	if err := e.edges.Committed(ctx, committedPlan(p.plan, p.receipts), p.receipts); err != nil {
		return fmt.Errorf("run: enqueue edges: %w", err)
	}
	return nil
}

func (e *Engine) discardStalePending() bool {
	stale := e.pending != nil && e.pending.epoch != e.state.Epoch
	if stale { e.pending = nil }
	kept := e.inbox[:0]
	for _, item := range e.inbox { if item.Epoch == e.state.Epoch { kept = append(kept, item) } }
	e.inbox = kept
	refused := e.refused[:0]
	for _, item := range e.refused { if item.Op.Epoch == e.state.Epoch { refused = append(refused, item) } }
	e.refused = refused
	return stale
}
