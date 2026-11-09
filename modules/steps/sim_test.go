package steps

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

// sim drives ticks against a world the way layer 6 does: fold, plan, apply
// the ops in order, dispatch the edges whose op was accepted, put a refused
// op's items back in place, carry the rest. It is the test's run loop.
type sim struct {
	*world
	ask      Asker
	inbox    []InboxItem
	inflight []EdgeRequest
	refused  []RefusedOp
	arrived  int
	sent     set
	last     Plan
	accepted []store.Op
	rejected []store.Receipt
}

func newSim(t *testing.T) *sim {
	return &sim{world: newWorld(t), ask: benchAsk{}, sent: set{}}
}

// now is the tick's clock: a second after the last op.
func (s *sim) inputs() Inputs {
	in := s.world.inputs()
	in.Inbox = slices.Clone(s.inbox)
	in.Inflight = slices.Clone(s.inflight)
	in.Refused = slices.Clone(s.refused)
	return in
}

// tick folds, plans one tick and applies it; the receipts are kept.
func (s *sim) tick() Plan {
	s.t.Helper()
	s.fold()
	return s.planTick()
}

// planTick plans on the view as folded (stale when moves came after the
// fold) and applies the plan: the model's Tick.
func (s *sim) planTick() Plan {
	s.t.Helper()
	in := s.inputs()
	plan := Tick(s.v, in, s.ask)
	s.last = plan
	s.refused = nil
	s.accepted, s.rejected = nil, nil
	back := set{}
	okOps := set{}
	for _, op := range plan.Ops {
		s.n++
		op.At = s.now.Add(time.Second)
		s.now = op.At
		rc, err := s.f.Apply(context.Background(), op)
		require.NoError(s.t, err, "op %s: %+v", op.ID, op)
		if rc.Accepted {
			okOps[op.ID] = struct{}{}
			s.accepted = append(s.accepted, op)
			continue
		}
		s.rejected = append(s.rejected, rc)
		gs := OpGroups(op)
		s.refused = append(s.refused, RefusedOp{ID: op.ID, Reason: rc.Reason, Groups: gs})
		for _, g := range gs {
			if g.Item != "" {
				back[g.Item] = struct{}{}
			}
		}
	}
	for _, e := range plan.Edges {
		if _, ok := okOps[e.After]; e.After == "" || ok {
			// a request asked again (a land judgement's retry: the same id)
			// is a new dispatch, not yet answered
			s.inflight = append(s.inflight, e)
			delete(s.sent, e.ID)
		}
	}
	var next []InboxItem
	consumed := set{}
	for _, it := range in.Inbox {
		_, b := back[it.ID]
		if b || slices.ContainsFunc(plan.Carry, func(c InboxItem) bool { return c.ID == it.ID }) {
			next = append(next, it)
		} else if it.Request != "" {
			consumed[it.Request] = struct{}{}
		}
	}
	s.inbox = next
	// A request leaves flight when a tick consumes a result of it (the
	// model's inflight'), the result's copies included.
	s.inflight = slices.DeleteFunc(s.inflight, func(r EdgeRequest) bool { _, c := consumed[r.ID]; return c })
	return plan
}

// arrive puts an edge result in the inbox and marks the request answered
// (it stays in flight until a tick consumes the result).
func (s *sim) arrive(it InboxItem) {
	s.arrived++
	it.ID = fmt.Sprintf("r%d", s.arrived)
	s.inbox = append(s.inbox, it)
	if it.Request != "" {
		s.sent[it.Request] = struct{}{}
	}
}

// settle is kept for the tests that call it: planTick drops a request from
// flight when a tick consumes its result.
func (s *sim) settle() {}

// run ticks until a tick plans nothing and the inbox is empty, at most n
// ticks.
func (s *sim) run(n int) {
	s.t.Helper()
	for i := 0; i < n; i++ {
		p := s.tick()
		s.settle()
		if len(p.Ops) == 0 && len(p.Edges) == 0 && len(s.inbox) == 0 {
			return
		}
	}
}

// take and report act as a row would, as their own ops (LAYER-6): take is
// the acknowledgement of a consumer already in the row's hand.
func (s *sim) take(c string) {
	s.t.Helper()
	require.Equal(s.t, "working", s.card(c).State, "%s is in its row's hand from the deal", c)
	r := s.ack(c)
	require.True(s.t, r.Accepted, "take %s refused: %s", c, r.Reason)
}

func (s *sim) land(req EdgeRequest) {
	s.landWith(req, InboxItem{Kind: "land", Request: req.ID, Outcome: "landed", Landed: req.Heads, Main: true})
}

func (s *sim) landWith(req EdgeRequest, it InboxItem) {
	s.arrive(it)
}

// inflightOf is the one request of kind for stream.
func (s *sim) request(kind, stream string) EdgeRequest {
	s.t.Helper()
	i := slices.IndexFunc(s.inflight, func(r EdgeRequest) bool { return r.Kind == kind && r.Stream == stream })
	require.GreaterOrEqual(s.t, i, 0, "no %s request for %s in flight: %+v", kind, stream, s.inflight)
	return s.inflight[i]
}

func (s *sim) state(id string) string { return s.card(id).State }

// tickOps is the last plan's ops of groups (the judgement op aside).
func (s *sim) tickOps() []store.Op { return tickOnly(s.last.Ops) }

func tickOnly(ops []store.Op) []store.Op {
	var out []store.Op
	for _, op := range ops {
		if op.Kind == "tick" {
			out = append(out, op)
		}
	}
	return out
}

func (s *sim) requireState(id, state string) {
	s.t.Helper()
	require.Equal(s.t, state, s.state(id), "card %s", id)
}

func (s *sim) openJudgements(kind string) []string {
	var out []string
	for id, j := range s.snap().Judgements {
		if j.Kind == kind && j.State == "open" {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}
