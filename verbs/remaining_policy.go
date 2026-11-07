package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type PolicyEvaluator interface {
	Check(context.Context, store.Snapshot, string) (PolicyCheck, error)
	Default(context.Context) (string, error)
}

// PolicyCheck contains deliberately safe, user-facing diagnostics; it must not contain secrets or raw provider output.
type PolicyCheck struct {
	Differences []string
	Refusals    []string
}

// PolicyFiles must stop reading once limit+1 bytes are available.
type PolicyFiles interface {
	ReadPolicy(context.Context, string, int64) ([]byte, error)
}
type PolicyActivator interface {
	PlanActivation(context.Context, store.Snapshot, int, string, string) (card.Plan, error)
}

const policyFileLimit int64 = 64 * 1024

type policyInput struct {
	verb, actor, reason string
	args                []string
	list                bool
}

func parsePolicy(args []string, actor string) (policyInput, bool) {
	in := policyInput{actor: actor}
	if len(args) == 0 {
		return in, false
	}
	in.verb = args[0]
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		k := args[i]
		switch k {
		case "--list":
			if seen[k] {
				return in, false
			}
			in.list = true
			seen[k] = true
		case "--as", "--reason":
			if seen[k] || i+1 >= len(args) || args[i+1] == "" {
				return in, false
			}
			seen[k] = true
			i++
			if k == "--as" {
				in.actor = args[i]
			} else {
				in.reason = args[i]
			}
		default:
			if k == "" || strings.HasPrefix(k, "-") {
				return in, false
			}
			in.args = append(in.args, k)
		}
	}
	if in.actor == "" {
		return in, false
	}
	switch in.verb {
	case "set", "activate":
		return in, len(in.args) == 1 && in.reason != "" && !in.list
	case "show":
		return in, len(in.args) <= 1 && in.reason == "" && (!in.list || len(in.args) == 0)
	case "diff":
		return in, len(in.args) >= 1 && len(in.args) <= 2 && in.reason == "" && !in.list
	case "check":
		return in, len(in.args) == 1 && in.reason == "" && !in.list
	}
	return in, false
}
func (e *Engine) policy(ctx context.Context, args []string) Result {
	in, ok := parsePolicy(args, e.Actor)
	if !ok {
		return refused("policy", "invalid arguments", "nova-sprint help policy")
	}
	verb := "policy " + in.verb
	st, ok := e.Store.(store.Store)
	if !ok || e.Env == nil {
		return unavailable(verb)
	}

	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure(verb, err)
	}
	if !hooked {
		return refused(verb, in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure(verb, err)
	}
	if snap.Policy == 0 {
		return refused(verb, "sprint is not set up", "nova-sprint up")
	}
	versions, err := policyHistory(ctx, st, snap)
	if err != nil {
		return operationalFailure(verb, err)
	}
	switch in.verb {
	case "set":
		return e.policySet(ctx, st, snap, versions, in)
	case "activate":
		return e.policyActivate(ctx, st, snap, versions, in)
	case "show":
		return policyShow(snap, versions, in)
	case "diff":
		return e.policyDiff(ctx, snap, versions, in)
	case "check":
		return e.policyCheck(ctx, snap, versions, in)
	}
	return unavailable(verb)
}

type policyRecord struct {
	Version store.PolicyVersion
	Reason  string
}

func policyHistory(ctx context.Context, st store.Store, snap store.Snapshot) (map[int]policyRecord, error) {
	result := map[int]policyRecord{}
	cursor := int64(0)
	for cursor < snap.Seq {
		events, err := st.Events(ctx, cursor, store.MaxEvents)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return nil, fmt.Errorf("incomplete history")
		}
		advanced := false
		for _, ev := range events {
			if ev.Seq > snap.Seq {
				break
			}
			if ev.Seq != cursor+1 {
				return nil, fmt.Errorf("policy history has a sequence gap")
			}
			cursor = ev.Seq
			advanced = true
			if !ev.Accepted || ev.After == nil {
				continue
			}
			for v, p := range ev.After.Policies {
				old, exists := result[v]
				if !exists {
					old.Reason = ev.Receipt.Note
				}
				old.Version = p
				result[v] = old
			}
		}
		if !advanced {
			return nil, fmt.Errorf("policy history made no progress")
		}
	}
	return result, nil
}
func policyNumber(s string) (int, bool) {
	s = strings.TrimPrefix(s, "v")
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}
func (e *Engine) readPolicy(ctx context.Context, path string) (string, error) {
	if e.PolicyFiles == nil {
		return "", fmt.Errorf("reader unavailable")
	}
	data, err := e.PolicyFiles.ReadPolicy(ctx, path, policyFileLimit)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > policyFileLimit {
		return "", &OperationError{Problem: "policy file exceeds 64 KiB", Fix: "reduce the file", Exit: 1}
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", &OperationError{Problem: "policy file is not valid text", Fix: "provide UTF-8 without NUL", Exit: 1}
	}
	return string(data), nil
}
func (e *Engine) evaluatePolicy(ctx context.Context, s store.Snapshot, body string) (PolicyCheck, error) {
	if e.PolicyEvaluator == nil {
		return PolicyCheck{}, fmt.Errorf("evaluator unavailable")
	}
	return e.PolicyEvaluator.Check(ctx, s, body)
}
func policyApply(ctx context.Context, st store.Store, s store.Snapshot, e *Engine, in policyInput, plan card.Plan) Result {
	verb := "policy " + in.verb
	if e.Now == nil {
		return unavailable(verb)
	}
	plan.Expect = append(plan.Expect, store.AtSeq{Seq: s.Seq})
	key, _ := json.Marshal([]any{in.verb, in.actor, in.reason, in.args})
	op := store.Op{ID: consumerOpID(verb+":"+in.actor, s.Epoch, string(key), int(s.Seq)), Epoch: s.Epoch, Actor: in.actor, Kind: "policy-" + in.verb, At: e.Now(), Note: in.reason, Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure(verb, err)
	}
	if !rec.Accepted {
		return refused(verb, rec.Reason, "retry nova-sprint "+verb)
	}
	return Result{}
}
func policyBase(path string) string { return filepath.Base(path) }
