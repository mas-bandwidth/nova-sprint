package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// lander is verbs.Lander: the hand landing and the stream's group branch,
// as the host's gh identity (intent rule 32). Its clones are its own (a
// directory apart from run's), since a hand merge checks a commit out.
type lander struct {
	clones *origin
	forge  forge.Forge
}

// locked is the stream's clone under its lock with refs fetched
// (origin.locked); the caller closes the lock after its last write.
func (l *lander) locked(ctx context.Context, s store.Stream, refs ...string) (string, *os.File, error) {
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "", nil, err
	}
	return l.clones.locked(ctx, url, refs...)
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
	dir, lock, err := l.locked(ctx, s, "refs/heads/"+s.Base)
	if err != nil {
		return err
	}
	return errors.Join(l.clones.git.Push(ctx, dir, sha, git.GroupRef(s.Name), ""), lock.Close())
}

// OnMain is whether sha is on the default branch at the stream's origin
// (git.Client.Main), read in the clone after a fetch of origin's refs.
func (l *lander) OnMain(ctx context.Context, s store.Stream, sha string) (bool, error) {
	dir, lock, err := l.locked(ctx, s)
	if err != nil {
		return false, err
	}
	_, tip, err := l.clones.git.Main(ctx, dir)
	on := false
	if err == nil {
		on, err = l.clones.git.IsAncestor(ctx, dir, sha, tip)
	}
	return on, errors.Join(err, lock.Close())
}

// Land merges head (the tip of branch) into the stream's base: base's tip
// when head is on it already; else the PR branch -> base merged by the
// repository's allowed method (opened when there is none), or, with no
// forge, head merged onto base's tip and pushed fast-forward.
// One lock on the clone is held from the fetch through the push (a merge
// with no forge checks out and merges in the clone's work tree).
func (l *lander) Land(ctx context.Context, s store.Stream, branch, head string) (string, error) {
	dir, lock, err := l.locked(ctx, s, "refs/heads/"+s.Base, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	sha, err := l.land_internal(ctx, dir, s, branch, head)
	return landResult(os.Stderr, sha, err, lock.Close())
}

// landResult is a landing's outcome: a merge that succeeded is landed at
// sha whatever the lock's release says (that error is written to w apart);
// a merge that failed carries both errors.
func landResult(w io.Writer, sha string, err, closeErr error) (string, error) {
	if err == nil && closeErr != nil {
		fmt.Fprintf(w, "nova-sprint: landed %s; the clone's lock did not release: %s\n", sha, edge.RedactN(closeErr.Error(), edge.CommandMax))
		return sha, nil
	}
	return sha, errors.Join(err, closeErr)
}

func (l *lander) land_internal(ctx context.Context, dir string, s store.Stream, branch, head string) (string, error) {
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
	merged, err := g.MergeGroup(ctx, dir, tip, []string{head})
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
