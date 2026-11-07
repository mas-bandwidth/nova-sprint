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

type holdInput struct {
	names         []string
	reason, actor string
	dry           bool
}

func parseHold(args []string, actor string) (holdInput, bool) {
	in := holdInput{actor: actor}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			in.dry = true
		case "--as", "--reason":
			if i+1 >= len(args) || args[i+1] == "" {
				return in, false
			}
			if args[i] == "--as" {
				in.actor = args[i+1]
			} else {
				in.reason = args[i+1]
			}
			i++
		default:
			if strings.HasPrefix(args[i], "-") || args[i] == "" || seen[args[i]] {
				return in, false
			}
			seen[args[i]] = true
			in.names = append(in.names, args[i])
		}
	}
	slices.Sort(in.names)
	return in, len(in.names) > 0 && in.reason != "" && in.actor != ""
}

func (e *Engine) hold(ctx context.Context, verb string, args []string) Result {
	in, ok := parseHold(args, e.Actor)
	if !ok {
		return refused(verb, "invalid arguments", "nova-sprint help "+verb)
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil {
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
	plans, withdrawn, why := planHolds(snap, in, verb == "hold")
	if why != "" {
		return refused(verb, why, "nova-sprint show")
	}
	plan, mergeWhy := card.Merge(plans...)
	if mergeWhy != nil {
		return refused(verb, mergeWhy.Reason, "nova-sprint show")
	}
	preview := fmt.Sprintf("%s targets=%s withdraw=%s dependants-fail=-", strings.ToUpper(verb), strings.Join(in.names, ","), formatNeeds(withdrawn))
	if in.dry {
		return Result{preview + " (dry run)\nnext: nova-sprint " + verb + " <row|stream> --reason <why>\n", 0}
	}
	if e.Preview == nil {
		return unavailable(verb)
	}
	if err := e.Preview(ctx, preview+"\n"); err != nil {
		return operationalFailure(verb, err)
	}
	plan.Expect = append(plan.Expect, store.AtSeq{Seq: snap.Seq})
	key, _ := json.Marshal([]any{in.names, in.reason, in.actor, verb})
	op := store.Op{ID: consumerOpID(verb+":"+in.actor, snap.Epoch, string(key), int(snap.Seq)), Epoch: snap.Epoch, Actor: in.actor, Kind: verb, At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure(verb, err)
	}
	if !rec.Accepted {
		return refused(verb, rec.Reason, "retry nova-sprint "+verb)
	}
	return Result{strings.ToUpper(verb) + " applied\nnext: nothing; wait for the bus\n", 0}
}

func planHolds(s store.Snapshot, in holdInput, holding bool) ([]card.Plan, []string, string) {
	var plans []card.Plan
	var withdrawn []string
	names := append([]string(nil), in.names...)
	slices.SortStableFunc(names, func(a, b string) int {
		_, as := s.Streams[a]
		_, bs := s.Streams[b]
		if as == bs {
			return strings.Compare(a, b)
		}
		if as {
			return -1
		}
		return 1
	})
	for _, name := range names {
		r, row := s.Rows[name]
		stream, streamOK := s.Streams[name]
		if row && streamOK {
			return nil, nil, "name " + name + " is both a row and a stream"
		}
		if !row && !streamOK {
			return nil, nil, "unknown name " + name
		}
		if streamOK {
			plan, why := holdStreamPlan(s, stream, in.reason, holding)
			if why != nil {
				return nil, nil, why.Reason
			}
			plans = append(plans, plan)
			continue
		}
		var event card.Event = card.Unhold{Reason: in.reason}
		if holding {
			event = card.Hold{Reason: in.reason}
		}
		plan, why := card.MoveRow(card.RowFromStore(r, 0, nil), event)
		if why != nil {
			return nil, nil, why.Reason
		}
		plans = append(plans, plan)

	}
	slices.Sort(withdrawn)
	return plans, withdrawn, ""
}

func holdStreamPlan(s store.Snapshot, stream store.Stream, reason string, holding bool) (card.Plan, *card.Refusal) {
	if !holding {
		return card.MoveStream(card.StreamFromStore(stream), card.UnholdStream{Reason: reason})
	}
	ready := []card.Card{}
	for _, p := range s.Cards {
		if p.Stream == stream.Name && p.Kind == "producer" && p.State == "ready" {
			ready = append(ready, card.FromStore(p))
		}
	}
	slices.SortFunc(ready, func(a, b card.Card) int { return strings.Compare(a.ID, b.ID) })
	return card.MoveStream(card.StreamFromStore(stream), card.HoldStream{Reason: reason, Ready: ready, Seq: s.Seq})
}
