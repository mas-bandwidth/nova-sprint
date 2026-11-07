package run

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// L3Heads supplies the observed base head for each stream.
type L3Heads interface {
	Heads(context.Context, store.Snapshot) (map[string]string, error)
}
type L3Asker interface {
	ForTick(context.Context, TickInput) (steps.Asker, error)
}

// L3Adapter owns the incremental Layer 3 view and dispatched request set.
// Engine calls its methods serially; downstream enqueue must not block.
type L3Adapter struct {
	Clock    Clock
	Heads    L3Heads
	Asker    L3Asker
	Edges    TickEdges
	view     *steps.View
	inflight map[string]steps.EdgeRequest
	seen     map[string]time.Time
	last     steps.Plan
	items    map[string]steps.InboxItem
	planEpoch int
}

func (a *L3Adapter) Init(s store.Snapshot) {
	a.view = steps.Build(s, a.Clock.Now())
	a.inflight = map[string]steps.EdgeRequest{}
	a.seen = map[string]time.Time{}
}
func (a *L3Adapter) Validate() error {
	if a.Clock == nil || a.Heads == nil || a.Asker == nil || a.Edges == nil {
		return fmt.Errorf("run: Layer 3 adapter dependencies are required")
	}
	return nil
}
func (a *L3Adapter) Fold(events []store.Event) {
	for _, event := range events {
		if event.Accepted && event.After != nil && event.After.Epoch != 0 {
			a.inflight = map[string]steps.EdgeRequest{}
			a.seen = map[string]time.Time{}
			a.items = nil
			a.last = steps.Plan{}
			a.planEpoch = 0
		}
	}
	a.view.Fold(events)
}

func (a *L3Adapter) Tick(ctx context.Context, s store.Snapshot, in TickInput) (TickPlan, error) {
	if a.view == nil || a.Clock == nil || a.Heads == nil || a.Asker == nil || a.Edges == nil || a.view.Seq != s.Seq {
		return TickPlan{}, fmt.Errorf("run: Layer 3 adapter dependencies or view unavailable")
	}
	heads, err := a.Heads.Heads(ctx, s)
	if err != nil {
		return TickPlan{}, err
	}
	ask, err := a.Asker.ForTick(ctx, in)
	if err != nil {
		return TickPlan{}, fmt.Errorf("run: policy asker: %w", err)
	}
	if ask == nil {
		return TickPlan{}, fmt.Errorf("run: policy asker unavailable")
	}
	items, err := l3Inbox(in.Inbox, s.Epoch)
	if err != nil {
		return TickPlan{}, err
	}
	for _, name := range in.Presence {
		a.seen[name] = in.At
	}
	input := steps.Inputs{Now: in.At, Inbox: items, Inflight: a.requests(), Heads: heads, Seen: a.seen, Refused: l3Refusals(in.Refused, s.Epoch), Roster: in.Roster}
	a.last = steps.Tick(a.view, input, ask)
	a.planEpoch = s.Epoch
	a.items = map[string]steps.InboxItem{}
	for _, item := range items {
		a.items[item.ID] = item
	}
	return l3Plan(a.last, s.Epoch), nil
}

func l3Inbox(in []EdgeResult, epoch int) ([]steps.InboxItem, error) {
	out := make([]steps.InboxItem, 0, len(in))
	for _, result := range in {
		if result.Epoch != epoch { continue }
		item, ok := result.Payload.(steps.InboxItem)
		if !ok || item.ID == "" || item.ID != result.ID {
			return nil, fmt.Errorf("run: invalid Layer 3 inbox item %q", result.ID)
		}
		out = append(out, item)
	}
	return out, nil
}

func l3Refusals(in []RefusedOp, epoch int) []steps.RefusedOp {
	out := make([]steps.RefusedOp, 0, len(in))
	for _, r := range in {
		if r.Op.Epoch != epoch { continue }
		out = append(out, steps.RefusedOp{ID: r.Op.ID, Reason: r.Receipt.Reason, Groups: steps.OpGroups(r.Op)})
	}
	return out
}

func (a *L3Adapter) requests() []steps.EdgeRequest {
	keys := make([]string, 0, len(a.inflight))
	for key := range a.inflight {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]steps.EdgeRequest, 0, len(keys))
	for _, key := range keys {
		out = append(out, a.inflight[key])
	}
	return out
}

func l3Plan(p steps.Plan, epoch int) TickPlan {
	out := TickPlan{Ops: p.Ops}
	for _, edge := range p.Edges {
		after := []string(nil)
		if edge.After != "" {
			after = []string{edge.After}
		}
		out.Edges = append(out.Edges, EdgeRequest{ID: edge.ID, Kind: edge.Kind, Payload: edge, After: after})
	}
	for _, answer := range p.Answers {
		out.Answers = append(out.Answers, RowAnswer{ID: answer.Result, Row: answer.Row, Text: answer.Text, Refused: answer.Refused, After: answerDependencies(p.Ops, answer.Result)})
	}
	for _, item := range p.Carry {
		out.Carry = append(out.Carry, EdgeResult{ID: item.ID, Kind: item.Kind, Epoch: epoch, Payload: item})
	}
	return out
}

func answerDependencies(ops []store.Op, item string) []string {
	var out []string
	for _, op := range ops {
		for _, group := range steps.OpGroups(op) {
			if group.Item == item && item != "" {
				out = append(out, op.ID)
				break
			}
		}
	}
	return out
}

func (a *L3Adapter) Committed(ctx context.Context, p TickPlan, receipts []store.Receipt) error {
	if a.planEpoch == 0 || a.planEpoch != a.view.Epoch { return nil }
	for _, id := range a.consumed(receipts) {
		if item := a.items[id]; item.Request != "" {
			delete(a.inflight, item.Request)
		}
	}
	if err := a.Edges.Committed(ctx, p, receipts); err != nil {
		return err
	}
	for _, edge := range p.Edges {
		if request, ok := edge.Payload.(steps.EdgeRequest); ok {
			a.inflight[request.ID] = request
		}
	}
	return nil
}

func (a *L3Adapter) consumed(receipts []store.Receipt) []string {
	carried, grouped, accepted := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range a.last.Carry {
		carried[item.ID] = true
	}
	for _, op := range a.last.Ops {
		for _, group := range steps.OpGroups(op) {
			if group.Item != "" {
				grouped[group.Item] = true
			}
		}
	}
	for i, receipt := range receipts {
		if i >= len(a.last.Ops) || !receipt.Accepted {
			continue
		}
		for _, group := range steps.OpGroups(a.last.Ops[i]) {
			if group.Item != "" {
				accepted[group.Item] = true
			}
		}
	}
	out := []string{}
	for id := range a.items {
		if !carried[id] && (!grouped[id] || accepted[id]) {
			out = append(out, id)
		}
	}
	return out
}
