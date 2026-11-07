package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/git"
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
	if _, err := e.admitStream(ctx, store.Stream{Name: name, Repo: repo, Base: base, Forge: forge}, "add the stream again"); err != nil {
		return "", err
	}
	if err := e.ensureGroup(ctx, store.Stream{Name: name, Repo: repo, Base: base, Mode: mode, Forge: forge}); err != nil {
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

// baseTip is the stream's base branch tip at origin, read through the git
// edge at the stream's clone address (git.CloneURL); again is what to do
// after the fix. A repository git cannot address, cannot read, or without
// the base is a refusal naming its fix.
func (e *Engine) baseTip(ctx context.Context, s store.Stream, again string) (string, error) {
	if e.Origin == nil {
		return "", broken("the git edge is not configured", "nova-sprint check")
	}
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "", RefusalOr(err, refuse(err.Error(), "name the repository --repo <org>/<repo> with --forge github, or by its clone URL, then "+again))
	}
	sha, ok, err := e.Origin.Tip(ctx, url, s.Base)
	if err != nil {
		fix := "make " + url + " readable from this host (for GitHub: gh auth login on this host as an identity that can read it), then " + again
		if s.Forge == "" && git.IsOrgRepo(s.Repo) {
			fix = "an <org>/<repo> is cloned through its forge: give stream " + s.Name + " --forge github, then " + again
		}
		return "", RefusalOr(err, broken("git ls-remote of "+url+" failed ("+errText(err)+")", fix))
	}
	if !ok {
		return "", refuse("branch "+s.Base+" is not at "+s.Repo+"'s origin ("+url+")", "push it, or pass --base <branch>, then "+again)
	}
	return sha, nil
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
