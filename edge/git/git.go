// Package git is the git and mechanical landing edge of layer 4
package git

import (
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

type PushRefusedError struct {
	Ref, SHA, Reason string
}

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
	Kind       MergeKind
	SHA, Tree  string
	ReusedHead bool
	Conflicts  []string
	Reason     string
}
type RebaseResult struct {
	OK        bool
	SHA       string
	Conflicts []string
	Reason    string
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
	Binary                     string
	Exec                       edge.Exec
	PushTimeout, RebaseTimeout time.Duration
	AuthorName, AuthorEmail    string
}
type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	if cfg.Binary == "" {
		cfg.Binary = "git"
	}
	if cfg.Exec == nil {
		cfg.Exec = edge.NewOSExec()
	}
	if cfg.PushTimeout <= 0 {
		cfg.PushTimeout = DefaultPushTimeout
	}
	if cfg.RebaseTimeout <= 0 {
		cfg.RebaseTimeout = DefaultRebaseTimeout
	}
	if cfg.AuthorName == "" {
		cfg.AuthorName = "Nova Sprint Lander"
	}
	if cfg.AuthorEmail == "" {
		cfg.AuthorEmail = "sprint@mas-bandwidth.com"
	}
	return &Client{cfg: cfg}
}
func (c *Client) gitEnv_internal() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=" + c.cfg.AuthorName,
		"GIT_AUTHOR_EMAIL=" + c.cfg.AuthorEmail,
		"GIT_COMMITTER_NAME=" + c.cfg.AuthorName,
		"GIT_COMMITTER_EMAIL=" + c.cfg.AuthorEmail,
	}
}
func (c *Client) runGit_internal(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, string, int, error) {
	res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: append([]string{c.cfg.Binary}, args...), Dir: dir, Env: c.gitEnv_internal(), Timeout: timeout})
	return res.Stdout, res.Stderr, res.ExitCode, err
}
func (c *Client) Fetch(ctx context.Context, repoDir string, refs ...string) error {
	args := []string{"fetch", "--no-tags", "origin"}
	for _, r := range refs {
		if strings.TrimSpace(r) != "" {
			args = append(args, r)
		}
	}
	_, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, args...)
	switch {
	case err != nil:
		return err
	case code != 0:
		return fmt.Errorf("%w: fetch %s failed: %s", ErrMissingRef, strings.Join(refs, " "), strings.TrimSpace(stderr))
	}
	return nil
}
func (c *Client) LsRemote(ctx context.Context, repoDir, ref string) (string, bool, error) {
	if isFullSHA_internal(ref) {
		if err := c.Fetch(ctx, repoDir, ref); err != nil {
			return "", false, nil
		}
		return strings.ToLower(ref), true, nil
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
	fields := strings.Fields(stdout)
	if len(fields) == 0 || !isFullSHA_internal(fields[0]) {
		return "", false, nil
	}
	return strings.ToLower(fields[0]), true, nil
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
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s exited %d: %s", ancestor, descendant, code, strings.TrimSpace(stderr))
}
func (c *Client) TreeHash(ctx context.Context, repoDir, rev string) (string, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "rev-parse", rev+"^{tree}")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git rev-parse %s^{tree} exited %d: %s", rev, code, strings.TrimSpace(stderr))
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
	ledgerMap := make(map[string]LedgerRule, len(ledgers))
	for _, l := range ledgers {
		ledgerMap[l.Path] = l
	}
	for _, head := range heads {
		if res, done, err := c.mergeOneHead_internal(ctx, repoDir, onto, head, ledgerMap); err != nil || done {
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
func (c *Client) mergeOneHead_internal(ctx context.Context, repoDir, onto, head string, ledgerMap map[string]LedgerRule) (MergeResult, bool, error) {
	_, _, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "merge", "--no-ff", "--no-edit", head)
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
	if err := c.resolveLedgers_internal(ctx, repoDir, ledgerFiles, ledgerMap); err != nil {
		_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "merge", "--abort")
		_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "reset", "--hard", onto)
		return MergeResult{Kind: MergeLedgerFailed, Conflicts: ledgerFiles, Reason: err.Error()}, true, nil
	}
	return MergeResult{}, false, nil
}
func (c *Client) resolveLedgers_internal(ctx context.Context, repoDir string, files []string, ledgerMap map[string]LedgerRule) error {
	for _, f := range files {
		rule := ledgerMap[f]
		if len(rule.Script) > 0 {
			_, _, _, _ = c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "checkout", "--ours", "--", f)
			res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: rule.Script, Dir: repoDir, Env: c.gitEnv_internal(), Timeout: c.cfg.RebaseTimeout})
			if err != nil || !res.OK() {
				return fmt.Errorf("ledger script %s failed on %s: %v %s", strings.Join(rule.Script, " "), f, err, strings.TrimSpace(res.Stderr))
			}
		} else if err := c.mergeLedgerLines_internal(ctx, repoDir, f); err != nil {
			return err
		}
		if _, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "add", "--", f); err != nil || code != 0 {
			return fmt.Errorf("git add %s failed: %v %s", f, err, strings.TrimSpace(stderr))
		}
	}
	if _, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "commit", "--no-edit"); err != nil || code != 0 {
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
				seen[line] = true
				out = append(out, line)
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
		if err != nil {
			return RebaseResult{}, err
		}
		return RebaseResult{OK: true, SHA: strings.TrimSpace(shaOut)}, nil
	}
	conflicts := c.unmergedFiles_internal(ctx, worktree)
	_, _, _, _ = c.runGit_internal(ctx, worktree, c.cfg.RebaseTimeout, "rebase", "--abort")
	reason := strings.TrimSpace(stderr)
	if len(conflicts) > 0 {
		reason = fmt.Sprintf("rebase conflict onto %s: %s", onto, strings.Join(conflicts, ", "))
	}
	return RebaseResult{OK: false, Conflicts: conflicts, Reason: reason}, nil
}

