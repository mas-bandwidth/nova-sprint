package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
	gates, err := streamGates(in, name, nil)
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
	tip, err := e.admitStream(ctx, store.Stream{Name: name, Repo: repo, Base: base, Forge: forge}, "add the stream again")
	if err != nil {
		return "", err
	}
	redLine := e.baseRedLine(ctx, store.Stream{Name: name, Repo: repo, Base: base, Forge: forge}, tip)
	if err := e.ensureGroup(ctx, store.Stream{Name: name, Repo: repo, Base: base, Mode: mode, Forge: forge}); err != nil {
		return "", err
	}
	// The admission reads the forge for seconds while a running sprint's
	// ticks move the store: the op is planned on a snapshot read after it
	// and fenced on the streams it read (the name absent, the next pos) and
	// the hooks (the caller the coordinator), never on the seq, which every
	// worker's beat moves. Another stream or another row's hook written
	// between is not its own: it reads again, the pos again (fenced).
	if snap, err = e.snapshot(ctx); err != nil {
		return "", err
	}
	if _, ok := snap.Streams[name]; ok {
		return "", refuse("stream "+name+" exists", "nova-sprint show "+name)
	}
	plan := func(s store.Snapshot) (store.Op, error) {
		pos := 0
		for _, st := range s.Streams {
			pos = max(pos, st.Pos+1)
		}
		op := e.newOp("stream-add", in.actor, s, name)
		op.Expect = []store.Expect{store.UnchangedSince{Seq: s.Seq, Streams: true, Hooks: true}} // and the caller still the coordinator it was read as
		op.Writes = []store.Write{store.PutStream{Name: name, Repo: repo, Base: base, Pos: pos, Mode: mode, Forge: forge}}
		if len(gates) > 0 {
			op.Writes = append(op.Writes, store.SetStreamGates{Stream: name, Gates: gates})
		}
		return op, nil
	}
	added := func(_, now store.Snapshot) string {
		if _, ok := now.Streams[name]; ok {
			return "stream " + name + " was added since this verb read it"
		}
		return ""
	}
	r, _, err := e.fenced(ctx, snap, "nova-sprint show "+name, owns(hookChange(in.actor), added), plan)
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("STREAM %s repo=%s base=%s mode=%s forge=%s applied at seq %d", name, repo, base, mode, orWord(forge, "none"), r.Seq)}
	if len(gates) > 0 {
		lines = append(append(lines, gateLines(gates)...), e.gatelessWarn(ctx, snap, store.Stream{Name: name, Gates: gates})...)
	}
	if redLine != "" {
		lines = append(lines, redLine)
	}
	if what, fix := e.agreementWarning(ctx, snap, store.Stream{Name: name, Repo: repo, Base: base, Forge: forge}); what != "" {
		lines = append(lines, "WARN "+oneLine(what)+": "+oneLine(fix))
	}
	return done("nova-sprint add --stream "+name+" --brief-file <f>", lines...), nil
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

// runStreamSet is `stream set`: a stream's mode, forge or gates, one write
// each. Only a change of mode or forge is admitted again (its repository,
// base and group branch); the gates change nothing there.
func runStreamSet(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name := in.arg(0)
	s, ok := snap.Streams[name]
	if !ok {
		return "", refuse("no stream "+name, "nova-sprint stream add "+name+" --repo <org/repo>")
	}
	if !in.has("mode") && !in.has("forge") && !in.has("gate") {
		return "", misuse("stream set", "stream set changes --mode, --forge, --gate, or several")
	}
	mode, forge, err := streamSettings(in, s.Mode, s.Forge)
	if err != nil {
		return "", err
	}
	gates, err := streamGates(in, name, s.Gates)
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
	landing := len(ws) > 0
	if !slices.Equal(gates, s.Gates) {
		ws = append(ws, store.SetStreamGates{Stream: name, Gates: gates})
	}
	if len(ws) == 0 {
		return done(waitNext, append([]string{fmt.Sprintf("STREAM %s mode=%s forge=%s unchanged", name, mode, orWord(forge, "none"))}, gateLines(gates)...)...), nil
	}
	// the stream it makes is admitted as stream add admits one
	s.Mode, s.Forge = mode, forge
	if landing {
		if _, err := e.admitStream(ctx, s, "set the stream again"); err != nil {
			return "", err
		}
		if err := e.ensureGroup(ctx, s); err != nil {
			return "", err
		}
	}
	// fenced on the streams (this one as read) and the hooks (the caller the
	// coordinator); another stream, this one's hold, or another row's hook
	// written between is not its own: it reads again (fenced)
	plan := func(cur store.Snapshot) (store.Op, error) {
		op := e.newOp("stream-set", in.actor, cur, name, mode, forge, gates)
		op.Expect = []store.Expect{store.UnchangedSince{Seq: cur.Seq, Streams: true, Hooks: true}} // and the caller still the coordinator it was read as
		op.Writes = ws
		return op, nil
	}
	r, _, err := e.fenced(ctx, snap, "nova-sprint show "+name, owns(hookChange(in.actor), streamChange(name)), plan)
	if err != nil {
		return "", err
	}
	return done(waitNext, append(append([]string{fmt.Sprintf("STREAM %s mode=%s forge=%s applied at seq %d", name, mode, orWord(forge, "none"), r.Seq)}, gateLines(gates)...), e.gatelessWarn(ctx, snap, store.Stream{Name: name, Gates: gates})...)...), nil
}

