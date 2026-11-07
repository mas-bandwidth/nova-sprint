// Package git is the git and mechanical landing edge of layer 4.
package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	DefaultRebaseTimeout        = 2 * time.Minute
	DefaultPushTimeout          = 60 * time.Second
	DefaultSSHConnectTimeoutSec = 10
)

var (
	ErrMissingRef    = errors.New("git: ref not on origin")
	ErrPushRefused   = errors.New("git: push refused")
	ErrNoLandingHost = errors.New("git: no landing host answered")
)

type PushRefusedError struct{ Ref, SHA, Reason string }

func (e *PushRefusedError) Error() string {
	return fmt.Sprintf("git refused: push %s to %s rejected: %s", e.SHA, e.Ref, e.Reason)
}
func (e *PushRefusedError) Unwrap() error { return ErrPushRefused }

type ExecFunc func(ctx context.Context, cmd edge.Cmd) (edge.Result, error)

func (f ExecFunc) Run(ctx context.Context, cmd edge.Cmd) (edge.Result, error) { return f(ctx, cmd) }

type LedgerRule struct {
	Path   string
	Script []string
}
type MergeKind string

const (
	MergeOK           MergeKind = "ok"
	MergeConflict     MergeKind = "conflict"
	MergeLedgerFailed MergeKind = "ledger_failed"
)

type MergeResult struct {
	Kind              MergeKind
	SHA, Tree, Reason string
	ReusedHead        bool
	Conflicts         []string
}
type RebaseResult struct {
	OK          bool
	SHA, Reason string
	Conflicts   []string
}
type Git interface {
	Fetch(ctx context.Context, repoDir string, refs ...string) error
	LsRemote(ctx context.Context, repoDir, ref string) (sha string, ok bool, err error)
	IsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error)
	TreeHash(ctx context.Context, repoDir, rev string) (string, error)
	MergeGroup(ctx context.Context, repoDir, onto string, heads []string, ledgers []LedgerRule) (MergeResult, error)
	Rebase(ctx context.Context, worktree, onto string) (RebaseResult, error)
	Push(ctx context.Context, repoDir, sha, ref, expectOld string) error
}
type Config struct {
	Binary, AuthorName, AuthorEmail string
	Exec                            edge.Exec
	PushTimeout, RebaseTimeout      time.Duration
}
type Client struct{ cfg Config }

