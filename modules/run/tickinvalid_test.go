package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

// faultyTick is a store whose check refuses every op the tick plans, as it
// refused the one judgement op of a planner that wrote a judgement twice
// (invalid), or that is away (fault): the tick ends at its first op. A tick
// op is one fenced on the run not being stopped (apply's store.Running);
// the run's own judgement op and the pusher's cursor go through.
type faultyTick struct {
	*store.Fake
	fault error
}

func (s *faultyTick) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if s.fault != nil && slices.ContainsFunc(op.Expect, func(e store.Expect) bool { _, ok := e.(store.Running); return ok }) {
		return store.Receipt{}, fmt.Errorf("%w: op %s", s.fault, op.ID)
	}
	return s.Fake.Apply(ctx, op)
}

func engineTick(snap store.Snapshot) (all, open []store.Judgement) {
	for _, j := range snap.Judgements {
		if j.Kind == "engine" && j.About == "tick" {
			all = append(all, j)
			if j.State == "open" {
				open = append(open, j)
			}
		}
	}
	return all, open
}

// TestInvalidTickIsJudged: a tick the store's check ends (an op of the plan
// is invalid: a planner defect, the same op every tick) still ends there,
// with its error, and nothing after the op applied; and it says so to the
// coordinator: one engine judgement about the tick, pushed as NEEDS YOU,
// once while it is open, however many ticks end so (seen 2026-11-16: 25,000
// ticks in a row whose only sign was status=error in the run's log). One
// answered while ticks still end so is opened again, as the next episode;
// a tick that fails for another reason (the store away) is not a planner
// defect and opens none; and with the cause gone the run deals again by
// itself.
func TestInvalidTickIsJudged(t *testing.T) {
	t.Parallel()
	f, ro := seeded(t)
	must(t, f, "coord", store.PutRow{Name: "carol", Session: "sess-c", Role: "coordinator"})
	st := &faultyTick{Fake: f}
	tk, err := NewTicker(context.Background(), st, &healthy{}, &answering{}, t0)
	require.NoError(t, err)
	now := t0
	tick := func() error {
		now = now.Add(time.Second)
		_, err := tk.Tick(context.Background(), Observed{Now: now, Present: []string{"alice", "carol"}, Roster: ro, Heads: map[string]string{"s": strings.Repeat("a", 40)}})
		return err
	}

	st.fault = errors.New("store: connection refused")
	require.ErrorContains(t, tick(), "connection refused")
	all, _ := engineTick(tk.Snapshot())
	require.Empty(t, all, "a store that is away is no planner defect")

	st.fault = fmt.Errorf("%w: write store.PutJudgement: a second write to judgement flap.r.4 field state", store.ErrInvalid)
	for range 3 {
		err := tick()
		require.ErrorIs(t, err, store.ErrInvalid, "the tick still ends at the invalid op, with its error")
	}
	snap := tk.Snapshot()
	all, open := engineTick(snap)
	require.Len(t, all, 1, "one judgement however many ticks end so")
	require.Len(t, open, 1)
	require.Equal(t, "engine.tick.1", open[0].ID)
	require.Contains(t, open[0].Question, "a second write to judgement flap.r.4 field state")
	require.Contains(t, open[0].Question, "no op planned after it is applied")
	require.False(t, open[0].Deadline.IsZero())
	require.Equal(t, "waiting", snap.Cards["p1"].State, "nothing of the tick was applied")
	b := &fakeBus{}
	require.Empty(t, tk.Push(context.Background(), b, now))
	var pushed []string
	for _, m := range b.sent {
		if m.to == "carol" && m.subject == "NEEDS YOU" {
			pushed = append(pushed, m.subject)
		}
	}
	require.Len(t, pushed, 1, "the coordinator is pushed once: %+v", b.sent)

	r, err := f.Apply(context.Background(), store.Op{ID: "answer", Epoch: 1, Actor: "carol", Kind: "judge", At: now,
		Writes: []store.Write{store.AnswerJudgement{ID: "engine.tick.1", Answer: "retry", Actor: "carol"}}})
	require.NoError(t, err)
	require.True(t, r.Accepted, r.Reason)
	require.ErrorIs(t, tick(), store.ErrInvalid)
	_, open = engineTick(tk.Snapshot())
	require.Len(t, open, 1, "answered while it still holds: said again")
	require.Equal(t, "engine.tick.2", open[0].ID)

	st.fault = nil
	for range 3 {
		require.NoError(t, tick())
	}
	require.Equal(t, "working", tk.Snapshot().Cards["p1"].State, "with the cause gone the run deals again by itself")
}
