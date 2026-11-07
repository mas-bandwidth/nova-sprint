package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func (e *Engine) policySet(ctx context.Context, st store.Store, s store.Snapshot, versions map[int]policyRecord, in policyInput) Result {
	body, err := e.readPolicy(ctx, in.args[0])
	if err != nil {
		return operationalFailure("policy set", err)
	}
	ids := policyVersions(versions)
	for _, v := range ids {
		p := versions[v].Version
		if p.Body == body {
			fix := fmt.Sprintf("nova-sprint policy activate %d --reason <why>", v)
			if p.Kind != "script" {
				fix = fmt.Sprintf("nova-sprint policy show %d", v)
			}
			return refused("policy set", fmt.Sprintf("v%d has this body", v), fix)
		}
	}
	name := policyBase(in.args[0])
	if !validPolicyName(name) {
		return refused("policy set", "invalid policy file name", "provide a named policy file")
	}
	checked, err := e.evaluatePolicy(ctx, s, body)
	if err != nil {
		return operationalFailure("policy set", err)
	}
	if len(checked.Refusals) > 0 {
		return refused("policy set", strings.Join(checked.Refusals, "; "), "nova-sprint policy check "+in.args[0])
	}
	plan := card.Plan{Writes: []store.Write{store.PutPolicy{Kind: "script", Name: name, Body: body}}}
	r := policyApply(ctx, st, s, e, in, plan)
	if r.Exit != 0 {
		return r
	}
	next := 1
	if len(ids) > 0 {
		next = ids[len(ids)-1] + 1
	}
	return Result{fmt.Sprintf("POLICY SET v%d\nnext: nova-sprint policy check v%d\n", next, next), 0}
}
func (e *Engine) policyActivate(ctx context.Context, st store.Store, s store.Snapshot, versions map[int]policyRecord, in policyInput) Result {
	version, ok := policyNumber(in.args[0])
	p, exists := versions[version]
	if !ok || !exists {
		return refused("policy activate", "unknown version "+in.args[0], "nova-sprint policy show --list")
	}
	if p.Version.Kind != "script" {
		return refused("policy activate", fmt.Sprintf("v%d is data, not script", version), "nova-sprint policy show --list")
	}
	if version == s.Policy {
		return refused("policy activate", fmt.Sprintf("v%d is already active", version), "nova-sprint policy show")
	}
	checked, err := e.evaluatePolicy(ctx, s, p.Version.Body)
	if err != nil {
		return operationalFailure("policy activate", err)
	}
	if len(checked.Refusals) > 0 {
		return refused("policy activate", strings.Join(checked.Refusals, "; "), fmt.Sprintf("nova-sprint policy check v%d", version))
	}
	if e.PolicyActivator == nil {
		return unavailable("policy activate")
	}
	plan, err := e.PolicyActivator.PlanActivation(ctx, s, version, in.actor, in.reason)
	if err != nil {
		return operationalFailure("policy activate", err)
	}
	if !policyClosureComplete(s, plan) {
		return unavailable("policy activate")
	}
	for _, w := range plan.Writes {
		if _, ok := w.(store.SetActivePolicy); ok {
			return unavailable("policy activate")
		}
	}
	plan.Writes = append(plan.Writes, store.SetActivePolicy{Version: version})
	r := policyApply(ctx, st, s, e, in, plan)
	if r.Exit != 0 {
		return r
	}
	return Result{fmt.Sprintf("POLICY ACTIVE v%d answers-changed=%d\nnext: nova-sprint next\n", version, len(checked.Differences)), 0}
}
func policyVersions(records map[int]policyRecord) []int {
	ids := make([]int, 0, len(records))
	for v := range records {
		ids = append(ids, v)
	}
	slices.Sort(ids)
	return ids
}

func policyClosureComplete(s store.Snapshot, plan card.Plan) bool {
	group := fmt.Sprintf("policy:v%d", s.Policy)
	for _, j := range s.Judgements {
		if j.Kind != "policy" || j.Group != group || j.State != "open" {
			continue
		}
		closed, fenced := false, false
		for _, w := range plan.Writes {
			if v, ok := w.(store.AnswerJudgement); ok && v.ID == j.ID {
				closed = true
			}
		}
		for _, x := range plan.Expect {
			if v, ok := x.(store.JudgementOpen); ok && v.ID == j.ID {
				fenced = true
			}
		}
		if !closed || !fenced {
			return false
		}
	}
	return true
}

func validPolicyName(s string) bool {
	return s != "" && len(s) <= 128 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
