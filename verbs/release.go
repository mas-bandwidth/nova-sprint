package verbs

import (
	"context"
	"strconv"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func parseRelease(args []string, actor string) (string, int, string, string, int, bool) {
	if len(args) < 1 {
		return "", 0, "", "", 0, false
	}
	id, gen, ok := cardRef(args[0])
	if !ok {
		return "", 0, "", "", 0, false
	}
	reason := ""
	epoch := 0
	for i := 1; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return "", 0, "", "", 0, false
		}
		switch args[i] {
		case "--as":
			actor = args[i+1]
		case "--reason":
			reason = args[i+1]
		case "--epoch":
			var err error
			epoch, err = strconv.Atoi(args[i+1])
			if err != nil || epoch < 1 {
				return "", 0, "", "", 0, false
			}
		default:
			return "", 0, "", "", 0, false
		}
		i++
	}
	return id, gen, actor, reason, epoch, actor != "" && reason != ""
}

func (e *Engine) release(ctx context.Context, args []string) Result {
	if len(args) > 0 && args[0] == "--row" {
		return e.releaseRow(ctx, args)
	}
	id, gen, actor, reason, epoch, ok := parseRelease(args, e.Actor)
	if !ok {
		return refused("release", "invalid arguments", "nova-sprint help release")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Now == nil || e.Env == nil {
		return unavailable("release")
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return unavailable("release")
	}
	if epoch != 0 && epoch != snap.Epoch {
		return epochRefusal("release", epoch, snap.Epoch)
	}
	opID := consumerOpID("release", snap.Epoch, id, gen)
	if r, found := replay(ctx, st, opID, actor, "release", id); found {
		return r
	}
	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return operationalFailure("release", err)
	}
	if !hooked {
		return refused("release", actor+" is not hooked in", recipe)
	}
	c, ok := snap.Cards[id]
	if !ok || c.Gen != gen || c.Holder != actor {
		return refused("release", id+" is not held by "+actor, "nova-sprint show "+id)
	}
	p, ok := snap.Cards[c.Of]
	if !ok {
		return unavailable("release")
	}
	stream, ok := snap.Streams[p.Stream]
	if !ok {
		return unavailable("release")
	}
	plan, why := card.MoveConsumer(card.FromStore(c), card.FromStore(p), card.Row{}, card.Withdraw{Reason: reason, Stream: card.StreamFromStore(stream)})
	if why != nil {
		return refused("release", why.Reason, "nova-sprint show "+id)
	}
	next := "nothing; wait for the bus"
	if len(obligations(snap, actor)) > 1 {
		next = "nova-sprint next"
	}
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: actor, Kind: "release", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes, Note: next}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return unavailable("release")
	}
	return receiptResult(rec, actor, "release", id)
}
