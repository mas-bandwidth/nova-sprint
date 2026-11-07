package verbs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type streamSettings struct {
	Repo string `json:"repo"`
	Base string `json:"base"`
	PR   bool   `json:"pr"`
	Mode string `json:"mode"`
}

func (e *Engine) setStream(ctx context.Context, args []string) Result {
	name, mode, actor, ok := parseStreamSet(args, e.Actor)
	if !ok {
		return refused("stream set", "invalid arguments", "nova-sprint help stream set")
	}

	if !validMode(mode) {
		return refused("stream set", "mode "+mode+" is not one of auto, group or hand", "nova-sprint stream set "+name+" --mode auto")
	}
	st, ok := e.Store.(store.Store)
	if !ok || e.Env == nil || e.Now == nil {
		return unavailable("stream set")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return operationalFailure("stream set", err)
	}
	if !hooked {
		return refused("stream set", actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("stream set", err)
	}
	if _, ok := snap.Streams[name]; !ok {
		return refused("stream set", "unknown stream "+name, "nova-sprint stream add "+name+" --repo <owner/name>")
	}
	body, found, err := (Choices{Store: st}).getAt(ctx, "stream:"+name, snap)
	if err != nil {
		return operationalFailure("stream set", err)
	}
	if !found {
		return unavailable("stream set")
	}
	var settings streamSettings
	if json.Unmarshal([]byte(body), &settings) != nil || !validMode(settings.Mode) {
		return unavailable("stream set")
	}
	if settings.Mode == mode {
		return refused("stream set", "stream "+name+" already uses "+mode, "nova-sprint show "+name)
	}
	settings.Mode = mode
	data, _ := json.Marshal(settings)
	op := store.Op{ID: consumerOpID("stream set:"+actor, snap.Epoch, name+"\x00"+mode, int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: "stream set", At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "stream:" + name, Body: string(data)}}}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("stream set", err)
	}
	if !rec.Accepted {
		return refused("stream set", rec.Reason, "nova-sprint stream set "+name+" --mode "+mode)
	}
	return Result{fmt.Sprintf("STREAM SET %s mode=%s\nnext: nothing; wait for the bus\n", name, mode), 0}
}

func parseStreamSet(args []string, actor string) (string, string, string, bool) {
	if len(args) < 3 {
		return "", "", "", false
	}
	name := args[0]
	mode := ""
	for i := 1; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return "", "", "", false
		}
		switch args[i] {
		case "--mode":
			mode = args[i+1]
		case "--as":
			actor = args[i+1]
		default:
			return "", "", "", false
		}
		i++
	}
	return name, mode, actor, name != "" && actor != ""
}
