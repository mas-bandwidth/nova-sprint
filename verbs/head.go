package verbs

import (
	"context"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// headReady is finish's check of the head at origin (CORRECT-USE, "Finish
// is checked, not trusted"): a full sha, not the base or the prior head, the
// tip of the dealt branch at origin, containing the stream's group head,
// and with the PR when the stream lands through a forge.
func (e *Engine) headReady(ctx context.Context, snap store.Snapshot, c store.Card, head string) error {
	if !fullSHA(head) {
		return refuse("head "+head+" is not a full commit sha", "pass the 40-hex sha git ls-remote origin "+c.Branch+" prints")
	}
	if head == c.BaseSHA || head == c.Prior {
		return refuse("head "+head+" is where this attempt started; nothing was done", "commit the work on "+c.Branch+", push it, and finish with its head")
	}
	if e.Origin == nil {
		return broken("the git edge is not configured", "nova-sprint check")
	}
	s := snap.Streams[c.Stream]
	tip, ok, err := e.Origin.Tip(ctx, s.Repo, c.Branch)
	if err != nil {
		return RefusalOr(err, broken("git ls-remote of "+s.Repo+" failed ("+errText(err)+")", "check the repository is readable, then finish again"))
	}
	if !ok || tip != head {
		return refuse("head "+head+" is not the tip of "+c.Branch+" at origin", "push first: git push origin "+c.Branch)
	}
	if err := e.upToGroup(ctx, s, c, head); err != nil {
		return err
	}
	return e.prReady(ctx, s, c, head)
}

// upToGroup refuses a head that does not contain the stream's group head.
func (e *Engine) upToGroup(ctx context.Context, s store.Stream, c store.Card, head string) error {
	group := git.StreamBranch(s.Name)
	tip, ok, err := e.Origin.Tip(ctx, s.Repo, group)
	if err != nil {
		return RefusalOr(err, broken("git ls-remote of "+s.Repo+" failed ("+errText(err)+")", "finish again"))
	}
	if !ok {
		return nil
	}
	in, err := e.Origin.Contains(ctx, s.Repo, tip, head)
	if err != nil {
		return RefusalOr(err, broken("git merge-base of "+s.Repo+" failed ("+errText(err)+")", "finish again"))
	}
	if !in {
		return refuse("bring "+c.Branch+" up to the stream head", "git fetch origin "+group+", git rebase origin/"+group+", run the gates, git push --force-with-lease origin "+c.Branch)
	}
	return nil
}

// prReady refuses a head with no PR at it when the stream has a forge.
func (e *Engine) prReady(ctx context.Context, s store.Stream, c store.Card, head string) error {
	if s.Forge == "" {
		return nil
	}
	group := git.StreamBranch(s.Name)
	at, ok, err := e.Origin.PR(ctx, s.Repo, c.Branch, group)
	if err != nil {
		return RefusalOr(err, broken("the forge did not answer ("+errText(err)+")", "finish again"))
	}
	if !ok {
		return refuse("the stream wants a PR and "+c.Branch+" has none", "open the PR: gh pr create --base "+group+" --head "+c.Branch)
	}
	if at != head {
		return refuse("the PR of "+c.Branch+" is at "+at+", not "+head, "push first: git push origin "+c.Branch)
	}
	return nil
}

func fullSHA(s string) bool {
	return len(s) == 40 && strings.Trim(strings.ToLower(s), "0123456789abcdef") == ""
}
