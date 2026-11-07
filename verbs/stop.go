package verbs

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func parseStop(args []string, actor string, stop bool) (string, string, bool) {
	reason := ""
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" || seen[args[i]] {
			return "", "", false
		}
		seen[args[i]] = true
		switch args[i] {
		case "--as":
			actor = args[i+1]
		case "--reason":
			reason = args[i+1]
		default:
			return "", "", false
		}
		i++
	}
	return actor, reason, actor != "" && (!stop || reason != "")
}
func (e *Engine) setStopped(ctx context.Context, verb string, args []string) Result {
	stopped := verb == "stop"
	actor, reason, ok := parseStop(args, e.Actor, stopped)
	if !ok {
		return refused(verb, "invalid arguments", "nova-sprint help "+verb)
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Now == nil {
		return unavailable(verb)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure(verb, err)
	}
	op := store.Op{ID: consumerOpID(verb+":"+actor, snap.Epoch, reason, int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: verb, At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.SetStopped{Stopped: stopped, Reason: reason}}}
	receipt, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure(verb, err)
	}
	if !receipt.Accepted {
		return refused(verb, receipt.Reason, "nova-sprint show run")
	}
	label, next := "STARTED", "nothing; wait for the bus"
	if stopped {
		label, next = "STOPPED", "nova-sprint start"
	}
	return Result{fmt.Sprintf("%s at seq %d\nnext: %s\n", label, receipt.Seq, next), 0}
}
