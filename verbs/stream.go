package verbs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Repositories checks a stream's remote without giving the store credentials.
type Repositories interface {
	Readable(context.Context, string, string, bool) error
}

type streamInput struct {
	name, repo, base, actor, mode string
	pr                            bool
}

func parseStream(args []string, actor string) (streamInput, bool) {
	in := streamInput{base: "main", actor: actor, mode: "auto"}
	if len(args) < 1 {
		return in, false
	}
	in.name = args[0]
	for i := 1; i < len(args); i++ {
		if args[i] == "--pr" {
			in.pr = true
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return in, false
		}
		switch args[i] {
		case "--repo":
			in.repo = args[i+1]
		case "--base":
			in.base = args[i+1]
		case "--mode":
			in.mode = args[i+1]
		case "--as":
			in.actor = args[i+1]
		default:
			return in, false
		}
		i++
	}
	return in, in.name != "" && in.repo != "" && in.actor != ""
}
func (e *Engine) stream(ctx context.Context, args []string) Result {
	if len(args) > 0 && args[0] == "set" {
		return e.setStream(ctx, args[1:])
	}
	if len(args) == 0 || args[0] != "add" {
		return refused("stream", "invalid arguments", "nova-sprint help stream")
	}
	in, ok := parseStream(args[1:], e.Actor)
	if !ok {
		return refused("stream add", "invalid arguments", "nova-sprint help stream add")
	}
	if !validMode(in.mode) {
		return refused("stream add", "mode "+in.mode+" is not one of auto, group or hand", "nova-sprint stream add "+in.name+" --repo "+in.repo+" --mode auto")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil || e.Repositories == nil {
		return unavailable("stream add")
	}
	schema, err := e.Env.Schema(ctx)
	if err != nil {
		return operationalFailure("stream add", err)
	}
	if !schema {
		return refused("stream add", "store is not at this schema", "nova-sprint up")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure("stream add", err)
	}
	if !hooked {
		return refused("stream add", in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("stream add", err)
	}
	if _, exists := snap.Streams[in.name]; exists {
		return refused("stream add", "stream "+in.name+" exists", "nova-sprint show "+in.name)
	}
	if err = e.Repositories.Readable(ctx, in.repo, in.base, in.pr); err != nil {
		return operationalFailure("stream add", err)
	}
	return e.addStream(ctx, st, snap, in)
}
func (e *Engine) addStream(ctx context.Context, st ApplyStore, snap store.Snapshot, in streamInput) Result {
	settings, _ := json.Marshal(streamSettings{Repo: in.repo, Base: in.base, PR: in.pr, Mode: in.mode})
	pos := 1
	for _, s := range snap.Streams {
		if s.Pos >= pos {
			pos = s.Pos + 1
		}
	}
	key, _ := json.Marshal([]string{"stream add", in.actor, in.name, in.repo, in.base, fmt.Sprint(in.pr), in.mode})
	op := store.Op{ID: consumerOpID(string(key), snap.Epoch, in.name, int(snap.Seq)), Epoch: snap.Epoch, Actor: in.actor, Kind: "stream add", At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutStream{Name: in.name, Repo: in.repo, Base: in.base, Pos: pos}, store.PutPolicy{Kind: "data", Name: "stream:" + in.name, Body: string(settings)}}}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("stream add", err)
	}
	if !rec.Accepted {
		return refused("stream add", rec.Reason, "nova-sprint stream add "+in.name+" --repo "+in.repo)
	}
	return Result{fmt.Sprintf("STREAM ADDED %s repo=%s base=%s branch=sprint/%s\nnext: nova-sprint add --stream %s --brief-file <file>\n", in.name, in.repo, in.base, in.name, in.name), 0}
}

func validMode(mode string) bool { return mode == "auto" || mode == "group" || mode == "hand" }
