package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"slices"
	"strings"
)

func parseEdit(args []string, actor string) (string, string, string, string, bool) {
	if len(args) < 1 {
		return "", "", "", "", false
	}
	id, file, reason := args[0], "", ""
	for i := 1; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return "", "", "", "", false
		}
		switch args[i] {
		case "--brief-file":
			file = args[i+1]
		case "--reason":
			reason = args[i+1]
		case "--as":
			actor = args[i+1]
		default:
			return "", "", "", "", false
		}
		i++
	}
	if id == "" || file == "" || reason == "" || actor == "" {
		return "", "", "", "", false
	}
	return id, file, reason, actor, true
}

func (e *Engine) edit(ctx context.Context, args []string) Result {
	id, file, reason, actor, valid := parseEdit(args, e.Actor)
	if !valid {
		return refused("edit", "invalid arguments", "nova-sprint help edit")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil || e.Briefs == nil {
		return unavailable("edit")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return operationalFailure("edit", err)
	}
	if !hooked {
		return refused("edit", actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("edit", err)
	}
	p, ok := snap.Cards[id]
	if !ok {
		return refused("edit", "unknown card "+id, "nova-sprint show "+id)
	}
	stream, ok := snap.Streams[p.Stream]
	if !ok {
		return unavailable("edit")
	}
	cs := consumers(snap, id)
	slices.SortFunc(cs, func(a, b card.Card) int { return strings.Compare(a.ID, b.ID) })
	for _, c := range cs {
		if c.State == "ready" || c.State == "working" {
			return refused("edit", "producer "+id+" is held by "+c.Holder+" in consumer "+c.ID, "nova-bus send --as "+actor+" --to "+c.Holder+" <subject> <message>")
		}
	}
	a, err := e.Briefs.Parse(ctx, file, stream)
	if err != nil {
		return operationalFailure("edit", err)
	}
	plan, why := card.Move(card.FromStore(p), consumers(snap, id), nil, card.StreamFromStore(stream), nil, card.Edit{Prose: a.Prose, Paths: a.Paths, DoneWhen: a.DoneWhen, Reason: reason})
	if why != nil {
		return refused("edit", why.Reason, "nova-sprint show "+id)
	}
	key, _ := json.Marshal([]string{"edit", actor, id, file, reason, a.Prose})
	op := store.Op{ID: consumerOpID(string(key), snap.Epoch, id, int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: "edit", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("edit", err)
	}
	if !rec.Accepted {
		return refused("edit", rec.Reason, "nova-sprint show "+id)
	}
	return Result{fmt.Sprintf("EDITED %s\nnext: nothing; wait for the bus\n", id), 0}
}
