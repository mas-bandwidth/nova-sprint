package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// headReady is finish's check of the head at origin (CORRECT-USE, "Finish
// is checked, not trusted"): a full sha, not the base, and
// the tip of the dealt branch at origin, every commit since the base made as
// the sprint's commit identity. That is all finish needs (intent
// rule 32): no PR, no forge and no token on the worker; the landing opens
// the PR and reads its CI on the run's host.
func (e *Engine) headReady(ctx context.Context, snap store.Snapshot, c store.Card, head string) error {
	if !fullSHA(head) {
		return refuse("head "+head+" is not a full commit sha", "pass the 40-hex sha git ls-remote origin "+c.Branch+" prints")
	}
	if head == c.BaseSHA {
		return refuse("head "+head+" is where this attempt started; nothing was done", "commit the work on "+c.Branch+", push it, and finish with its head")
	}
	s, url, tip, ok, err := e.branchTip(ctx, snap, c, "then finish again")
	if err != nil {
		return err
	}
	if !ok || tip != head {
		return refuse("head "+head+" is not the tip of "+c.Branch+" at origin", "push first: git push origin "+c.Branch)
	}
	return e.madeAsIdentity(ctx, snap, s, url, c, head)
}

// pushedHead is a failed work report's head when it names none: the dealt
// branch's tip at origin, so the card goes back with the pushed work and the
// next attempt continues from it. "" when the branch is absent or its tip is
// the base or the prior head (nothing new was pushed).
func (e *Engine) pushedHead(ctx context.Context, snap store.Snapshot, c store.Card) (string, error) {
	_, _, tip, ok, err := e.branchTip(ctx, snap, c, "or name the pushed head with --head <sha>")
	if err != nil || !ok || tip == c.BaseSHA || tip == c.Prior {
		return "", err
	}
	return tip, nil
}

// branchTip is c's stream, its repository's URL and the dealt branch's tip
// at origin (ok false: no such branch). A failed read names then as its
// remedy's end.
func (e *Engine) branchTip(ctx context.Context, snap store.Snapshot, c store.Card, then string) (store.Stream, string, string, bool, error) {
	s := snap.Streams[c.Stream]
	if e.Origin == nil {
		return s, "", "", false, broken("the git edge is not configured", "nova-sprint check")
	}
	url, err := streamURL(s)
	if err != nil {
		return s, "", "", false, err
	}
	tip, ok, err := e.Origin.Tip(ctx, url, c.Branch)
	if err != nil {
		return s, url, "", false, RefusalOr(err, broken("git ls-remote of "+s.Repo+" failed ("+errText(err)+")", "check the repository is readable, "+then))
	}
	return s, url, tip, ok, nil
}

// madeAsIdentity is finish's admission of the head's commits (round 24):
// there is at least one commit on head that is neither in the attempt's
// base nor on the stream's base at origin (none is nothing done), and every
// such commit is authored and committed as the sprint's
// identity, by email (case folded), read in a clone through the git edge.
// The refusal names each commit made as anyone else and its emails; the fix
// is the two git config lines and a rewrite of those commits.
func (e *Engine) madeAsIdentity(ctx context.Context, snap store.Snapshot, s store.Stream, url string, c store.Card, head string) error {
	if !snap.Identity.Set() {
		return noIdentity()
	}
	commits, err := e.Origin.Commits(ctx, url, head, s.Base, c.BaseSHA)
	if err != nil {
		return RefusalOr(err, broken("the commits of "+head+" could not be read in a clone of "+s.Repo+" ("+errText(err)+")", "check the repository is readable, then finish again"))
	}
	if len(commits) == 0 {
		return refuse("head "+head+" has no commit beyond "+orWord(c.BaseSHA, "the base")+" and origin/"+s.Base+"; nothing was done", "commit the work on "+c.Branch+", push it, and finish with its head")
	}
	want := snap.Identity.Email
	var bad []string
	for _, k := range commits {
		if !strings.EqualFold(k.AuthorEmail, want) || !strings.EqualFold(k.CommitterEmail, want) {
			bad = append(bad, fmt.Sprintf("%s (author %s, committer %s)", short(k.SHA), orWord(k.AuthorEmail, "none"), orWord(k.CommitterEmail, "none")))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	n := len(bad)
	if n > 10 {
		bad = append(bad[:10], fmt.Sprintf("and %d more", n-10))
	}
	since := orWord(c.BaseSHA, "origin/"+s.Base)
	return refuse(fmt.Sprintf("%d of the %d commits on %s since the base are not made as %s: %s", n, len(commits), c.Branch, commitIdentity(snap), strings.Join(bad, ", ")),
		"in the clone run "+strings.ReplaceAll(worker.CommitConfig(snap.Identity.Name, snap.Identity.Email), "\n", " and ")+
			", then rewrite those commits as it (git rebase --rebase-merges "+since+" --exec 'git commit --amend --no-edit --reset-author'), git push --force-with-lease origin "+c.Branch+", and finish with the new head")
}

// short is a sha's first twelve characters.
func short(sha string) string { return sha[:min(len(sha), 12)] }

// streamURL is the stream's repository as the git edge reaches it
// (git.CloneURL); a repository it cannot address is a refusal naming the
// form.
func streamURL(s store.Stream) (string, error) {
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "", RefusalOr(err, refuse("stream "+s.Name+": "+err.Error(), "nova-sprint check"))
	}
	return url, nil
}

func fullSHA(s string) bool {
	return len(s) == 40 && strings.Trim(strings.ToLower(s), "0123456789abcdef") == ""
}
