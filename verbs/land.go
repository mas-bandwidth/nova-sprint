package verbs

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Landing is the forge as the coordinator's hand merge uses it (edge/forge's
// Client, the host's gh): the same calls the run's landing makes.
type Landing interface {
	FindPR(ctx context.Context, repo, head, base string) (forge.PRState, bool, error)
	OpenPR(ctx context.Context, repo, head, base, title, body string) (forge.PRInfo, error)
	Merge(ctx context.Context, repo string, pr int, sha, base string) error
	MergePR(ctx context.Context, repo string, pr int, head string, methods []string) (string, error)
	Behind(ctx context.Context, repo, base, head string) (int, error)
}

// runLand is `land <card|stream> --reason <why> [--head <sha>]`: the
// coordinator's hand merge after looking (INTENT rule 16, rung 3). A card:
// its PR from its branch into the base (opened if it has none) merged as
// the landing merges it, then Landed. A stream: its group's PR merged if
// one is open, then every merging producer of the stream whose head is in
// the base is Landed. An open land judgement on it is answered land.
func runLand(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	return e.landHand(ctx, in, snap, in.arg(0), "")
}

func (e *Engine) landHand(ctx context.Context, in *input, snap store.Snapshot, about, judgement string) (string, error) {
	if in.str("reason") == "" {
		return "", refuse("a hand landing needs its reason", "add --reason \"<why>\"")
	}
	if e.Landing == nil || e.Origin == nil {
		return "", broken("the forge edge is not configured", "nova-sprint check")
	}
	if judgement == "" {
		judgement = openLand(snap, about)
	}
	if _, ok := snap.Streams[about]; ok {
		return e.landStream(ctx, in, snap, about, judgement)
	}
	return e.landCard(ctx, in, snap, about, judgement)
}

// openLand is the open land judgement about about, or "".
func openLand(snap store.Snapshot, about string) string {
	for _, id := range slices.Sorted(maps.Keys(snap.Judgements)) {
		if j := snap.Judgements[id]; j.Kind == "land" && j.About == about && j.State == "open" {
			return id
		}
	}
	return ""
}

func (e *Engine) landCard(ctx context.Context, in *input, snap store.Snapshot, id, judgement string) (string, error) {
	c, ok := snap.Cards[id]
	if !ok || c.Kind != "producer" {
		return "", refuse("no producer "+id, "nova-sprint show "+id)
	}
	if c.State != "merging" {
		return "", refuse(id+" is "+c.State+"; a hand landing is of a merging producer (read ok)", "nova-sprint show "+id)
	}
	if h := in.str("head"); h != "" && h != c.Head {
		return "", refuse("--head "+h+" is not "+id+"'s head "+c.Head, "look again: nova-sprint show "+id)
	}
	s := snap.Streams[c.Stream]
	repo, branch := forge.Slug(s.Repo), snap.Cards[id+".w"+strconv.Itoa(c.Attempt)].Branch
	if branch == "" {
		return "", refuse(id+" has no branch recorded", "merge it by hand, then nova-sprint drop or replace it")
	}
	tip, ok, err := e.Origin.Tip(ctx, s.Repo, branch)
	if err != nil {
		return "", broken("git ls-remote of "+s.Repo+" failed ("+clip(err.Error())+")", "land again")
	}
	if !ok || tip != c.Head {
		return "", refuse("head "+c.Head+" is not the tip of "+branch+" at origin", "the worker pushes it: git push origin "+branch)
	}
	pr, ok, err := e.Landing.FindPR(ctx, repo, branch, s.Base)
	if err != nil {
		return "", broken("the forge did not answer ("+clip(err.Error())+")", "land again")
	}
	how := "was merged"
	switch {
	case ok && pr.State == "MERGED" && pr.HeadSHA == c.Head:
	case ok && pr.State == "MERGED":
		return "", refuse(fmt.Sprintf("PR %s was merged at %s, not at %s", pr.URL, pr.HeadSHA, c.Head), "nova-sprint judge")
	default:
		if !ok || pr.State != "OPEN" {
			if _, err := e.Landing.OpenPR(ctx, repo, branch, s.Base, id+": land on "+s.Base, "nova-sprint hand landing of "+id+" at "+c.Head+"."); err != nil {
				return "", refuse("the PR cannot be opened: "+clip(err.Error()), "gh pr create -R "+repo+" --head "+branch+" --base "+s.Base)
			}
			if pr, ok, err = e.Landing.FindPR(ctx, repo, branch, s.Base); err != nil || !ok {
				return "", broken("the PR was opened but cannot be read", "land again")
			}
		}
		if pr.HeadSHA != c.Head {
			return "", refuse(fmt.Sprintf("PR %s is at %s, not %s: the branch moved", pr.URL, pr.HeadSHA, c.Head), "nova-sprint judge")
		}
		if how, err = e.mergePR(ctx, repo, s.Base, pr, c.Head); err != nil {
			return "", err
		}
	}
	r := steps.Request{Card: id, Event: card.Landed{Head: c.Head}, Judgement: judgement}
	if judgement != "" {
		r.Answer = "land"
	}
	line, err := e.move(ctx, in, snap, r, id)
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("PR %s %s", pr.URL, how), line), nil
}

