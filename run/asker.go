package run

import (
	"context"
	"maps"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// asker is layer 5 as layer 3 asks it (SEAMS section 4): one call per tick
// on the tick's snapshot, roster and versions. It turns steps' facts per
// subject into policy's ids per subject, and policy's answers back into
// steps' shape; policy computes the facts itself from the same snapshot and
// roster, so the two never disagree about a card or a row. A version that
// fails is in the failures, whose judgements the tick applies.
type asker struct {
	ctx      context.Context
	policy   Asking
	vs       policy.Versions
	snap     store.Snapshot
	ro       roster.Roster
	failures []policy.Failure
	answers  policy.Answers
	asked    bool
}

func (t *Ticker) asker(ctx context.Context, ro roster.Roster) *asker {
	vs := policy.Versions{Seq: t.snap.Seq}
	if v, ok := t.policyVersion(ctx, t.snap.Policy); ok {
		vs.Active = &v
	}
	if v, ok := t.policyVersion(ctx, t.prev); ok && t.prev != t.snap.Policy {
		vs.Prev = &v
	}
	return &asker{ctx: ctx, policy: t.policy, vs: vs, snap: t.snap, ro: ro}
}

func (t *Ticker) policyVersion(ctx context.Context, v int) (store.PolicyVersion, bool) {
	if v == 0 {
		return store.PolicyVersion{}, false
	}
	pv, err := t.store.Policy(ctx, v)
	return pv, err == nil
}

// Ask is steps.Asker.
func (a *asker) Ask(q steps.Questions) steps.Answers {
	pq := policy.Questions{}
	for _, c := range q.Order {
		pq.Order = append(pq.Order, c.ID)
	}
	for _, r := range q.Rows {
		pq.Rows = append(pq.Rows, policy.RowsQuestion{Card: r.Card.ID, Kind: r.Kind})
	}
	for _, c := range q.Reads {
		pq.Reads = append(pq.Reads, c.ID)
	}
	for _, v := range q.Verdict {
		pq.Verdict = append(pq.Verdict, v.Card.ID)
	}
	for _, s := range q.Landing {
		pq.Landing = append(pq.Landing, policy.LandingQuestion{Stream: s.Name, Rung: policy.RungFlowing})
	}
	a.answers, a.failures = a.policy.AnswerContext(a.ctx, a.vs, a.snap, a.ro, pq)
	a.asked = a.answers.Version >= 0
	return stepsAnswers(a.answers, a.failures)
}

// stepsAnswers is policy's answers in steps' shape. Version -1 (no version
// answered) is version 0 to steps: every default.
func stepsAnswers(p policy.Answers, fs []policy.Failure) steps.Answers {
	if p.Version < 0 {
		return steps.Answers{Failed: map[string]string{"*": "no policy version answered"}}
	}
	a := steps.Answers{Version: p.Version, Order: map[string][]any{}, Rows: map[string][]string{},
		Reads: maps.Clone(p.Reads), Verdict: maps.Clone(p.Verdict), Width: map[string]steps.WidthAnswer{},
		Landing: map[string]steps.LandingAnswer{}, Failed: map[string]string{}}
	for id, t := range p.Order {
		a.Order[id] = orderTuple(t)
	}
	for q, rows := range p.Rows {
		a.Rows[q.Card] = rows
	}
	for row, w := range p.Width {
		a.Width[row] = steps.WidthAnswer{Work: w.Work, Ahead: w.Ahead}
	}
	for s, l := range p.Landing {
		a.Landing[s] = steps.LandingAnswer{BackedUp: l.BackedUp, Flowing: l.Flowing, CIWait: l.CIWait}
	}
	l, d := p.Limits, steps.Engine.Limits
	a.Limits = &steps.Limits{Bounces: l.Bounces, Faults: l.Faults, SameFinding: l.SameFinding, ReportsPerTick: l.ReportsPerTick,
		RouteRestWindow: l.RouteRestWindow, RouteRestAfter: l.RouteRestAfter, Budget: d.Budget, Pack: d.Pack}
	tm := p.Timeouts
	a.Timeouts = &steps.Timeouts{RowSilentGone: tm.RowSilentGone, RowSilentWithdraw: tm.RowSilentWithdraw, LandingStall: tm.LandingStall,
		Judgement: tm.Judgement, RouteRestFor: tm.RouteRestFor, BaseRegate: tm.BaseRegate}
	for _, f := range fs {
		if f.Question != "" {
			a.Failed[f.Question] = f.Msg
		}
	}
	return a
}

// orderTuple is policy's tuple (int64 or string) as steps takes it (int or
// string).
func orderTuple(t []any) []any {
	out := make([]any, len(t))
	for i, x := range t {
		if n, ok := x.(int64); ok {
			out[i] = int(n)
		} else {
			out[i] = x
		}
	}
	return out
}
