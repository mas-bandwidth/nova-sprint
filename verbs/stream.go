package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// forges are the forges the verb takes: none, or github. The store keeps
// an open string; an empty forge is no PR and no CI, landed by hand.
var forges = []string{"", "github"}

// runStreamAdd is `stream add`: a stream, its repository and base branch
// (checked at origin), its landing mode and forge, as store state.
func runStreamAdd(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name, repo, base := in.arg(0), in.str("repo"), orWord(in.str("base"), "main")
	mode, forge, err := streamSettings(in, store.StreamAuto, "")
	if err != nil {
		return "", err
	}
	switch {
	case !validName(name):
		return "", misuse("stream add", "a stream's name is 1 to 128 of a-z A-Z 0-9 . _ -")
	case repo == "":
		return "", misuse("stream add", "stream add names its repository: --repo <org/repo>")
	}
	if _, ok := snap.Streams[name]; ok {
		return "", refuse("stream "+name+" exists", "nova-sprint stream set "+name+" --mode <m> --forge <f>, or nova-sprint show "+name)
	}
	if err := e.baseAt(ctx, repo, base); err != nil {
		return "", err
	}
	pos := 0
	for _, s := range snap.Streams {
		pos = max(pos, s.Pos+1)
	}
	op := e.newOp("stream-add", in.actor, snap, name)
	op.Expect = []store.Expect{store.AtSeq{Seq: snap.Seq}}
	op.Writes = []store.Write{store.PutStream{Name: name, Repo: repo, Base: base, Pos: pos, Mode: mode, Forge: forge}}
	r, err := e.commit(ctx, op, "nova-sprint show "+name)
	if err != nil {
		return "", err
	}
	return done("nova-sprint add --stream "+name+" --brief-file <f>",
		fmt.Sprintf("STREAM %s repo=%s base=%s mode=%s forge=%s applied at seq %d", name, repo, base, mode, orWord(forge, "none"), r.Seq)), nil
}

// baseAt refuses a base branch origin does not have.
func (e *Engine) baseAt(ctx context.Context, repo, base string) error {
	if e.Origin == nil {
		return broken("the git edge is not configured", "nova-sprint check")
	}
	_, ok, err := e.Origin.Tip(ctx, repo, base)
	if err != nil {
		return broken("git ls-remote of "+repo+" failed ("+clip(err.Error())+")", "check the repository is readable, then add the stream again")
	}
	if !ok {
		return refuse("branch "+base+" is not at "+repo+"'s origin", "push it, or pass --base <branch>")
	}
	return nil
}

// runStreamSet is `stream set`: a stream's mode or forge, one write each.
func runStreamSet(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name := in.arg(0)
	s, ok := snap.Streams[name]
	if !ok {
		return "", refuse("no stream "+name, "nova-sprint stream add "+name+" --repo <org/repo>")
	}
	if !in.has("mode") && !in.has("forge") {
		return "", misuse("stream set", "stream set changes --mode, --forge, or both")
	}
	mode, forge, err := streamSettings(in, s.Mode, s.Forge)
	if err != nil {
		return "", err
	}
	var ws []store.Write
	if mode != orWord(s.Mode, store.StreamAuto) {
		ws = append(ws, store.SetStreamMode{Stream: name, Mode: mode})
	}
	if forge != s.Forge {
		ws = append(ws, store.SetStreamForge{Stream: name, Forge: forge})
	}
	if len(ws) == 0 {
		return done(waitNext, fmt.Sprintf("STREAM %s mode=%s forge=%s unchanged", name, mode, orWord(forge, "none"))), nil
	}
	op := e.newOp("stream-set", in.actor, snap, name, mode, forge)
	op.Writes = ws
	r, err := e.commit(ctx, op, "nova-sprint show "+name)
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("STREAM %s mode=%s forge=%s applied at seq %d", name, mode, orWord(forge, "none"), r.Seq)), nil
}

// streamSettings is --mode and --forge over the stream's own; `--forge
// none` clears the forge.
func streamSettings(in *input, mode, forge string) (string, string, error) {
	mode = orWord(in.str("mode"), orWord(mode, store.StreamAuto))
	if !slices.Contains(store.StreamModes, mode) {
		return "", "", misuse(in.verb, "--mode is one of "+strings.Join(store.StreamModes, " "))
	}
	if in.has("forge") {
		forge = in.str("forge")
	}
	if forge == "none" {
		forge = ""
	}
	if !slices.Contains(forges, forge) {
		return "", "", misuse(in.verb, "--forge is github, or none for no PR and no CI")
	}
	return mode, forge, nil
}
