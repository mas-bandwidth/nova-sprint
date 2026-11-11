package verbs

import (
	"context"
	"fmt"
	"maps"
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
	// worker's beat moves.
	if snap, err = e.snapshot(ctx); err != nil {
		return "", err
	}
	if _, ok := snap.Streams[name]; ok {
		return "", refuse("stream "+name+" exists", "nova-sprint show "+name)
	}
	pos := 0
	for _, s := range snap.Streams {
		pos = max(pos, s.Pos+1)
	}
	op := e.newOp("stream-add", in.actor, snap, name)
	op.Expect = []store.Expect{store.UnchangedSince{Seq: snap.Seq, Streams: true, Hooks: true}} // and the caller still the coordinator it was read as
	op.Writes = []store.Write{store.PutStream{Name: name, Repo: repo, Base: base, Pos: pos, Mode: mode, Forge: forge}}
	if len(gates) > 0 {
		op.Writes = append(op.Writes, store.SetStreamGates{Stream: name, Gates: gates})
	}
	r, err := e.commit(ctx, op, "nova-sprint show "+name)
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

// runStreamSet is `stream set`: a stream's mode, forge, gates or base, one
// write each. A change of mode, forge or base is admitted again (its
// repository, base and group branch); the gates change nothing there. A
// base change is refused while cards merged onto the old base wait for
// their promotion to main (it looks for them on the stream's base); cards
// merging have their gate let go and the group built on the new base by
// the landing's next pass (edges.go land), and cards working finish their
// attempt on the old base, their heads landing onto the new one.
func runStreamSet(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name := in.arg(0)
	s, ok := snap.Streams[name]
	if !ok {
		return "", refuse("no stream "+name, "nova-sprint stream add "+name+" --repo <org/repo>")
	}
	if !in.has("mode") && !in.has("forge") && !in.has("gate") && !in.has("base") {
		return "", misuse("stream set", "stream set changes --mode, --forge, --gate, --base, or several")
	}
	oldBase, base := s.Base, orWord(in.str("base"), s.Base)
	var merging, working []string
	if base != oldBase {
		var based []string
		for _, id := range slices.Sorted(maps.Keys(snap.Cards)) {
			c := snap.Cards[id]
			_, onBase := store.BaseMerge(c.Reason)
			switch {
			case c.Stream != name || c.Kind != "producer":
			case c.State == "merging" && onBase:
				based = append(based, id)
			case c.State == "merging":
				merging = append(merging, id)
			case c.State == "working":
				working = append(working, id)
			}
		}
		if len(based) > 0 {
			return "", refuse("stream "+name+" has cards merged onto "+oldBase+" that wait for their promotion to main ("+strings.Join(based, ", ")+")",
				"let the promotion land them on main (nova-sprint show "+name+"), then set the base again")
		}
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
	if base != oldBase { // overwrites repo, base, pos, mode and forge; keeps held and gates
		ws = append(ws, store.PutStream{Name: name, Repo: s.Repo, Base: base, Pos: s.Pos, Mode: mode, Forge: forge})
	} else {
		if mode != orWord(s.Mode, store.StreamAuto) {
			ws = append(ws, store.SetStreamMode{Stream: name, Mode: mode})
		}
		if forge != s.Forge {
			ws = append(ws, store.SetStreamForge{Stream: name, Forge: forge})
		}
	}
	landing := len(ws) > 0
	if !slices.Equal(gates, s.Gates) {
		ws = append(ws, store.SetStreamGates{Stream: name, Gates: gates})
	}
	if len(ws) == 0 {
		return done(waitNext, append([]string{fmt.Sprintf("STREAM %s base=%s mode=%s forge=%s unchanged", name, base, mode, orWord(forge, "none"))}, gateLines(gates)...)...), nil
	}
	// the stream it makes is admitted as stream add admits one
	s.Mode, s.Forge, s.Base = mode, forge, base
	if landing {
		if _, err := e.admitStream(ctx, s, "set the stream again"); err != nil {
			return "", err
		}
		if err := e.ensureGroup(ctx, s); err != nil {
			return "", err
		}
	}
	op := e.newOp("stream-set", in.actor, snap, name, mode, forge, gates, base)
	op.Expect = []store.Expect{store.UnchangedSince{Seq: snap.Seq, Streams: true, Hooks: true}} // and the caller still the coordinator it was read as
	op.Writes = ws
	r, err := e.commit(ctx, op, "nova-sprint show "+name)
	if err != nil {
		return "", err
	}
	line := fmt.Sprintf("STREAM %s mode=%s forge=%s applied at seq %d", name, mode, orWord(forge, "none"), r.Seq)
	var moved []string
	if base != oldBase {
		line = fmt.Sprintf("STREAM %s base=%s (was %s) mode=%s forge=%s applied at seq %d", name, base, oldBase, mode, orWord(forge, "none"), r.Seq)
		if len(merging) > 0 {
			moved = append(moved, "NOTE cards merging ("+strings.Join(merging, ", ")+"): the landing lets go of any gate built on "+oldBase+" and builds the group on "+base+" at its next pass")
		}
		if len(working) > 0 {
			moved = append(moved, "NOTE cards working ("+strings.Join(working, ", ")+"): each finishes its attempt on "+oldBase+" and its head lands onto "+base+"; cards taken from now on, and every retry, start from "+base)
		}
	}
	return done(waitNext, append(append(append([]string{line}, moved...), gateLines(gates)...), e.gatelessWarn(ctx, snap, store.Stream{Name: name, Gates: gates})...)...), nil
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
