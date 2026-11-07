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

type dropInput struct {
	id, actor, reason, relink string
	waive, dry, expectSet     bool
	expect                    []string
}

func parseDrop(args []string, actor string) (dropInput, bool) {
	in := dropInput{actor: actor}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if seen[arg] {
			return in, false
		}
		switch arg {
		case "--dry-run":
			in.dry = true
			seen[arg] = true
		case "--waive":
			in.waive = true
			seen[arg] = true
		case "--as", "--reason", "--relink", "--expect":
			if i+1 >= len(args) || args[i+1] == "" {
				return in, false
			}
			seen[arg] = true
			i++
			v := args[i]
			switch arg {
			case "--as":
				in.actor = v
			case "--reason":
				in.reason = v
			case "--relink":
				in.relink = v
			case "--expect":
				in.expectSet = true
				if v != "-" {
					in.expect = strings.Split(v, ",")
				}
			}
		default:
			if strings.HasPrefix(arg, "-") || arg == "" || in.id != "" {
				return in, false
			}
			in.id = arg
		}
	}
	slices.Sort(in.expect)
	for i, id := range in.expect {
		if id == "" || i > 0 && id == in.expect[i-1] {
			return in, false
		}
	}
	return in, in.id != "" && in.actor != "" && in.reason != "" && !(in.waive && in.relink != "")
}
func (e *Engine) drop(ctx context.Context, args []string) Result {
	in, ok := parseDrop(args, e.Actor)
	if !ok {
		return refused("drop", "invalid arguments", "nova-sprint help drop")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil {
		return unavailable("drop")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure("drop", err)
	}
	if !hooked {
		return refused("drop", in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("drop", err)
	}
	if _, ok := snap.Cards[in.id]; !ok {
		return refused("drop", "unknown card "+in.id, "nova-sprint show "+in.id)
	}
	deps := dropDependants(snap, in.id)
	touches := append([]string{in.id}, deps...)
	slices.Sort(touches)
	if in.expectSet && !slices.Equal(in.expect, deps) {
		return refused("drop", "dependants changed", "nova-sprint drop "+in.id+" --reason <why> --dry-run")
	}
	plan, _, why, fix := planDrop(snap, in)
	if why != "" {
		return refused("drop", why, fix)
	}
	preview := dropPreview(in, touches)
	if in.dry {
		return dropDryRun(in, deps, preview)
	}
	if e.Preview == nil {
		return unavailable("drop")
	}
	if err = e.Preview(ctx, preview+"\n"); err != nil {
		return operationalFailure("drop", err)
	}
	key, _ := json.Marshal([]any{in.id, in.actor, in.reason, in.waive, in.relink})
	plan.Expect = append(plan.Expect, store.AtSeq{Seq: snap.Seq})
	op := store.Op{ID: consumerOpID("drop:"+in.actor, snap.Epoch, string(key), int(snap.Seq)), Epoch: snap.Epoch, Actor: in.actor, Kind: "drop", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("drop", err)
	}
	if !rec.Accepted {
		return refused("drop", rec.Reason, "retry nova-sprint drop")
	}
	return Result{"DROP applied\nnext: nothing; wait for the bus\n", 0}
}

func dropPreview(in dropInput, touches []string) string {
	preview := fmt.Sprintf("DROP touch=%s withdraw=- dependants-fail=-", strings.Join(touches, ","))
	if in.waive {
		preview += " waive=" + in.id
	}
	if in.relink != "" {
		preview += " relink=" + in.id + "=" + in.relink
	}
	return preview
}
func dropDryRun(in dropInput, deps []string, preview string) Result {
	next := "nova-sprint drop " + in.id + " --reason <why>"
	if in.waive {
		next += " --waive"
	} else if in.relink != "" {
		next += " --relink " + in.relink
	} else if len(deps) > 0 {
		next = "nova-sprint help drop"
	}
	if next != "nova-sprint help drop" {
		next += " --expect " + formatNeeds(deps)
	}
	return Result{preview + " (dry run)\nnext: " + next + "\n", 0}
}

func planDrop(s store.Snapshot, in dropInput) (card.Plan, []string, string, string) {
	p, ok := s.Cards[in.id]
	if !ok {
		return card.Plan{}, nil, "unknown card " + in.id, "nova-sprint show " + in.id
	}
	if p.Kind == "work" || p.Kind == "read" {
		return card.Plan{}, nil, in.id + " is a consumer", "nova-sprint show " + in.id
	}
	deps := dropDependants(s, in.id)
	if why, fix := dropBlocked(s, in.id); why != "" {
		return card.Plan{}, nil, why, fix
	}
	if len(deps) > 0 && !in.waive && in.relink == "" {
		if in.dry {
			return dropDryPlan(s, in, deps)
		}
		return card.Plan{}, nil, "card " + in.id + " has dependants " + strings.Join(deps, ","), "nova-sprint drop " + in.id + " --reason <why> --waive or --relink <other>"
	}
	if in.relink != "" {
		if _, ok := s.Cards[in.relink]; !ok {
			return card.Plan{}, nil, "unknown card " + in.relink, "nova-sprint show " + in.relink
		}
	}
	all := map[string]card.Card{}
	for id, c := range s.Cards {
		all[id] = card.FromStore(c)
	}
	plans := []card.Plan{}
	first, why := dropMove(s, all, in.id, card.Cancel{Reason: in.reason})
	if why != nil {
		return card.Plan{}, nil, why.Reason, "nova-sprint show " + in.id
	}
	plans = append(plans, first)
	for _, id := range deps {
		if why, fix := dropBlocked(s, id); why != "" {
			return card.Plan{}, nil, why, fix
		}
		var event card.Event = card.Waive{Need: in.id, Reason: in.reason, InThisOp: true}
		if in.relink != "" {
			event = card.Relink{Need: in.id, To: in.relink, Reason: in.reason, InThisOp: true}
		}
		plan, why := dropMove(s, all, id, event)
		if why != nil {
			return card.Plan{}, nil, why.Reason, "nova-sprint show " + id
		}
		plans = append(plans, plan)
	}
	merged, why := card.Merge(plans...)
	if why != nil {
		return card.Plan{}, nil, why.Reason, "nova-sprint help drop"
	}
	return merged, deps, "", ""
}
func dropDryPlan(s store.Snapshot, in dropInput, deps []string) (card.Plan, []string, string, string) {
	all := map[string]card.Card{}
	for id, c := range s.Cards {
		all[id] = card.FromStore(c)
	}
	plan, why := dropMove(s, all, in.id, card.Cancel{Reason: in.reason})
	if why != nil {
		return card.Plan{}, nil, why.Reason, "nova-sprint show " + in.id
	}
	return plan, deps, "", ""
}

func dropDependants(s store.Snapshot, id string) []string {
	var deps []string
	for _, c := range s.Cards {
		if (c.Kind == "producer" || c.Kind == "sentinel") && c.State != "fail" && c.State != "landed" && slices.Contains(c.Needs, id) {
			deps = append(deps, c.ID)
		}
	}
	slices.Sort(deps)
	return deps
}
func dropBlocked(s store.Snapshot, id string) (string, string) {
	var live []store.Card
	for _, c := range s.Cards {
		if c.Of == id && (c.State == "ready" || c.State == "working") {
			live = append(live, c)
		}
	}
	slices.SortFunc(live, func(a, b store.Card) int { return strings.Compare(a.ID, b.ID) })
	if len(live) > 0 {
		c := live[0]
		return "card " + id + " has live consumer " + c.ID + " held by " + c.Holder, fmt.Sprintf("nova-sprint release %s@%d --as %s --reason <why>", c.ID, c.Gen, c.Holder)
	}
	var js []string
	for _, j := range s.Judgements {
		if j.About == id && j.State == "open" {
			js = append(js, j.ID)
		}
	}
	slices.Sort(js)
	if len(js) > 0 {
		return "card " + id + " has open judgement " + js[0], "nova-sprint judge " + js[0] + " drop --reason <why>"
	}
	return "", ""
}
func dropMove(s store.Snapshot, all map[string]card.Card, id string, event card.Event) (card.Plan, *card.Refusal) {
	p := s.Cards[id]
	return card.Move(card.FromStore(p), consumers(s, id), nil, card.StreamFromStore(s.Streams[p.Stream]), all, event)
}
