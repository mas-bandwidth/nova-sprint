package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Briefs parses and verifies every field and gate required by CARD-CONTRACT.
// It supplies a refusal with a named repair on any failed contract check.
type Briefs interface {
	Parse(context.Context, string, store.Stream) (card.Add, error)
}

type addInput struct {
	stream, actor, priority, sentinel string
	files, needs, after               []string
	dry                               bool
}

func parseAdd(args []string, actor string) (addInput, bool) {
	in := addInput{actor: actor}
	for i := 0; i < len(args); i++ {
		if args[i] == "--dry-run" {
			in.dry = true
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return in, false
		}
		v := args[i+1]
		switch args[i] {
		case "--stream":
			in.stream = v
		case "--brief-file":
			in.files = append(in.files, v)
		case "--as":
			in.actor = v
		case "--priority":
			in.priority = v
		case "--needs":
			in.needs = strings.Split(v, ",")
		case "--after":
			in.after = strings.Split(v, ",")
		case "--sentinel":
			in.sentinel = v
		default:
			return in, false
		}
		i++
	}
	return in, in.actor != "" && in.stream != "" && (len(in.files) > 0 || in.sentinel != "")
}
func (e *Engine) add(ctx context.Context, args []string) Result {
	in, ok := parseAdd(args, e.Actor)
	if !ok {
		return refused("add", "invalid arguments", "nova-sprint help add")
	}
	if in.sentinel != "" && len(in.files) > 0 {
		return refused("add", "--sentinel cannot be mixed with --brief-file", "choose one form")
	}
	if in.sentinel != "" && len(in.needs) == 0 {
		return refused("add", "a sentinel needs at least one card", "nova-sprint add --stream "+in.stream+" --sentinel "+in.sentinel+" --needs <cards>")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil || (len(in.files) > 0 && e.Briefs == nil) {
		return unavailable("add")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure("add", err)
	}
	if !hooked {
		return refused("add", in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("add", err)
	}
	stream, ok := snap.Streams[in.stream]
	if !ok {
		return refused("add", "unknown stream "+in.stream, "nova-sprint stream add "+in.stream+" --repo <owner/name>")
	}
	adds := make([]card.Add, 0, len(in.files)+1)
	if in.sentinel != "" {
		adds = append(adds, card.Add{ID: in.sentinel, Kind: "sentinel", Stream: in.stream, Tier: "flash", Priority: in.priority, Needs: in.needs, After: in.after})
	}
	for _, file := range in.files {
		a, err := e.Briefs.Parse(ctx, file, stream)
		if err != nil {
			return operationalFailure("add", err)
		}
		if a.ID == "group" {
			return refused("add", "card group is reserved for the stream group branch", "choose another card id")
		}
		a.Stream = in.stream
		if in.priority != "" {
			a.Priority = in.priority
		}
		a.Needs = append(a.Needs, in.needs...)
		a.After = append(a.After, in.after...)
		adds = append(adds, a)
	}
	return e.applyAdds(ctx, st, snap, in, adds)
}

func (e *Engine) applyAdds(ctx context.Context, st ApplyStore, snap store.Snapshot, in addInput, adds []card.Add) Result {
	plans, why := planAdds(snap, adds)
	if why != "" {
		return refused("add", why, "nova-sprint help add")
	}
	if in.dry {
		return Result{fmt.Sprintf("ADD DRY RUN cards=%d\nnext: nova-sprint add --stream %s --brief-file <file>\n", len(adds), in.stream), 0}
	}
	plan, mergeRefusal := card.Merge(plans...)
	if mergeRefusal != nil {
		return refused("add", mergeRefusal.Reason, "nova-sprint help add")
	}
	plan.Expect = append(plan.Expect, store.AtSeq{Seq: snap.Seq})
	key, _ := json.Marshal(adds)
	id := consumerOpID("add:"+in.actor, snap.Epoch, string(key), int(snap.Seq))
	op := store.Op{ID: id, Epoch: snap.Epoch, Actor: in.actor, Kind: "add", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("add", err)
	}
	if !rec.Accepted {
		return refused("add", rec.Reason, "nova-sprint help add")
	}
	var out strings.Builder
	for _, a := range adds {
		fmt.Fprintf(&out, "ADDED %s stream=%s tier=%s priority=%s needs=%s\n", a.ID, a.Stream, a.Tier, defaultPriority(a.Priority), formatNeeds(a.Needs))
	}
	next := "nothing; wait for the bus"
	up := false
	for _, r := range snap.Rows {
		if r.State == "up" {
			up = true
			break
		}
	}
	if !up {
		next = "nova-sprint up"
	}
	fmt.Fprintln(&out, "next: "+next)
	return Result{out.String(), 0}
}
func defaultPriority(s string) string {
	if s == "" {
		return "normal"
	}
	return s
}
func formatNeeds(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, ",")
}

func planAdds(s store.Snapshot, adds []card.Add) ([]card.Plan, string) {
	existing := map[string]card.Card{}
	for id, c := range s.Cards {
		existing[id] = card.FromStore(c)
	}
	pending := map[string]card.Add{}
	for _, a := range adds {
		if _, ok := existing[a.ID]; ok {
			return nil, "card " + a.ID + " exists"
		}
		if _, ok := pending[a.ID]; ok {
			return nil, "card " + a.ID + " is named twice"
		}
		pending[a.ID] = a

	}
	order, why := addOrder(pending)
	if why != "" {
		return nil, why
	}
	plans := make([]card.Plan, 0, len(adds))
	for _, id := range order {
		a := pending[id]
		plan, r := card.MoveAdd(a, existing)
		if r != nil {
			return nil, r.Reason
		}
		plans = append(plans, plan)
		existing[id] = card.Card{ID: a.ID, Kind: a.Kind, Stream: a.Stream, Needs: a.Needs, After: a.After}
	}
	return plans, ""
}
func addOrder(pending map[string]card.Add) ([]string, string) {
	state := map[string]int{}
	order := []string{}
	stack := []string{}
	var visit func(string) string
	visit = func(id string) string {
		if state[id] == 2 {
			return ""
		}
		if state[id] == 1 {
			return "cycle through " + strings.Join(append(stack, id), " -> ")
		}
		state[id] = 1
		stack = append(stack, id)
		a := pending[id]
		for _, dep := range append(append([]string{}, a.Needs...), a.After...) {
			if _, ok := pending[dep]; ok {
				if why := visit(dep); why != "" {
					return why
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		order = append(order, id)
		return ""
	}
	names := make([]string, 0, len(pending))
	for id := range pending {
		names = append(names, id)
	}
	slices.Sort(names)
	for _, id := range names {
		if why := visit(id); why != "" {
			return nil, why
		}
	}
	return order, ""
}
