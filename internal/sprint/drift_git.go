package sprint

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
)

// DriftGitReq is a read of the repository's drift: the clone it reads in, the remote, the
// base, and the branches to measure against it (BranchesNamed).
type DriftGitReq struct {
	RepoDir  string
	Env      []string // the git children's environment; nil inherits
	Remote   string   // "origin" when ""
	Repo     string   // owner/name, carried into the facts
	Base     string
	Branches []BranchLag
}

// ReadDrift reads the repository's half of the drift facts in the clone: the base and
// each branch fetched from the remote, the base's tip, and each branch's commits ahead of
// the base and behind it; a branch the remote does not have is Missing. The forge's half
// (the promotion PR, the whole-tree gate at the tip) is the caller's to add before
// DriftRead records them. It runs outside the plan: git runs once, a retry runs it again.
func ReadDrift(ctx context.Context, req DriftGitReq) (*DriftFacts, error) {
	if req.Base == "" {
		return nil, fmt.Errorf("drift read: no base branch")
	}
	if req.Remote == "" {
		req.Remote = "origin"
	}
	git := func(args ...string) (string, error) {
		res, err := gitrun.Run(ctx, gitrun.Options{C: req.RepoDir, Env: req.Env, OwnRepo: true}, args...)
		if err != nil {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(res.Stderr)+"\n"+string(res.Stdout)))
		}
		return strings.TrimSpace(string(res.Stdout)), nil
	}
	ref := func(b string) string { return "refs/remotes/" + req.Remote + "/" + b }
	if _, err := git("fetch", "-q", "--prune", req.Remote, "+refs/heads/*:refs/remotes/"+req.Remote+"/*"); err != nil {
		return nil, fmt.Errorf("drift read: %w", err)
	}
	f := &DriftFacts{Repo: req.Repo, Base: req.Base}
	var err error
	if f.BaseTip, err = git("rev-parse", "--verify", "-q", ref(req.Base)); err != nil {
		return nil, fmt.Errorf("drift read: the base %s: %w", req.Base, err)
	}
	for _, b := range req.Branches {
		lag := BranchLag{Branch: b.Branch, Named: b.Named}
		if _, err := git("rev-parse", "--verify", "-q", ref(b.Branch)); err != nil {
			lag.Missing = true
			f.Branches = append(f.Branches, lag)
			continue
		}
		out, err := git("rev-list", "--left-right", "--count", ref(b.Branch)+"..."+ref(req.Base))
		if err != nil {
			return nil, fmt.Errorf("drift read: %s: %w", b.Branch, err)
		}
		parts := strings.Fields(out)
		if len(parts) != 2 {
			return nil, fmt.Errorf("drift read: %s: rev-list said %q", b.Branch, out)
		}
		if lag.Ahead, err = strconv.Atoi(parts[0]); err != nil {
			return nil, fmt.Errorf("drift read: %s: %w", b.Branch, err)
		}
		if lag.Behind, err = strconv.Atoi(parts[1]); err != nil {
			return nil, fmt.Errorf("drift read: %s: %w", b.Branch, err)
		}
		f.Branches = append(f.Branches, lag)
	}
	return f, nil
}