// streamChange is stream name's own record changed: what stream set read
// and admitted (repository, base, mode, forge, gates), or the stream gone.
// Its hold and its position are not stream set's.
func streamChange(name string) func(was, now store.Snapshot) string {
	return func(was, now store.Snapshot) string {
		a, b := was.Streams[name], now.Streams[name]
		if _, ok := now.Streams[name]; !ok {
			return "stream " + name + " is gone since this verb read it"
		}
		if a.Repo != b.Repo || a.Base != b.Base || a.Mode != b.Mode || a.Forge != b.Forge || !slices.Equal(a.Gates, b.Gates) {
			return fmt.Sprintf("stream %s changed since this verb read it (repo=%s base=%s mode=%s forge=%s gates=%d, now repo=%s base=%s mode=%s forge=%s gates=%d)",
				name, a.Repo, a.Base, orWord(a.Mode, store.StreamAuto), orWord(a.Forge, "none"), len(a.Gates), b.Repo, b.Base, orWord(b.Mode, store.StreamAuto), orWord(b.Forge, "none"), len(b.Gates))
		}
		return ""
	}
}

// streamGates is --gate over the stream's own gates: each given one
// replaces the whole list, in order; `--gate none` alone clears it. Each
// is one command as a DONE-WHEN is (edge.ParseGate, the parser the bench
// worker runs), refused naming the gate and the fix as add refuses a bad
// DONE-WHEN.
func streamGates(in *input, stream string, gates []string) ([]string, error) {
	if !in.has("gate") {
		return gates, nil
	}
	given := in.list("gate")
	if slices.Equal(given, []string{"none"}) {
		return nil, nil
	}
	for _, g := range given {
		if g == "none" {
			return nil, misuse(in.verb, "--gate none clears the gates and is given alone")
		}
		if _, why := edge.ParseGate(g); why != "" {
			return nil, refuse("stream "+stream+": gate "+clip(g)+" is not one command: "+why, edge.GateFix+", then nova-sprint "+in.verb+" "+stream+" --gate <command> again")
		}
	}
	return slices.Clone(given), nil
}

// gatelessWarn is the WARN line stream add and stream set print when s has
// gates and hooked bench rows run a worker that does not run them
// (gatelessRows): their cards of s finish without them. Nothing is refused.
func (e *Engine) gatelessWarn(ctx context.Context, snap store.Snapshot, s store.Stream) []string {
	if len(s.Gates) == 0 {
		return nil
	}
	if what := gatelessLine(s, e.gatelessRows(ctx, snap)); what != "" {
		return []string{"WARN " + oneLine(what) + ": " + oneLine(gatelessFix)}
	}
	return nil
}

// gateLines are a stream's gates as stream add and stream set print them:
// one GATE line each, or GATES none.
func gateLines(gates []string) []string {
	if len(gates) == 0 {
		return []string{"GATES none"}
	}
	out := make([]string, 0, len(gates))
	for _, g := range gates {
		out = append(out, "GATE "+g)
	}
	return out
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
