package verbs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runPolicy is the policy verbs: layer 5's logic (package policy) with the
// caller, the roster and the op id this layer owns.
func runPolicy(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	if e.Policy == nil {
		return "", broken("the policy engine is not configured", "nova-sprint check")
	}
	switch in.verb {
	case "policy show":
		return policyShow(ctx, e, in)
	case "policy diff":
		out, err := policy.Diff(ctx, e.Store, in.arg(0), in.arg(1))
		return done("nova-sprint policy check "+in.arg(0), out), err
	}
	lim, _, err := e.rules(ctx, snap)
	if err != nil {
		return "", err
	}
	ro, err := e.roster(ctx, lim, snap)
	if err != nil {
		return "", err
	}
	if in.verb == "policy check" {
		return policyCheck(ctx, e, ro, in.arg(0))
	}
	return policyWrite(ctx, e, in, snap, ro)
}

// policyWrite is set and activate, by the coordinator, with a reason.
func policyWrite(ctx context.Context, e *Engine, in *input, snap store.Snapshot, ro roster.Roster) (string, error) {
	reason := in.str("reason")
	c := policy.Caller{Actor: in.actor, Coordinator: true, OpID: e.newOp(strings.ReplaceAll(in.verb, " ", "-"), in.actor, snap, in.args).ID, At: e.Now()}
	var r policy.Result
	var err error
	if in.verb == "policy set" {
		r, err = policy.Set(ctx, e.Store, ro, e.Policy, c, in.arg(0), reason)
	} else {
		v, f := version(in.arg(0))
		if f != nil {
			return "", f
		}
		r, err = policy.Activate(ctx, e.Store, ro, e.Policy, c, v, reason)
	}
	if err != nil {
		return "", err
	}
	word := map[string]string{"policy set": "STORED", "policy activate": "ACTIVE"}[in.verb]
	if r.Activated {
		word = "STORED AND ACTIVE" // the first set: none was active (defect 53)
	}
	return done(r.Next, fmt.Sprintf("POLICY %s v%d changes=%d", word, r.Version, r.Changes)), nil
}

// policyCheck is the report; a report with refusals is a refusal, printed
// after what it found.
func policyCheck(ctx context.Context, e *Engine, ro roster.Roster, target string) (string, error) {
	rep, err := policy.Check(ctx, e.Store, ro, e.Policy, target)
	if err != nil {
		return "", err
	}
	if rep.Code() != 0 {
		f := refuse(fmt.Sprintf("%s has %d refusals", rep.Label, len(rep.Refusals)), "fix them and run nova-sprint policy check "+target+" again")
		f.Detail = strings.TrimRight(rep.Text(), "\n") + "\n"
		return "", f
	}
	return done(policy.CatalogFix, rep.Text()), nil
}

func policyShow(ctx context.Context, e *Engine, in *input) (string, error) {
	if in.has("list") {
		out, err := policy.List(ctx, e.Store)
		return done("nova-sprint policy show <version>", out), err
	}
	v := 0
	if in.arg(0) != "" {
		var f *Fail
		if v, f = version(in.arg(0)); f != nil {
			return "", f
		}
	}
	out, err := policy.Show(ctx, e.Store, v)
	return done("nova-sprint policy show --list", out), err
}

// version is 7 or v7.
func version(s string) (int, *Fail) {
	v, err := strconv.Atoi(strings.TrimPrefix(s, "v"))
	if err != nil || v < 1 {
		return 0, misuse("policy", "a version is 7 or v7, as nova-sprint policy show --list prints it ("+badNumber(s, err)+")")
	}
	return v, nil
}
