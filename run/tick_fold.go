package run

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func (e *Engine) applyOne(ctx context.Context, op store.Op, uncertain bool) (store.Receipt, error) {
	if uncertain {
		r, found, err := e.store.Replay(ctx, op.ID)
		if err != nil {
			return store.Receipt{}, fmt.Errorf("run: replay %s: %w: %v", op.ID, store.ErrUnknown, err)
		}
		if found {
			return r, nil
		}
	}
	r, err := e.store.Apply(ctx, op)
	if err != nil {
		return store.Receipt{}, fmt.Errorf("run: apply %s: %w", op.ID, err)
	}
	return r, nil
}

// A hot writer cannot make one tick chase an unbounded event stream.
const maxFoldPages = 16

func (e *Engine) fold(ctx context.Context) error {
	e.folded = false
	for page := 0; page < maxFoldPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := e.store.Events(ctx, e.state.Seq, store.MaxEvents)
		if err != nil {
			return fmt.Errorf("run: events: %w", err)
		}
		for i, event := range events {
			want := e.state.Seq + int64(i) + 1
			if event.Seq != want {
				return fmt.Errorf("run: event sequence gap: got %d, expected %d", event.Seq, want)
			}
		}
		if len(events) > 0 {
			e.state = foldSnapshot(e.state, events)
		}
		if len(events) < store.MaxEvents {
			e.folded = true
			return nil
		}
	}
	return fmt.Errorf("run: event catch-up continues next tick")
}
func foldSnapshot(s store.Snapshot, events []store.Event) store.Snapshot {
	policies := map[int]store.PolicyVersion{}
	if s.Policy != 0 {
		policies[s.Policy] = store.PolicyVersion{Active: true}
	}
	image := &store.State{Epoch: s.Epoch, Streams: s.Streams, Cards: s.Cards, Rows: s.Rows, Judgements: s.Judgements, Cursors: s.Cursors, Policies: policies}
	batch := make([]store.Event, 0, len(events)+1)
	batch = append(batch, store.Event{Seq: s.Seq, Accepted: true, After: image})
	batch = append(batch, events...)
	return store.Rebuild(batch)
}

// committedPlan suppresses effects whose causal op did not commit. An empty
// dependency list is reserved for effects independent of every planned op.
func committedPlan(plan TickPlan, receipts []store.Receipt) TickPlan {
	accepted := map[string]bool{}
	for i, r := range receipts {
		if i < len(plan.Ops) && r.Accepted {
			accepted[plan.Ops[i].ID] = true
		}
	}
	out := TickPlan{}
	for _, edge := range plan.Edges {
		if hasAll(accepted, edge.After) {
			out.Edges = append(out.Edges, edge)
		}
	}
	for _, answer := range plan.Answers {
		if hasAll(accepted, answer.After) {
			out.Answers = append(out.Answers, answer)
		}
	}
	return out
}
func hasAll(accepted map[string]bool, ids []string) bool {
	for _, id := range ids {
		if !accepted[id] {
			return false
		}
	}
	return true
}

// mergeInbox retains arrival order and avoids repeating a stable result ID.
func mergeInbox(groups ...[]EdgeResult) []EdgeResult {
	seen := map[string]bool{}
	var out []EdgeResult
	for _, group := range groups {
		for _, result := range group {
			if result.ID != "" && seen[result.ID] {
				continue
			}
			if result.ID != "" {
				seen[result.ID] = true
			}
			out = append(out, result)
		}
	}
	return out
}
