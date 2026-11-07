package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// Tree is every file at sha in the address's clone under the state dir
// (fetched first, so a base newer than the clone is there).
func (o *origin) Tree(ctx context.Context, url, sha string) ([]string, error) {
	dir, err := o.clone(ctx, url)
	if err == nil {
		err = o.git.Fetch(ctx, dir)
	}
	if err != nil {
		return nil, err
	}
	return o.git.ListTree(ctx, dir, sha)
}

// Commits is every commit on head not on base at origin nor reachable from
// since, in the address's clone under the state dir, fetched first (the
// head and the base are there): one git log.
func (o *origin) Commits(ctx context.Context, url, head, base, since string) ([]git.Commit, error) {
	dir, err := o.clone(ctx, url)
	if err == nil {
		err = o.git.Fetch(ctx, dir)
	}
	if err != nil {
		return nil, err
	}
	return o.git.Commits(ctx, dir, head, "refs/remotes/origin/"+base, since)
}

// Branches is the branches under prefix at the address: one ls-remote.
func (o *origin) Branches(ctx context.Context, url, prefix string) (map[string]string, error) {
	return o.git.Branches(ctx, url, prefix)
}

// hostForge is GitHub as this host's own gh identity (the identity the run
// does the forge work with), as admission reads it: two cheap reads.
type hostForge struct{ exec edge.Exec }

// actingPermissions are the repository permissions that may push branches
// and open and merge pull requests.
var actingPermissions = []string{"ADMIN", "MAINTAIN", "WRITE"}

func (f hostForge) gh(ctx context.Context, args ...string) (string, error) {
	argv := append([]string{"gh"}, args...)
	// As the run's forge: the host's login, never a credential in the environment.
	res, err := f.exec.Run(ctx, edge.Cmd{Argv: argv, Env: append(forge.HostEnv(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1"), CleanEnv: true, Timeout: 30 * time.Second})
	if err != nil || !res.OK() {
		return "", edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	}
	return res.Stdout, nil
}

// CanAct reads the identity's permission on repo: gh repo view fails for an
// identity that is not logged in or cannot see the repository, and a read
// or triage permission cannot push, open or merge.
func (f hostForge) CanAct(ctx context.Context, repo string) error {
	out, err := f.gh(ctx, "repo", "view", repo, "--json", "viewerPermission", "--jq", ".viewerPermission")
	if err != nil {
		return verbs.RefusalOr(err, &verbs.OperationError{Problem: edge.RedactN("this host's gh cannot read "+repo+": "+err.Error(), edge.CommandMax),
			Fix: "gh auth login on this host as an identity with write access to " + repo + " (gh auth status shows who it is), then add the stream again", Exit: 1})
	}
	perm := strings.TrimSpace(out)
	for _, p := range actingPermissions {
		if perm == p {
			return nil
		}
	}
	return &verbs.OperationError{Problem: "this host's gh identity has " + orNone(perm) + " permission on " + repo + ", and the landing needs write to push, open and merge",
		Fix: "give the identity gh auth status names write access to " + repo + ", then add the stream again", Exit: 1}
}

// PR is pull request n of repo: its head and whether it merged.
func (f hostForge) PR(ctx context.Context, repo string, n int) (string, bool, error) {
	out, err := f.gh(ctx, "pr", "view", strconv.Itoa(n), "-R", repo, "--json", "headRefOid,state")
	if err != nil {
		return "", false, err
	}
	var v struct{ HeadRefOid, State string }
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", false, fmt.Errorf("gh pr view %d -R %s: %s", n, repo, edge.ParseFailure(err, []byte(out)))
	}
	return strings.ToLower(v.HeadRefOid), v.State == "MERGED", nil
}

// RequiredChecks are the status checks the base branch of repo requires, from
// the two places a repository sets them: the branch's protection and the
// rulesets that apply to it. Both are readable with read access; either
// failing is an error (a half read would hide a requirement).
func (f hostForge) RequiredChecks(ctx context.Context, repo, branch string) ([]string, error) {
	prot, err := f.gh(ctx, "api", "repos/"+repo+"/branches/"+branch, "--jq", ".protection.required_status_checks.contexts // [] | .[]")
	if err != nil {
		return nil, fmt.Errorf("the protection of %s's %s: %w", repo, branch, err)
	}
	rules, err := f.gh(ctx, "api", "repos/"+repo+"/rules/branches/"+branch, "--jq", `.[] | select(.type == "required_status_checks") | .parameters.required_status_checks[].context`)
	if err != nil {
		return nil, fmt.Errorf("the rulesets of %s's %s: %w", repo, branch, err)
	}
	var out []string
	for _, line := range strings.Split(prot+"\n"+rules, "\n") {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(out, line) {
			out = append(out, line)
		}
	}
	return out, nil
}

func orNone(s string) string {
	if s == "" {
		return "no"
	}
	return s
}
