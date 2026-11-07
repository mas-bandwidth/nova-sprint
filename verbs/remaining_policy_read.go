package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func policyShow(s store.Snapshot, records map[int]policyRecord, in policyInput) Result {
	if in.list {
		var b strings.Builder
		ids := policyVersions(records)
		for i := len(ids) - 1; i >= 0; i-- {
			p := records[ids[i]].Version
			mark := ""
			if p.Version == s.Policy {
				mark = " active"
			}
			fmt.Fprintf(&b, "v%d kind=%s name=%s%s\n", p.Version, p.Kind, p.Name, mark)
		}
		b.WriteString("next: nova-sprint next\n")
		return Result{b.String(), 0}
	}
	v := s.Policy
	if len(in.args) > 0 {
		n, ok := policyNumber(in.args[0])
		if !ok {
			return refused("policy show", "unknown version "+in.args[0], "nova-sprint policy show --list")
		}
		v = n
	}
	p, ok := records[v]
	if !ok {
		return refused("policy show", fmt.Sprintf("unknown version v%d", v), "nova-sprint policy show --list")
	}
	return Result{fmt.Sprintf("POLICY v%d kind=%s name=%s actor=%s at=%s reason=%s\n%s\nnext: nova-sprint next\n", v, p.Version.Kind, p.Version.Name, p.Version.Actor, p.Version.At.Format("2006-01-02T15:04:05Z07:00"), p.Reason, p.Version.Body), 0}
}
func (e *Engine) policyBody(ctx context.Context, s store.Snapshot, records map[int]policyRecord, ref string, fileOK bool) (string, error) {
	if ref == "default" {
		if e.PolicyEvaluator == nil {
			return "", fmt.Errorf("evaluator unavailable")
		}
		return e.PolicyEvaluator.Default(ctx)
	}
	if n, ok := policyNumber(ref); ok {
		p, found := records[n]
		if !found {
			return "", &OperationError{Problem: "unknown version " + ref, Fix: "nova-sprint policy show --list", Exit: 1}
		}
		if p.Version.Kind != "script" {
			return "", &OperationError{Problem: fmt.Sprintf("v%d is data, not script", n), Fix: "nova-sprint policy show --list", Exit: 1}
		}
		return p.Version.Body, nil
	}
	if fileOK {
		return e.readPolicy(ctx, ref)
	}
	return "", &OperationError{Problem: "unknown version " + ref, Fix: "nova-sprint policy show --list", Exit: 1}
}
func (e *Engine) policyDiff(ctx context.Context, s store.Snapshot, records map[int]policyRecord, in policyInput) Result {
	a, err := e.policyBody(ctx, s, records, in.args[0], false)
	if err != nil {
		return operationalFailure("policy diff", err)
	}
	other := fmt.Sprintf("v%d", s.Policy)
	if len(in.args) == 2 {
		other = in.args[1]
	}
	b, err := e.policyBody(ctx, s, records, other, false)
	if err != nil {
		return operationalFailure("policy diff", err)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", in.args[0], other)
	if a != b {
		for _, line := range strings.Split(a, "\n") {
			fmt.Fprintln(&out, "-"+line)
		}
		for _, line := range strings.Split(b, "\n") {
			fmt.Fprintln(&out, "+"+line)
		}
	}
	out.WriteString("next: nova-sprint next\n")
	return Result{out.String(), 0}
}
func (e *Engine) policyCheck(ctx context.Context, s store.Snapshot, records map[int]policyRecord, in policyInput) Result {
	body, err := e.policyBody(ctx, s, records, in.args[0], true)
	if err != nil {
		return operationalFailure("policy check", err)
	}
	check, err := e.evaluatePolicy(ctx, s, body)
	if err != nil {
		return operationalFailure("policy check", err)
	}
	var out strings.Builder
	for _, v := range check.Differences {
		fmt.Fprintln(&out, "DIFF "+v)
	}
	for _, v := range check.Refusals {
		fmt.Fprintln(&out, "REFUSED "+v)
	}
	out.WriteString("next: nova-sprint next\n")
	exit := 0
	if len(check.Refusals) > 0 {
		exit = 1
	}
	return Result{out.String(), exit}
}