// mergePR merges pr at head the landing's way: a fast-forward to head when
// head contains the base, else the repository's method. A refusal (the base
// moved under it, a ruleset) is said verbatim.
func (e *Engine) mergePR(ctx context.Context, repo, base string, pr forge.PRState, head string) (string, error) {
	behind, err := e.Landing.Behind(ctx, repo, base, head)
	if err != nil {
		return "", broken("the forge did not answer ("+clip(err.Error())+")", "land again")
	}
	if behind == 0 {
		if err := e.Landing.Merge(ctx, repo, pr.Number, head, base); err != nil {
			return "", refuse("the fast-forward of "+base+" was refused: "+clip(err.Error()), "look at the base, then land again")
		}
		return "fast-forwarded " + base + " to " + head, nil
	}
	sha, err := e.Landing.MergePR(ctx, repo, pr.Number, head, forge.MergeMethods)
	if err != nil {
		return "", refuse("the merge was refused: "+clip(err.Error()), "gh pr merge "+strconv.Itoa(pr.Number)+" -R "+repo)
	}
	return "merged as " + sha, nil
}

func (e *Engine) landStream(ctx context.Context, in *input, snap store.Snapshot, name, judgement string) (string, error) {
	s := snap.Streams[name]
	repo, group := forge.Slug(s.Repo), git.StreamBranch(name)
	var lines []string
	pr, ok, err := e.Landing.FindPR(ctx, repo, group, s.Base)
	if err != nil {
		return "", broken("the forge did not answer ("+clip(err.Error())+")", "land again")
	}
	if ok && pr.State == "OPEN" {
		how, err := e.mergePR(ctx, repo, s.Base, pr, pr.HeadSHA)
		if err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("PR %s %s", pr.URL, how))
	}
	base, ok, err := e.Origin.Tip(ctx, s.Repo, s.Base)
	if err != nil || !ok {
		return "", broken("the base "+s.Base+" cannot be read at origin", "land again")
	}
	var landed []string
	for _, id := range slices.Sorted(maps.Keys(snap.Cards)) {
		c := snap.Cards[id]
		if c.Kind != "producer" || c.Stream != name || c.State != "merging" {
			continue
		}
		if in, err := e.Origin.Contains(ctx, s.Repo, c.Head, base); err != nil || !in {
			continue
		}
		landed = append(landed, id)
	}
	if len(landed) == 0 && judgement == "" {
		return "", refuse("no merging producer of "+name+" has its head in "+s.Base+" at "+base, "merge by hand first, then land again")
	}
	for i, id := range landed {
		if snap, err = e.again(ctx, snap, i); err != nil {
			return "", err
		}
		r := steps.Request{Card: id, Event: card.Landed{Head: snap.Cards[id].Head}}
		if i == 0 && judgement != "" {
			r.Judgement, r.Answer = judgement, "land"
		}
		line, err := e.move(ctx, in, snap, r, id)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	if len(landed) == 0 {
		line, err := e.move(ctx, in, snap, steps.Request{Judgement: judgement, Answer: "land"}, name)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	return done(waitNext, lines...), nil
}
