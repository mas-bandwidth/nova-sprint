package run

import (
	"context"
	"fmt"
	"maps"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
	stored   int // the newest stored policy version while none is active (noDeal's fix), else 0
}

func (t *Ticker) asker(ctx context.Context, ro roster.Roster) *asker {
	vs := policy.Versions{Seq: t.snap.Seq}
	if v, ok := t.policyVersion(ctx, t.snap.Policy); ok {
		vs.Active = &v
	}
	if v, ok := t.policyVersion(ctx, t.prev); ok && t.prev != t.snap.Policy {
		vs.Prev = &v
	}
	a := &asker{ctx: ctx, policy: t.policy, vs: vs, snap: t.snap, ro: ro}
	if vs.Active == nil {
		a.stored = t.storedPolicy(ctx)
	}
	return a
}

// storedPolicy is the newest stored policy version, 0 when none is stored
// or one cannot be read (the probe policy show --list makes; it runs only
// while no version is active).
func (t *Ticker) storedPolicy(ctx context.Context) int {
	v := 0
	for {
		if _, err := t.store.Policy(ctx, v+1); err != nil {
			return v
		}
		v++
	}
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
	out := stepsAnswers(a.answers, a.failures)
	out.NoDeal = noDeal(a.vs, a.answers, a.failures, a.stored)
	return out
}

// noDeal is the deal's gate (defect 53): why the run deals nothing this
// tick, "" when the active version answered. With no version active the
// built-in's defaults still run the rest of the tick, but nothing is dealt:
// a deal on defaults carried no model and moved nothing (the fleet's idle
// night of 2026-11-07). An active version that failed, so that another
// answered, names its failure. With versions stored and none active (a
// store whose first set did not activate; defect D3, 2026-11-07) the fix is
// to activate the newest stored, not to set another.
func noDeal(vs policy.Versions, p policy.Answers, fs []policy.Failure, stored int) string {
	if vs.Active == nil && stored > 0 {
		return fmt.Sprintf("no policy is active (v%d is stored, none active): nova-sprint policy activate v%d --reason <why>", stored, stored)
	}
	if vs.Active == nil {
		return "no policy is active: nova-sprint policy set <file> --reason <why> (the first set activates; nova-sprint up installs the default)"
	}
	if p.Version == vs.Active.Version {
		return ""
	}
	why := "it did not answer"
	for _, f := range fs {
		if f.Version == vs.Active.Version || f.Version < 0 {
			why = f.Error()
			break
		}
	}
	return fmt.Sprintf("the active policy v%d failed (%s): nova-sprint policy check v%d, then fix it with nova-sprint policy set <file> --reason <why>", vs.Active.Version, why, vs.Active.Version)
}

// stepsAnswers is policy's answers in steps' shape. Version -1 (no version
// answered) is version 0 to steps: every default.
func stepsAnswers(p policy.Answers, fs []policy.Failure) steps.Answers {
	if p.Version < 0 {
		return steps.Answers{Failed: map[string]string{"*": "no policy version answered"}}
	}
	a := steps.Answers{Version: p.Version, Order: map[string][]any{}, Rows: map[string][]string{},
		Reads: maps.Clone(p.Reads), Verdict: maps.Clone(p.Verdict), Width: map[string]steps.WidthAnswer{},
		Landing: map[string]steps.LandingAnswer{}, Failed: map[string]string{}, Models: maps.Clone(p.Catalog.Bench)}
	for id, t := range p.Order {
		a.Order[id] = orderTuple(t)
	}
	for q, rows := range p.Rows {
		a.Rows[q.Card] = rows
	}
	for row, w := range p.Width {
		a.Width[row] = steps.WidthAnswer{Work: w.Work}
	}
	for s, l := range p.Landing {
		a.Landing[s] = steps.LandingAnswer{BackedUp: l.BackedUp, Flowing: l.Flowing, CIWait: l.CIWait}
	}
	l, d := p.Limits, steps.Engine.Limits
	a.Limits = &steps.Limits{Bounces: l.Bounces, Faults: l.Faults, SameFinding: l.SameFinding, ReportsPerTick: l.ReportsPerTick,
		RouteRestWindow: l.RouteRestWindow, RouteRestAfter: l.RouteRestAfter, Budget: d.Budget, Pack: d.Pack}
	tm := p.Timeouts
	a.Timeouts = &steps.Timeouts{RowSilentGone: tm.RowSilentGone, RowSilentWithdraw: tm.RowSilentWithdraw, LandingStall: tm.LandingStall,
		Judgement: tm.Judgement, RouteRestFor: tm.RouteRestFor, ReadyNag: tm.ReadyNag, ReadyTake: tm.ReadyTake, BaseRegate: tm.BaseRegate}
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
