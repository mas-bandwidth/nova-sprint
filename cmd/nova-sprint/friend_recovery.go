package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

type recoveryTipsFn func(context.Context, string, []string) (map[string]string, error)

// recoveryBranchTips reads only the exact requested refs, in one bounded
// ls-remote per repository. It neither changes nor deletes the old branches.
func (a *app) recoveryBranchTips(ctx context.Context, repo string, branches []string) (map[string]string, error) {
	args := []string{"ls-remote", "--heads", "--", repo}
	wanted := map[string]bool{}
	for _, branch := range branches {
		ref := "refs/heads/" + branch
		args = append(args, ref)
		wanted[ref] = true
	}
	out, err := gitrun.Output(ctx, gitrun.Options{Env: a.gitEnv, Timeout: sprint.RecoveryHeadRemoteBudget}, args...)
	if err != nil {
		return nil, err
	}
	heads := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || !wanted[f[1]] {
			return nil, fmt.Errorf("unexpected branch-head response")
		}
		branch := strings.TrimPrefix(f[1], "refs/heads/")
		if _, duplicate := heads[branch]; duplicate {
			return nil, fmt.Errorf("duplicate branch-head response")
		}
		heads[branch] = f[0]
	}
	return heads, nil
}

// recoveryHeads echoes exact request identities. A failed query is evidence
// of failure for every requested branch, never a successful absent branch.
// The caller supplies one absolute deadline across engine retries.
func (a *app) recoveryHeads(ctx context.Context, requests []sprint.RecoveryHeadRequest) []sprint.RecoveryHead {
	if len(requests) > sprint.RecoveryHeadBatch {
		requests = requests[:sprint.RecoveryHeadBatch]
	}
	byRepo := map[string][]sprint.RecoveryHeadRequest{}
	for _, request := range requests {
		byRepo[request.Repo] = append(byRepo[request.Repo], request)
	}
	var repos []string
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	var out []sprint.RecoveryHead
	for _, repo := range repos {
		// Each repository is bounded on its own, from the caller's context:
		// one dead remote spends at most RecoveryHeadRemoteBudget and the
		// later repositories are still read.
		repoCtx, repoCancel := context.WithTimeout(ctx, sprint.RecoveryHeadRemoteBudget)
		group := byRepo[repo]
		var branches []string
		for _, request := range group {
			branches = append(branches, request.Branch)
		}
		var heads map[string]string
		var err error
		switch {
		case repo == "":
			err = fmt.Errorf("the revoked generation names no repository")
		case repoCtx.Err() != nil:
			err = repoCtx.Err()
		case a.recoveryTips == nil:
			err = fmt.Errorf("the branch-head reader is unavailable")
		default:
			heads, err = a.recoveryTips(repoCtx, repo, branches)
		}
		repoCancel()
		for _, request := range group {
			result := sprint.RecoveryHead{RecoveryHeadRequest: request}
			if err != nil {
				result.Error = oneline.Escape(err.Error())
			} else {
				result.Head = heads[request.Branch]
			}
			out = append(out, result)
		}
	}
	return out
}
