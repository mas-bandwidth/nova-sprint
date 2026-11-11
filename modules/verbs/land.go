package verbs

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Lander is the landing as the verbs drive it by hand (layer 4's git and
// forge edges, adapted in cmd/nova-sprint), acting as the host's gh
// identity (intent rule 32).
type Lander interface {
	// Land merges head, the tip of the card's branch, into the stream's
	// base with no check gate (the coordinator looked), and is the merge
	// sha base then has: base's tip when head is already on it. A stream
	// with a forge merges its PR (opened when there is none) by the
	// repository's allowed method; one without merges onto base and pushes
	// it, fast-forward only. Anything that keeps it from merging is a
	// refusal naming its fix.
	Land(ctx context.Context, s store.Stream, branch, head string) (string, error)
	// Heads is every branch at the stream's origin: refs/heads/<name> -> sha.
	Heads(ctx context.Context, s store.Stream) (map[string]string, error)
	// CreateGroup creates the stream's group branch at sha (its base tip);
	// it fails when the branch exists.
	CreateGroup(ctx context.Context, s store.Stream, sha string) error
	// OnMain is whether sha is on the default branch at the stream's origin.
	OnMain(ctx context.Context, s store.Stream, sha string) (bool, error)
}

// runLand is `land <card> --reason <why> [--head <sha>]`: the hand merge of
// a merging producer's read-ok head (intent rule 32), recorded as landed
// with the merge sha in one op; an open land judgement on the card is
// answered land in the same op.
func runLand(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	reason := in.str("reason")
	if reason == "" {
		return "", refuse("a hand landing needs its reason", "add --reason \"<why>\"")
	}
	line, err := e.handLand(ctx, in, snap, in.arg(0), strings.ToLower(in.str("head")), reason, "")
	if err != nil {
		return "", err
	}
	return done(waitNext, line), nil
}

// handLand lands card id at head (its own head when "") by hand, answering
// judgement (or the card's open land judgement when "") with land.
func (e *Engine) handLand(ctx context.Context, in *input, snap store.Snapshot, id, head, reason, judgement string) (string, error) {
	c, ok := snap.Cards[id]
	switch {
	case !ok || c.Kind != "producer":
		return "", refuse("no producer "+id, "nova-sprint show")
	case c.State != card.Merging:
		return "", refuse(fmt.Sprintf("%s is %s; a hand landing is of a merging producer (its head passed its reads)", id, c.State), "nova-sprint show "+id)
	case head != "" && head != c.Head:
		return "", refuse(fmt.Sprintf("%s's finished head is %s, not %s", id, c.Head, head), "nova-sprint land "+id+" --reason \"<why>\" (its own head), or send it to fix")
	case e.Lander == nil:
		return "", broken("the landing edge is not configured", "nova-sprint check")
	}
	// a merge the hand landing makes is the store's identity (round 24), never a literal
	ctx = git.WithIdentity(ctx, snap.Identity.Name, snap.Identity.Email)
	s := snap.Streams[c.Stream]
	merge, onBase := store.BaseMerge(c.Reason)
	if !onBase {
		var err error
		if merge, err = e.Lander.Land(ctx, s, branchOf(snap, c), c.Head); err != nil {
			return "", RefusalOr(err, broken("the hand landing of "+id+" failed: "+errText(err), "fix what it names, then nova-sprint land "+id+" --reason \"<why>\" again"))
		}
	}
	// landed means on main: a merge onto another base is recorded on base
	on, err := e.Lander.OnMain(ctx, s, cmp.Or(merge, c.Head))
	if err != nil {
		return "", RefusalOr(err, broken("reading whether "+id+" is on main failed: "+errText(err), "make "+s.Repo+" readable from this host, then nova-sprint land "+id+" --reason \"<why>\" again"))
	}
	if !on && onBase {
		return "", refuse(id+" is on "+s.Base+" but not on main yet", "merge "+s.Base+" into main, then nova-sprint land "+id+" --reason \"<why>\" again")
	}
	if judgement == "" {
		judgement = openLand(snap, id)
	}
	var ev card.Event = card.Landed{Head: c.Head, Merge: merge, Reason: "by hand: " + reason}
	if !on {
		ev = card.OnBase{Head: c.Head, Merge: merge, Base: s.Base}
	}
	r := steps.Request{Card: id, Event: ev, Judgement: judgement}
	if judgement != "" {
		r.Answer = "land"
	}
	line, err := e.move(ctx, in, snap, fixed(r), id)
	if err != nil {
		return "", RefusalOr(err, broken("merged at "+merge+" but not recorded: "+errText(err), "nova-sprint land "+id+" --reason \"<why>\" again (the head is on base, so it records at once)"))
	}
	if !on {
		return fmt.Sprintf("ON BASE %s head=%s merge=%s by hand (landed when %s is on main): %s", id, c.Head, merge, s.Base, line), nil
	}
	return fmt.Sprintf("LANDED %s head=%s merge=%s by hand: %s", id, c.Head, merge, line), nil
}

// branchOf is producer c's branch: its latest work consumer's (a producer
// holds none of its own).
func branchOf(snap store.Snapshot, c store.Card) string {
	out, at := "sprint/"+c.Stream+"/"+c.ID, -1
	for _, x := range snap.Cards {
		if x.Of == c.ID && x.Kind == "work" && x.Branch != "" && x.Attempt > at {
			out, at = x.Branch, x.Attempt
		}
	}
	return out
}

// openLand is the open land judgement on card id, or "".
func openLand(snap store.Snapshot, id string) string {
	for _, j := range snap.Judgements {
		if j.Kind == "land" && j.About == id && j.State == "open" {
			return j.ID
		}
	}
	return ""
}

// ensureGroup is stream add's group branch (defect 46): for a stream that
// lands through a forge, sprint-group/<stream> must be a name git accepts
// and no branch at origin may keep it from being created; absent, it is
// created at the base tip. A stream landed by hand has none.
func (e *Engine) ensureGroup(ctx context.Context, s store.Stream) error {
	if s.Forge == "" || s.Mode == "hand" {
		return nil
	}
	branch := git.GroupBranch(s.Name)
	if why := git.GroupNameProblem(s.Name); why != "" {
		return refuse("stream "+s.Name+"'s group branch "+branch+" cannot be created: "+why, "name the stream with letters, digits, _ and - (no leading or trailing dot, no .., no .lock), then add it again")
	}
	if e.Lander == nil {
		return broken("the landing edge is not configured", "nova-sprint check")
	}
	heads, err := e.Lander.Heads(ctx, s)
	if err != nil {
		return RefusalOr(err, broken("listing "+s.Repo+"'s branches failed: "+errText(err), "make the repository readable from this host, then add the stream again"))
	}
	if b := git.GroupBlocker(s.Name, heads); b != "" {
		return refuse("branch "+b+" at "+s.Repo+" keeps the group branch "+branch+" from being created", "rename or delete "+b+" at origin, then add the stream again")
	}
	if _, ok := heads[git.GroupRef(s.Name)]; ok {
		return nil
	}
	tip, err := e.baseTip(ctx, s, "add the stream again")
	if err != nil {
		return err
	}
	if err := e.Lander.CreateGroup(ctx, s, tip); err != nil {
		return RefusalOr(err, broken("creating "+branch+" at "+s.Repo+" failed: "+errText(err), "give this host's gh identity push access to "+s.Repo+", then add the stream again"))
	}
	return nil
}
