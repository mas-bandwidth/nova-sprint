package verbs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func (e *Engine) priority(ctx context.Context, args []string) Result {
	id, level, reason, actor, ok := parsePriority(args, e.Actor)
	if !ok {
		return refused("priority", "invalid arguments", "nova-sprint help priority")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil {
		return unavailable("priority")
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("priority", err)
	}

	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return operationalFailure("priority", err)
	}
	if !hooked {
		return refused("priority", actor+" is not hooked in", recipe)
	}
	p, ok := snap.Cards[id]
	if !ok {
		return refused("priority", "unknown card "+id, "nova-sprint show "+id)
	}
	if p.Kind == "work" || p.Kind == "read" {
		return refused("priority", id+" is a consumer; priority belongs to its producer", "nova-sprint priority "+p.Of+" "+level+" --reason <why>")
	}
	plan, why := card.Move(card.FromStore(p), consumers(snap, p.ID), nil, card.StreamFromStore(snap.Streams[p.Stream]), nil, card.SetPriority{Level: level, Reason: reason})
	if why != nil {
		return refused("priority", why.Reason, "nova-sprint show "+p.ID)
	}
	key, _ := json.Marshal([]string{"priority", actor, level, reason})
	op := store.Op{ID: consumerOpID(string(key), snap.Epoch, p.ID, int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: "priority", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("priority", err)
	}
	if !rec.Accepted {
		return refused("priority", rec.Reason, "nova-sprint show "+p.ID)
	}
	return Result{fmt.Sprintf("PRIORITY %s %s\nnext: nothing; wait for the bus\n", p.ID, level), 0}
}

func consumers(s store.Snapshot, id string) []card.Card {
	result := []card.Card{}
	for _, c := range s.Cards {
		if c.Of == id {
			result = append(result, card.FromStore(c))
		}
	}
	return result
}

func parsePriority(args []string, actor string) (string, string, string, string, bool) {
	if len(args) < 2 {
		return "", "", "", "", false
	}
	reason := ""
	for i := 2; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return "", "", "", "", false
		}
		switch args[i] {
		case "--reason":
			reason = args[i+1]
		case "--as":
			actor = args[i+1]
		default:
			return "", "", "", "", false
		}
		i++
	}
	return args[0], args[1], reason, actor, reason != "" && actor != ""
}
