package verbs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
	write := func(c policy.Caller) (policy.Result, error) {
		return policy.Set(ctx, e.Store, ro, e.Policy, c, in.arg(0), reason)
	}
	if in.verb == "policy activate" {
		v, f := version(in.arg(0))
		if f != nil {
			return "", f
		}
		write = func(c policy.Caller) (policy.Result, error) {
			return policy.Activate(ctx, e.Store, ro, e.Policy, c, v, reason)
		}
	}
	// activate, and a set that activates, answer the open policy judgements
	// they read, so they are fenced on every judgement (policy.fenced): one
	// the run opened or closed between (a red, a down) is not their own.
	// Refused by the store with a write between, they read again and run
	// again, up to fenceTries in all, as fenced does, while their own record
	// is as first read: the epoch, the active policy, the versions stored
	// and the caller's hook. A change to it is the refusal, named.
	was := snap
	stored, err := policy.Stored(ctx, e.Store)
	if err != nil {
		return "", err
	}
	var r policy.Result
	for try := 1; ; try++ {
		c := policy.Caller{Actor: in.actor, Coordinator: true, OpID: e.newOp(strings.ReplaceAll(in.verb, " ", "-"), in.actor, snap, in.args).ID, At: e.Now()}
		r, err = write(c)
		var ref *policy.Refused
		if !errors.As(err, &ref) || !ref.Between() {
			break
		}
		if snap, err = e.snapshot(ctx); err != nil {
			return "", err
		}
		now, serr := policy.Stored(ctx, e.Store)
		if serr != nil {
			return "", serr
		}
		if what := cmp.Or(epochChange(was, snap), policyChange(was, snap, stored, now), hookChange(in.actor)(was, snap)); what != "" {
			ref.What += "; " + what
			err = ref
			break
		}
		if try == fenceTries {
			ref.What += fmt.Sprintf(" (refused %d times, each by a write to records this verb did not read: the store moves faster than it can plan)", try)
			err = ref
			break
		}
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

// policyChange is a policy verb's own record changed: the active version
// on now is not was's, or versions were stored past the stored the verb
// first read.
func policyChange(was, now store.Snapshot, stored, nowStored int) string {
	switch {
	case was.Policy != now.Policy:
		return fmt.Sprintf("the active policy changed since this verb read it (v%d, now v%d)", was.Policy, now.Policy)
	case nowStored != stored:
		return fmt.Sprintf("policy v%d was stored since this verb read it", nowStored)
	}
	return ""
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
	if in.arg(0) == "default" {
		// the built-in as a file to set; its next line is a comment, so the
		// whole output is that file (policy.FirstPolicyFix redirects it)
		return strings.TrimRight(policy.DefaultBody(), "\n") + "\n# next: nova-sprint policy set <file> --reason \"<why>\"\n", nil
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
