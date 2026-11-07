package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// EdgeResult is an outcome received from an asynchronous edge. Layer 3 owns
// its contents and the order of any results it carries to a later tick.
type EdgeResult struct {
	ID, Kind string
	Payload  any
}
type EdgeRequest struct {
	ID, Kind string
	Payload  any
	After    []string
}
type RowAnswer struct {
	ID, Text string
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

// TickEdges enqueues committed effects without blocking on external work.
// A rejected enqueue is surfaced; later steps rederive the keyed request.
type TickEdges interface {
	Committed(context.Context, TickPlan, []store.Receipt) error
}
type chooserHealth interface{ Check(context.Context) error }

type pendingTick struct {
	plan      TickPlan
	next      int
	receipts  []store.Receipt
	uncertain bool
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
	lastPlannedPolicy int
	folded            bool
}

// NewEngine reads one initial snapshot; later reads use Events only.
func NewEngine(ctx context.Context, s store.Store, guard chooserHealth, steps Steps, edges TickEdges) (*Engine, error) {
	if err := guard.Check(ctx); err != nil {
		return nil, err
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: snapshot: %w", err)
	}
	return &Engine{store: s, guard: guard, steps: steps, edges: edges, state: snap, policy: map[int]store.PolicyVersion{}}, nil
}

// Tick processes one scheduled tick. An unknown operation remains pinned until
// Replay or a same-ID retry resolves it; no new planning occurs in between.
func (e *Engine) Tick(ctx context.Context, incoming []EdgeResult) error {
	return e.tick(ctx, incoming, observation{})
}
func (e *Engine) tick(ctx context.Context, incoming []EdgeResult, obs observation) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.folded = false
	e.inbox = mergeInbox(e.inbox, incoming)
	if err := e.guard.Check(ctx); err != nil {
		return err
	}
	if err := e.fold(ctx); err != nil {
		return err
	}
	if e.pending != nil {
		return e.applyPending(ctx)
	}
	policy, err := e.activePolicy(ctx)
	if err != nil {
		return err
	}
	plan, err := e.steps.Tick(ctx, snapshotCopy(e.state), TickInput{Inbox: e.inbox, Refused: e.refused, At: obs.at, Presence: append([]string(nil), obs.presence...), Policy: policy})
	if err != nil {
		return fmt.Errorf("run: steps: %w", err)
	}
	plan.Ops = annotatePolicy(plan.Ops, e.state.Policy)
	e.lastPlannedPolicy = e.state.Policy
	saved := append([]EdgeResult(nil), e.inbox...)
	e.inbox, e.refused = nil, nil
	e.pending = &pendingTick{plan: plan, input: saved}
	return e.applyPending(ctx)
}

func (e *Engine) applyPending(ctx context.Context) error {
	p := e.pending
	for p.next < len(p.plan.Ops) {
		if err := e.guard.Check(ctx); err != nil {
			return err
		}
		op := p.plan.Ops[p.next]
		receipt, err := e.applyOne(ctx, op, p.uncertain)
		if err != nil {
			p.uncertain = errors.Is(err, store.ErrUnknown)
			return err
		}
		p.uncertain = false
		p.receipts = append(p.receipts, receipt)
		p.next++
		if !receipt.Accepted {
			e.refused = append(e.refused, RefusedOp{Op: op, Receipt: receipt})
		}
		if err := e.fold(ctx); err != nil {
			return err
		}
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
	e.pending = nil
	if err := e.edges.Committed(ctx, committedPlan(p.plan, p.receipts), p.receipts); err != nil {
		return fmt.Errorf("run: enqueue edges: %w", err)
	}
	return nil
}
