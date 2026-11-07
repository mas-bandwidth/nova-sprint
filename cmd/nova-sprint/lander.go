package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// lander is verbs.Lander: the hand landing and the stream's group branch,
// as the host's gh identity. Its clones are its own (a
// directory apart from run's), since a hand merge checks a commit out.
type lander struct {
	clones *origin
	forge  forge.Forge
}

// fetched is the stream's clone with refs fetched from origin.
func (l *lander) fetched(ctx context.Context, s store.Stream, refs ...string) (string, error) {
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "", err
	}
	dir, err := l.clones.clone(ctx, url)
	if err == nil {
		err = l.clones.git.Fetch(ctx, dir, refs...)
	}
	return dir, err
}

// Heads is every branch at the stream's origin.
func (l *lander) Heads(ctx context.Context, s store.Stream) (map[string]string, error) {
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return nil, err
	}
	return l.clones.git.Heads(ctx, url)
}

// CreateGroup pushes sha to the stream's group branch, leased on its
// absence: a branch made meanwhile is not overwritten.
func (l *lander) CreateGroup(ctx context.Context, s store.Stream, sha string) error {
	dir, err := l.fetched(ctx, s, "refs/heads/"+s.Base)
	if err != nil {
		return err
	}
	return l.clones.git.Push(ctx, dir, sha, git.GroupRef(s.Name), "")
}

// Land merges head (the tip of branch) into the stream's base: base's tip
// when head is on it already; else the PR branch -> base merged by the
// repository's allowed method (opened when there is none), or, with no
// forge, head merged onto base's tip and pushed fast-forward.
func (l *lander) Land(ctx context.Context, s store.Stream, branch, head string) (string, error) {
	dir, err := l.fetched(ctx, s, "refs/heads/"+s.Base, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	g := l.clones.git
	tip, ok, err := g.LsRemote(ctx, dir, "refs/heads/"+s.Base)
	if err == nil && !ok {
		err = fmt.Errorf("base branch %s is not on origin", s.Base)
	}
	if err != nil {
		return "", err
	}
	if on, err := g.IsAncestor(ctx, dir, head, tip); err != nil || on {
		return tip, err
	}
	if s.Forge == "" {
		return l.pushBase_internal(ctx, dir, s, tip, head)
	}
	return l.mergePR_internal(ctx, s, branch, head)
}

func (l *lander) mergePR_internal(ctx context.Context, s store.Stream, branch, head string) (string, error) {
	if l.forge == nil {
		return "", &verbs.OperationError{Problem: "stream " + s.Name + " names forge " + s.Forge + " but this process has no forge", Fix: "run the land from a host whose gh is logged in", Exit: 2}
	}
	pr, ok, err := l.forge.PR(ctx, s.Repo, branch, s.Base)
	if err == nil && !ok {
		pr, err = l.forge.OpenPR(ctx, s.Repo, branch, s.Base, s.Name+": land "+branch+" by hand", "Landed by hand with nova-sprint land.")
	}
	if err != nil {
		return "", fmt.Errorf("the PR %s -> %s could not be opened or read: %w", branch, s.Base, err)
	}
	if pr.HeadSHA != "" && !strings.EqualFold(pr.HeadSHA, head) {
		return "", &verbs.OperationError{Problem: fmt.Sprintf("PR %s's head is %s, not the card's head %s", pr.URL, pr.HeadSHA, head), Fix: "send the card to fix (its branch moved after the read), or push its branch back to " + head, Exit: 1}
	}
	return l.forge.Merge(ctx, s.Repo, pr.Number, head)
}

func (l *lander) pushBase_internal(ctx context.Context, dir string, s store.Stream, tip, head string) (string, error) {
	g := l.clones.git
	merged, err := g.MergeGroup(ctx, dir, tip, []string{head}, nil)
	if err != nil {
		return "", err
	}
	if merged.Kind != git.MergeOK {
		return "", &verbs.OperationError{Problem: fmt.Sprintf("head %s does not merge onto %s at %s: %s %s", head, s.Base, tip, merged.Reason, strings.Join(merged.Conflicts, ", ")), Fix: "merge it into " + s.Base + " yourself and push, then land it again (a head on base records at once)", Exit: 1}
	}
	if err := g.Push(ctx, dir, merged.SHA, "refs/heads/"+s.Base, tip); err != nil {
		return "", err
	}
	return merged.SHA, nil
}