// StreamBranch returns the canonical integration branch name ("sprint/<stream>/group") for a stream.
func StreamBranch(stream string) string {
	return "sprint/" + strings.TrimSpace(stream) + "/group"
}

// StreamRef returns the full integration ref ("refs/heads/sprint/<stream>/group") for a stream.
func StreamRef(stream string) string {
	return "refs/heads/" + StreamBranch(stream)
}

// ValidateCardBranch refuses a card branch or head ref that collides with the stream integration branch ("group").
func ValidateCardBranch(stream, branch string) error {
	b := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if b == "group" || strings.HasSuffix(b, "/group") || b == "sprint/"+stream {
		return fmt.Errorf("git refused: card branch %q cannot use reserved stream integration branch %s", branch, StreamBranch(stream))
	}
	return nil
}

func classifyPushRef_internal(ref, expectOld string) (string, bool, error) {
	if !strings.HasPrefix(ref, "refs/heads/") {
		ref = "refs/heads/" + strings.TrimPrefix(ref, "/")
	}
	if strings.HasPrefix(ref, "refs/heads/sprint/") {
		rest := strings.TrimPrefix(ref, "refs/heads/sprint/")
		if !strings.Contains(rest, "/") {
			return "", false, fmt.Errorf("git refused: bare stream ref %s is forbidden: use %s/group for the stream branch or %s/<producer> for a card branch", ref, ref, ref)
		}
	}
	isStream := strings.HasPrefix(ref, "refs/heads/sprint/") && strings.HasSuffix(ref, "/group")
	if !isStream && expectOld != "" {
		return "", false, fmt.Errorf("git refused: force-with-lease is forbidden on base branch %s: base pushes must fast-forward", ref)
	}
	return ref, isStream, nil
}

func (c *Client) Push(ctx context.Context, repoDir, sha, ref, expectOld string) error {
	var isStream bool
	var err error
	ref, isStream, err = classifyPushRef_internal(ref, expectOld)
	if err != nil {
		return err
	}
	args := []string{"push", "origin", sha + ":" + ref}
	if isStream {
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
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