func New(cfg Config) *Client {
	cfg.Binary = cmp.Or(cfg.Binary, "git")
	cfg.Exec = cmp.Or[edge.Exec](cfg.Exec, edge.NewOSExec())
	cfg.PushTimeout = cmp.Or(cfg.PushTimeout, DefaultPushTimeout)
	cfg.RebaseTimeout = cmp.Or(cfg.RebaseTimeout, DefaultRebaseTimeout)
	cfg.AuthorName = cmp.Or(cfg.AuthorName, "nova-sprint")
	cfg.AuthorEmail = cmp.Or(cfg.AuthorEmail, "sprint@mas-bandwidth")
	return &Client{cfg: cfg}
}
func (c *Client) gitEnv_internal(extra ...string) []string {
	return append([]string{"GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=" + c.cfg.AuthorName, "GIT_AUTHOR_EMAIL=" + c.cfg.AuthorEmail, "GIT_COMMITTER_NAME=" + c.cfg.AuthorName, "GIT_COMMITTER_EMAIL=" + c.cfg.AuthorEmail}, extra...)
}
func (c *Client) runGitEnv_internal(ctx context.Context, dir string, timeout time.Duration, extraEnv []string, args ...string) (string, string, int, error) {
	res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: append([]string{c.cfg.Binary}, args...), Dir: dir, Env: c.gitEnv_internal(extraEnv...), Timeout: timeout})
	return res.Stdout, res.Stderr, res.ExitCode, err
}
func (c *Client) runGit_internal(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, string, int, error) {
	return c.runGitEnv_internal(ctx, dir, timeout, nil, args...)
}
func (c *Client) Fetch(ctx context.Context, repoDir string, refs ...string) error {
	args := []string{"fetch", "--no-tags", "origin"}
	for _, r := range refs {
		if strings.TrimSpace(r) != "" {
			args = append(args, r)
		}
	}
	_, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%w: fetch %s failed: %s", ErrMissingRef, strings.Join(refs, " "), strings.TrimSpace(stderr))
	}
	return nil
}
func (c *Client) LsRemote(ctx context.Context, repoDir, ref string) (string, bool, error) {
	if isFullSHA_internal(ref) {
		return c.lsRemoteSHA_internal(ctx, repoDir, strings.ToLower(ref))
	}
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, "ls-remote", "--exit-code", "origin", ref)
	switch {
	case err != nil:
		return "", false, err
	case code == 2:
		return "", false, nil
	case code != 0:
		return "", false, fmt.Errorf("git ls-remote origin %s exited %d: %s", ref, code, strings.TrimSpace(stderr))
	}
	if fields := strings.Fields(stdout); len(fields) > 0 && isFullSHA_internal(fields[0]) {
		return strings.ToLower(fields[0]), true, nil
	}
	return "", false, nil
}
func (c *Client) lsRemoteSHA_internal(ctx context.Context, repoDir, target string) (string, bool, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, "ls-remote", "origin")
	if err != nil {
		return "", false, err
	}
	if code != 0 {
		return "", false, fmt.Errorf("git ls-remote origin exited %d: %s", code, strings.TrimSpace(stderr))
	}
	var remoteSHAs []string
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.Fields(line); len(fields) >= 1 && isFullSHA_internal(fields[0]) {
			s := strings.ToLower(fields[0])
			if s == target {
				if err := c.Fetch(ctx, repoDir, target); err != nil {
					return "", false, nil
				}
				return target, true, nil
			}
			remoteSHAs = append(remoteSHAs, s)
		}
	}
	for _, rsha := range remoteSHAs {
		_ = c.Fetch(ctx, repoDir, rsha)
		if ok, err := c.IsAncestor(ctx, repoDir, target, rsha); err == nil && ok {
			return target, true, nil
		}
	}
	return "", false, nil
}
func (c *Client) IsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error) {
	_, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "merge-base", "--is-ancestor", ancestor, descendant)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return true, nil
	case code == 1:
		return false, nil
	default:
		return false, fmt.Errorf("git merge-base --is-ancestor %s %s exited %d: %s", ancestor, descendant, code, strings.TrimSpace(stderr))
	}
}
func (c *Client) TreeHash(ctx context.Context, repoDir, rev string) (string, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "rev-parse", rev+"^{tree}")
	if err != nil || code != 0 {
		return "", cmp.Or(err, fmt.Errorf("git rev-parse %s^{tree} exited %d: %s", rev, code, strings.TrimSpace(stderr)))
	}
	return strings.TrimSpace(stdout), nil
}
func (c *Client) MergeGroup(ctx context.Context, repoDir, onto string, heads []string, ledgers []LedgerRule) (MergeResult, error) {
	if len(heads) == 0 {
		return MergeResult{}, fmt.Errorf("git refused: MergeGroup requires at least one head")
	}
	if len(heads) == 1 {
		if ok, err := c.IsAncestor(ctx, repoDir, onto, heads[0]); err == nil && ok {
			if _, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "checkout", "--detach", heads[0]); err != nil || code != 0 {
				return MergeResult{}, fmt.Errorf("git checkout %s failed: %v %s", heads[0], err, strings.TrimSpace(stderr))
			}
			tree, _ := c.TreeHash(ctx, repoDir, heads[0])
			return MergeResult{Kind: MergeOK, SHA: heads[0], Tree: tree, ReusedHead: true}, nil
		}
	}
	if _, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "checkout", "--detach", onto); err != nil || code != 0 {
		return MergeResult{}, fmt.Errorf("git checkout %s failed: %v %s", onto, err, strings.TrimSpace(stderr))
	}
	baseDateRaw, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "show", "-s", "--format=%cI", onto)
	if err != nil || code != 0 {
		return MergeResult{}, fmt.Errorf("git show %s committer date failed: %v %s", onto, err, strings.TrimSpace(stderr))
	}
	baseDate := strings.TrimSpace(baseDateRaw)
	dateEnv := []string{"GIT_AUTHOR_DATE=" + baseDate, "GIT_COMMITTER_DATE=" + baseDate}
	commitMsg := "nova-sprint merge " + strings.Join(heads, " ")
	ledgerMap := make(map[string]LedgerRule, len(ledgers))
	for _, l := range ledgers {
		ledgerMap[l.Path] = l
	}
	for _, head := range heads {
		if res, done, err := c.mergeOneHead_internal(ctx, repoDir, onto, head, ledgerMap, dateEnv, commitMsg); err != nil || done {
			return res, err
		}
	}
	shaOut, _, _, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "rev-parse", "HEAD")
	if err != nil {
		return MergeResult{}, err
	}
	sha := strings.TrimSpace(shaOut)
	tree, _ := c.TreeHash(ctx, repoDir, sha)
	return MergeResult{Kind: MergeOK, SHA: sha, Tree: tree}, nil
}
func (c *Client) mergeOneHead_internal(ctx context.Context, repoDir, onto, head string, ledgerMap map[string]LedgerRule, dateEnv []string, commitMsg string) (MergeResult, bool, error) {
	_, _, code, err := c.runGitEnv_internal(ctx, repoDir, c.cfg.RebaseTimeout, dateEnv, "merge", "--no-ff", "-m", commitMsg, head)
	if err != nil || code == 0 {
		return MergeResult{}, err != nil, err
	}
	conflicts := c.unmergedFiles_internal(ctx, repoDir)
	var nonLedger, ledgerFiles []string
	for _, f := range conflicts {
		if _, ok := ledgerMap[f]; ok {
			ledgerFiles = append(ledgerFiles, f)
		} else {
			nonLedger = append(nonLedger, f)
		}
	}
	if len(nonLedger) > 0 || len(ledgerFiles) == 0 {
		_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "merge", "--abort")
		_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "reset", "--hard", onto)
		if len(nonLedger) == 0 {
			nonLedger = conflicts
		}
		return MergeResult{Kind: MergeConflict, Conflicts: nonLedger, Reason: fmt.Sprintf("merge conflict on %s: %s", head, strings.Join(nonLedger, ", "))}, true, nil
	}
	if err := c.resolveLedgers_internal(ctx, repoDir, ledgerFiles, ledgerMap, dateEnv, commitMsg); err != nil {
		_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "merge", "--abort")
		_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "reset", "--hard", onto)
		return MergeResult{Kind: MergeLedgerFailed, Conflicts: ledgerFiles, Reason: err.Error()}, true, nil
	}
	return MergeResult{}, false, nil
}
func (c *Client) resolveLedgers_internal(ctx context.Context, repoDir string, files []string, ledgerMap map[string]LedgerRule, dateEnv []string, commitMsg string) error {
	for _, f := range files {
		if rule := ledgerMap[f]; len(rule.Script) > 0 {
			_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "checkout", "--ours", "--", f)
			if res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: rule.Script, Dir: repoDir, Env: c.gitEnv_internal(), Timeout: c.cfg.RebaseTimeout}); err != nil || !res.OK() {
				return fmt.Errorf("ledger script %s failed on %s: %v %s", strings.Join(rule.Script, " "), f, err, strings.TrimSpace(res.Stderr))
			}
		} else if err := c.mergeLedgerLines_internal(ctx, repoDir, f); err != nil {
			return err
		}
		if _, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "add", "--", f); err != nil || code != 0 {
			return fmt.Errorf("git add %s failed: %v %s", f, err, strings.TrimSpace(stderr))
		}
	}
	if _, stderr, code, err := c.runGitEnv_internal(ctx, repoDir, c.cfg.RebaseTimeout, dateEnv, "commit", "-m", commitMsg); err != nil || code != 0 {
		return fmt.Errorf("git commit ledger merge failed: %v %s", err, strings.TrimSpace(stderr))
	}
	return nil
}
func (c *Client) mergeLedgerLines_internal(ctx context.Context, repoDir, relPath string) error {
	ours, _, _, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "show", ":2:"+relPath)
	if err != nil {
		return err
	}
	theirs, _, _, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "show", ":3:"+relPath)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repoDir, relPath), []byte(unionLines_internal(ours, theirs)), 0o644)
}
func unionLines_internal(ours, theirs string) string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range []string{ours, theirs} {
		for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			if line != "" && !seen[line] {
				seen[line], out = true, append(out, line)
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}
func (c *Client) unmergedFiles_internal(ctx context.Context, dir string) []string {
	stdout, _, _, _ := c.runGit_internal(ctx, dir, c.cfg.RebaseTimeout, "diff", "--name-only", "--diff-filter=U")
	var files []string
	for _, line := range strings.Split(stdout, "\n") {
		if f := strings.TrimSpace(line); f != "" {
			files = append(files, f)
		}
	}
	slices.Sort(files)
	return slices.Compact(files)
}
func (c *Client) Rebase(ctx context.Context, worktree, onto string) (RebaseResult, error) {
	_, stderr, code, err := c.runGit_internal(ctx, worktree, c.cfg.RebaseTimeout, "rebase", onto)
	if err != nil {
		return RebaseResult{}, err
	}
	if code == 0 {
		shaOut, _, _, err := c.runGit_internal(ctx, worktree, c.cfg.RebaseTimeout, "rev-parse", "HEAD")
		return RebaseResult{OK: err == nil, SHA: strings.TrimSpace(shaOut)}, err
	}
	conflicts := c.unmergedFiles_internal(ctx, worktree)
	_, _, _, _ = c.runGit_internal(ctx, worktree, c.cfg.RebaseTimeout, "rebase", "--abort")
	reason := strings.TrimSpace(stderr)
	if len(conflicts) > 0 {
		reason = fmt.Sprintf("rebase conflict onto %s: %s", onto, strings.Join(conflicts, ", "))
	}
	return RebaseResult{OK: false, Conflicts: conflicts, Reason: reason}, nil
}
func StreamBranch(stream string) string { return "sprint/" + strings.TrimSpace(stream) + "/group" }
func StreamRef(stream string) string    { return "refs/heads/" + StreamBranch(stream) }
func ValidateCardBranch(stream, branch string) error {
	b := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if b == "group" || strings.HasSuffix(b, "/group") || b == "sprint/"+stream {
		return fmt.Errorf("git refused: card branch %q cannot use reserved stream integration branch %s", branch, StreamBranch(stream))
	}
	return nil
}
func classifyPushRef_internal(ref string) (string, bool, error) {
	if !strings.HasPrefix(ref, "refs/heads/") {
		ref = "refs/heads/" + strings.TrimPrefix(ref, "/")
	}
	if strings.HasPrefix(ref, "refs/heads/sprint/") && !strings.Contains(strings.TrimPrefix(ref, "refs/heads/sprint/"), "/") {
		return "", false, fmt.Errorf("git refused: bare stream ref %s is forbidden: use %s/group for the stream branch or %s/<producer> for a card branch", ref, ref, ref)
	}
	return ref, strings.HasPrefix(ref, "refs/heads/sprint/") && strings.HasSuffix(ref, "/group"), nil
}
func (c *Client) Push(ctx context.Context, repoDir, sha, ref, expectOld string) error {
	ref, isStream, err := classifyPushRef_internal(ref)
	if err != nil {
		return err
	}
	if !isStream && expectOld != "" {
		if ok, err := c.IsAncestor(ctx, repoDir, expectOld, sha); err != nil || !ok {
			return fmt.Errorf("git refused: force-with-lease is forbidden on base branch %s: base pushes must fast-forward", ref)
		}
	}
	args := []string{"push", "origin", sha + ":" + ref}
	if isStream || expectOld != "" {
		args = []string{"push", fmt.Sprintf("--force-with-lease=%s:%s", ref, expectOld), "origin", sha + ":" + ref}
	}
	_, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return &PushRefusedError{Ref: ref, SHA: sha, Reason: strings.TrimSpace(stderr)}
	}
	return nil
}
func isFullSHA_internal(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < 40; i++ {
		if c := s[i]; !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Clone makes dir a working clone of repo, once: a dir that already holds a
// .git is left as it is (run keeps one clone per stream repo, LAYER-4
// "Git"). It never fetches into an existing clone; LandPass fetches what it
// needs.
func (c *Client) Clone(ctx context.Context, repo, dir string) error {
	if repo == "" || dir == "" {
		return fmt.Errorf("git refused: clone needs a repository and a directory")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	_, stderr, code, err := c.runGit_internal(ctx, "", c.cfg.RebaseTimeout, "clone", "--no-tags", repo, dir)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("git clone %s failed: %s; check the stream's repo (nova-sprint stream show)", repo, strings.TrimSpace(stderr))
	}
	return nil
}
