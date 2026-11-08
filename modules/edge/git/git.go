// Package git is the git and mechanical landing edge of layer 4.
package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

const (
	DefaultRebaseTimeout = 2 * time.Minute
	DefaultPushTimeout   = 60 * time.Second
)

var (
	ErrMissingRef  = errors.New("git: ref not on origin")
	ErrPushRefused = errors.New("git: push refused")
)

type PushRefusedError struct{ Ref, SHA, Reason string }

func (e *PushRefusedError) Error() string {
	return fmt.Sprintf("git refused: push %s to %s rejected: %s", e.SHA, e.Ref, e.Reason)
}
func (e *PushRefusedError) Unwrap() error { return ErrPushRefused }

type ExecFunc func(ctx context.Context, cmd edge.Cmd) (edge.Result, error)

func (f ExecFunc) Run(ctx context.Context, cmd edge.Cmd) (edge.Result, error) { return f(ctx, cmd) }

type MergeKind string

const (
	MergeOK       MergeKind = "ok"
	MergeConflict MergeKind = "conflict"
)

type MergeResult struct {
	Kind        MergeKind
	SHA, Reason string
	Conflicts   []string
}
type Git interface {
	Fetch(ctx context.Context, repoDir string, refs ...string) error
	LsRemote(ctx context.Context, repoDir, ref string) (sha string, ok bool, err error)
	IsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error)
	MergeGroup(ctx context.Context, repoDir, onto string, heads []string) (MergeResult, error)
	Push(ctx context.Context, repoDir, sha, ref, expectOld string) error
}
type Config struct {
	Binary, AuthorName, AuthorEmail string
	Exec                            edge.Exec
	PushTimeout, RebaseTimeout      time.Duration
	// Token, when set, is the one credential git uses over https: the host's
	// credential helpers are reset and replaced by one that answers with it,
	// so a push is the token's identity and never the host's own login
	// (defect 39). It reaches git in the environment, never in argv.
	Token string
	// GitHubCredential is the gh binary git asks for https://github.com's
	// credential (`gh auth git-credential` as the host's gh login, every
	// GitHub credential of the process unset for it); "" leaves the host's
	// own git configuration.
	GitHubCredential string
}

// tokenEnv_internal is the credential helper that answers git with the
// token for https://github.com only (githubHelperEnv_internal): a shell
// helper reading the token from the child's environment.
func tokenEnv_internal(token string) []string {
	if token == "" {
		return nil
	}
	helper := `!f() { test "$1" = get || exit 0; echo username=x-access-token; echo "password=$NOVA_SPRINT_GIT_TOKEN"; }; f`
	return append([]string{"NOVA_SPRINT_GIT_TOKEN=" + token}, githubHelperEnv_internal(helper)...)
}

// githubHelperEnv_internal is helper as git's one credential helper, and
// for https://github.com alone, as GIT_CONFIG_* command-line config:
// credential.helper reset to empty first (which drops every helper the
// host's or the repository's config files name, scoped ones included), then
// helper under credential.https://github.com.helper, so any other host (an
// origin or url.*.insteadOf pointed elsewhere) is asked with no helper and
// gets no credential.
func githubHelperEnv_internal(helper string) []string {
	key := "credential." + strings.TrimSuffix(GitHubHost, "/") + ".helper"
	return []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=" + key, "GIT_CONFIG_VALUE_1=" + helper}
}

// CredentialEnv is the git environment that makes one credential the only
// one git uses, for https://github.com only: the token when it is set
// (tokenEnv_internal), else gh's login (credentialEnv_internal). Each
// resets credential.helper before it names its own, so a helper from the
// host's config files (~/.gitconfig's included) is never asked, and no
// other host is ever answered (githubHelperEnv_internal). The token
// reaches git in the environment only, never in argv. The landing's git
// (Client) and the worker's (edge/worker OSGitOps) both use it.
func CredentialEnv(token, gh string) []string {
	if token != "" {
		return tokenEnv_internal(token)
	}
	return credentialEnv_internal(gh)
}

type Client struct{ cfg Config }

func New(cfg Config) *Client {
	cfg.Binary = cmp.Or(cfg.Binary, "git")
	cfg.Exec = cmp.Or[edge.Exec](cfg.Exec, edge.NewOSExec())
	cfg.PushTimeout = cmp.Or(cfg.PushTimeout, DefaultPushTimeout)
	cfg.RebaseTimeout = cmp.Or(cfg.RebaseTimeout, DefaultRebaseTimeout)
	return &Client{cfg: cfg}
}

type identityKey struct{}

// WithIdentity is ctx carrying the sprint's commit identity (store.Identity,
// round 24): every commit git makes under it (the landing's merge commits)
// is authored and committed as name <email>, over Config's. The landing
// passes the store's identity this way, read in the same snapshot as the
// request, so a merge commit never carries a made-up identity (fix list
// L4.8; defect 80: the CAA refused merge commits authored by a literal).
func WithIdentity(ctx context.Context, name, email string) context.Context {
	return context.WithValue(ctx, identityKey{}, [2]string{name, email})
}

// identity_internal is the identity commits are made as: ctx's, else
// Config's; ok is false when neither names both a name and an email.
func (c *Client) identity_internal(ctx context.Context) (name, email string, ok bool) {
	if id, found := ctx.Value(identityKey{}).([2]string); found && id[0] != "" && id[1] != "" {
		return id[0], id[1], true
	}
	return c.cfg.AuthorName, c.cfg.AuthorEmail, c.cfg.AuthorName != "" && c.cfg.AuthorEmail != ""
}

func (c *Client) gitEnv_internal(ctx context.Context, extra ...string) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if name, email, ok := c.identity_internal(ctx); ok {
		env = append(env, "GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email, "GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email)
	}
	return append(append(env, CredentialEnv(c.cfg.Token, c.cfg.GitHubCredential)...), extra...)
}

// runGitEnv_internal runs git; a run that could not complete (a timeout, a
// missing binary) is an edge.CommandError naming the command, so every
// caller's err carries it.
func (c *Client) runGitEnv_internal(ctx context.Context, dir string, timeout time.Duration, extraEnv []string, args ...string) (string, string, int, error) {
	cmd := edge.Cmd{Argv: append([]string{c.cfg.Binary}, args...), Dir: dir, Env: c.gitEnv_internal(ctx, extraEnv...), Timeout: timeout}
	res, err := c.cfg.Exec.Run(ctx, cmd)
	if err != nil {
		return res.Stdout, res.Stderr, res.ExitCode, c.failed_internal(res.ExitCode, res.Stdout, res.Stderr, err, args...)
	}
	return res.Stdout, res.Stderr, res.ExitCode, nil
}

// failed_internal is a git run's failure: the command line, its exit code
// and the first lines of its stdout and stderr (edge.Failure).
func (c *Client) failed_internal(code int, stdout, stderr string, err error, args ...string) error {
	return edge.Failure(append([]string{c.cfg.Binary}, args...), nil, code, stdout, stderr, err)
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
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, args...)
	if err != nil {
		return err
	}
	if code != 0 && missingRef_internal(stderr) {
		return fmt.Errorf("%w: %w", ErrMissingRef, c.failed_internal(code, stdout, stderr, nil, args...))
	}
	if code != 0 { // a login, a host, a timeout: git's own words, never "not on origin"
		return c.failed_internal(code, stdout, stderr, nil, args...)
	}
	return nil
}

// missingRef_internal: git's stderr says the fetched ref or object is not on
// origin (a branch: "couldn't find remote ref"; a sha: "not our ref", or a
// server that serves no unadvertised object).
func missingRef_internal(stderr string) bool {
	return strings.Contains(stderr, "couldn't find remote ref") || strings.Contains(stderr, "not our ref") || strings.Contains(stderr, "unadvertised object")
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
		return "", false, c.failed_internal(code, stdout, stderr, nil, "ls-remote", "--exit-code", "origin", ref)
	}
	if fields := strings.Fields(stdout); len(fields) > 0 && isFullSHA_internal(fields[0]) {
		return strings.ToLower(fields[0]), true, nil
	}
	return "", false, nil
}

// Tip is branch's tip at the clone address url (CloneURL), read with one
// `git ls-remote` and no clone: ok is false when url has no such branch. A
// bare branch name is read as refs/heads/<branch>, never a tag.
func (c *Client) Tip(ctx context.Context, url, branch string) (string, bool, error) {
	if url == "" || branch == "" {
		return "", false, fmt.Errorf("git refused: a tip needs a clone address and a branch")
	}
	ref := branch
	if !strings.HasPrefix(ref, "refs/") {
		ref = "refs/heads/" + ref
	}
	stdout, stderr, code, err := c.runGit_internal(ctx, "", c.cfg.PushTimeout, "ls-remote", "--exit-code", url, ref)
	switch {
	case err != nil:
		return "", false, err
	case code == 2:
		return "", false, nil
	case code != 0:
		return "", false, c.failed_internal(code, stdout, stderr, nil, "ls-remote", "--exit-code", url, ref)
	}
	if fields := strings.Fields(stdout); len(fields) > 0 && isFullSHA_internal(fields[0]) {
		return strings.ToLower(fields[0]), true, nil
	}
	return "", false, c.failed_internal(code, stdout, stderr, fmt.Errorf("no sha in its output"), "ls-remote", "--exit-code", url, ref)
}

func (c *Client) lsRemoteSHA_internal(ctx context.Context, repoDir, target string) (string, bool, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, "ls-remote", "origin")
	if err != nil {
		return "", false, err
	}
	if code != 0 {
		return "", false, c.failed_internal(code, stdout, stderr, nil, "ls-remote", "origin")
	}
	var remoteSHAs []string
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.Fields(line); len(fields) >= 1 && isFullSHA_internal(fields[0]) {
			s := strings.ToLower(fields[0])
			if s == target {
				// A tip origin lists that cannot be fetched is not absent: the
				// fetch's failure is the answer, never "not on origin".
				// Fetches without the clone's lock (LockClone): on a shared clone, ls-remote a ref, never a sha.
				if err := c.Fetch(ctx, repoDir, target); err != nil {
					return "", false, err
				}
				return target, true, nil
			}
			remoteSHAs = append(remoteSHAs, s)
		}
	}
	// target is on origin when some tip contains it. A tip that cannot be
	// fetched or checked leaves the answer unknown: when no tip contains it,
	// the first such failure is returned rather than "not on origin".
	var unknown error
	for _, rsha := range remoteSHAs {
		if err := c.Fetch(ctx, repoDir, rsha); err != nil { // unlocked too, as above
			unknown = cmp.Or(unknown, err)
			continue
		}
		ok, err := c.IsAncestor(ctx, repoDir, target, rsha)
		if err != nil {
			unknown = cmp.Or(unknown, err)
			continue
		}
		if ok {
			return target, true, nil
		}
	}
	return "", false, unknown
}
func (c *Client) IsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "merge-base", "--is-ancestor", ancestor, descendant)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return true, nil
	case code == 1:
		return false, nil
	default:
		return false, c.failed_internal(code, stdout, stderr, nil, "merge-base", "--is-ancestor", ancestor, descendant)
	}
}
func (c *Client) MergeGroup(ctx context.Context, repoDir, onto string, heads []string) (MergeResult, error) {
	if len(heads) == 0 {
		return MergeResult{}, fmt.Errorf("git refused: MergeGroup requires at least one head")
	}
	if len(heads) == 1 {
		if ok, err := c.IsAncestor(ctx, repoDir, onto, heads[0]); err == nil && ok {
			if stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "checkout", "--detach", heads[0]); err != nil || code != 0 {
				return MergeResult{}, cmp.Or(err, c.failed_internal(code, stdout, stderr, nil, "checkout", "--detach", heads[0]))
			}
			return MergeResult{Kind: MergeOK, SHA: heads[0]}, nil
		}
	}
	if _, _, ok := c.identity_internal(ctx); !ok {
		return MergeResult{}, fmt.Errorf("git refused: MergeGroup makes merge commits and has no commit identity (the sprint's, round 24); set it with nova-sprint identity --name --email --reason")
	}
	if stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "checkout", "--detach", onto); err != nil || code != 0 {
		return MergeResult{}, cmp.Or(err, c.failed_internal(code, stdout, stderr, nil, "checkout", "--detach", onto))
	}
	baseDateRaw, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "show", "-s", "--format=%cI", onto)
	if err != nil || code != 0 {
		return MergeResult{}, cmp.Or(err, c.failed_internal(code, baseDateRaw, stderr, nil, "show", "-s", "--format=%cI", onto))
	}
	baseDate := strings.TrimSpace(baseDateRaw)
	dateEnv := []string{"GIT_AUTHOR_DATE=" + baseDate, "GIT_COMMITTER_DATE=" + baseDate}
	commitMsg := "nova-sprint merge " + strings.Join(heads, " ")
	for _, head := range heads {
		if res, done, err := c.mergeOneHead_internal(ctx, repoDir, onto, head, dateEnv, commitMsg); err != nil || done {
			return res, err
		}
	}
	shaOut, shaErr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, "rev-parse", "HEAD")
	if err != nil || code != 0 {
		return MergeResult{}, cmp.Or(err, c.failed_internal(code, shaOut, shaErr, nil, "rev-parse", "HEAD"))
	}
	return MergeResult{Kind: MergeOK, SHA: strings.TrimSpace(shaOut)}, nil
}
func (c *Client) mergeOneHead_internal(ctx context.Context, repoDir, onto, head string, dateEnv []string, commitMsg string) (MergeResult, bool, error) {
	mergeOut, mergeErr, code, err := c.runGitEnv_internal(ctx, repoDir, c.cfg.RebaseTimeout, dateEnv, "merge", "--no-ff", "-m", commitMsg, head)
	if err != nil || code == 0 {
		return MergeResult{}, err != nil, err
	}
	conflicts, listErr := c.unmergedFiles_internal(ctx, repoDir)
	cleanup := c.cleanup_internal(ctx, repoDir, []string{"merge", "--abort"}, []string{"reset", "--hard", onto})
	reason := fmt.Sprintf("merge conflict on %s: %s", head, strings.Join(conflicts, ", "))
	if len(conflicts) == 0 {
		// No unmerged file: the merge failed for another reason, and
		// git's own words are the reason.
		reason = fmt.Sprintf("merge of %s failed: %v", head, cmp.Or(listErr, c.failed_internal(code, mergeOut, mergeErr, nil, "merge", "--no-ff", "-m", commitMsg, head)))
	}
	return MergeResult{Kind: MergeConflict, Conflicts: conflicts, Reason: reason + cleanup}, true, nil
}

// cleanup_internal runs each git command to put a work tree back; it
// returns "" when all succeed, else "; cleanup: <the first failure>" for the
// caller's reason (the tree may be left dirty, and that is said).
func (c *Client) cleanup_internal(ctx context.Context, dir string, cmds ...[]string) string {
	for _, args := range cmds {
		stdout, stderr, code, err := c.runGit_internal(ctx, dir, c.cfg.RebaseTimeout, args...)
		if err != nil || code != 0 {
			return "; cleanup: " + cmp.Or(err, c.failed_internal(code, stdout, stderr, nil, args...)).Error()
		}
	}
	return ""
}

func (c *Client) unmergedFiles_internal(ctx context.Context, dir string) ([]string, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, dir, c.cfg.RebaseTimeout, "diff", "--name-only", "--diff-filter=U")
	if err != nil || code != 0 {
		return nil, cmp.Or(err, c.failed_internal(code, stdout, stderr, nil, "diff", "--name-only", "--diff-filter=U"))
	}
	var files []string
	for _, line := range strings.Split(stdout, "\n") {
		if f := strings.TrimSpace(line); f != "" {
			files = append(files, f)
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// GroupPrefix is the group branches' namespace: sprint-group/<stream>,
// beside sprint/ and never inside it, so neither a base named
// sprint/<stream> nor a card branch sprint/<stream>/<card> can block it
// (git keeps no ref that is both a leaf and a directory; defect 46).
const GroupPrefix = "sprint-group/"

// GroupBranch is stream's group branch, where rung 2 merges its heads.
func GroupBranch(stream string) string { return GroupPrefix + strings.TrimSpace(stream) }

// GroupRef is GroupBranch as a ref.
func GroupRef(stream string) string { return "refs/heads/" + GroupBranch(stream) }

// GroupBlocker is the branch among heads (refs/heads/<name> -> sha, as
// Heads reads them) that keeps stream's group branch from being created:
// sprint-group itself (a leaf where the directory must be) or a branch
// under sprint-group/<stream>/ (a directory where the leaf must be); "" when
// none does. An existing group branch blocks nothing.
func GroupBlocker(stream string, heads map[string]string) string {
	leaf := "refs/heads/" + strings.TrimSuffix(GroupPrefix, "/")
	under := GroupRef(stream) + "/"
	var out []string
	for ref := range heads {
		if ref == leaf || strings.HasPrefix(ref, under) {
			out = append(out, strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	slices.Sort(out)
	return strings.Join(out, ", ")
}

// GroupNameProblem is why sprint-group/<stream> is not a branch name git
// accepts (a component starting or ending with a dot, a "..", a ".lock"
// end), or "" when it is one.
func GroupNameProblem(stream string) string {
	switch {
	case stream == "":
		return "the stream has no name"
	case strings.HasPrefix(stream, "."), strings.HasSuffix(stream, "."):
		return "a branch name component cannot start or end with a dot"
	case strings.Contains(stream, ".."):
		return "a branch name cannot contain .."
	case strings.HasSuffix(stream, ".lock"):
		return "a branch name cannot end with .lock"
	}
	return ""
}

// Heads is every branch at the clone address url, one `git ls-remote
// --heads` and no clone: refs/heads/<name> -> sha.
func (c *Client) Heads(ctx context.Context, url string) (map[string]string, error) {
	stdout, stderr, code, err := c.runGit_internal(ctx, "", c.cfg.PushTimeout, "ls-remote", "--heads", url)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, c.failed_internal(code, stdout, stderr, nil, "ls-remote", "--heads", url)
	}
	out := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		if f := strings.Fields(line); len(f) == 2 && isFullSHA_internal(f[0]) {
			out[f[1]] = strings.ToLower(f[0])
		}
	}
	return out, nil
}

// ValidateCardBranch refuses a card branch that is a group branch.
func ValidateCardBranch(stream, branch string) error {
	b := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if b == "" || strings.HasPrefix(b, GroupPrefix) {
		return fmt.Errorf("git refused: card branch %q of stream %s is empty or under %s, the group branches' namespace", branch, stream, GroupPrefix)
	}
	return nil
}

// classifyPushRef_internal is ref as refs/heads/..., and whether it is a
// group branch (rewritten with a lease) rather than one that only
// fast-forwards.
func classifyPushRef_internal(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "refs/heads/") {
		ref = "refs/heads/" + strings.TrimPrefix(ref, "/")
	}
	return ref, strings.HasPrefix(ref, "refs/heads/"+GroupPrefix)
}
func (c *Client) Push(ctx context.Context, repoDir, sha, ref, expectOld string) error {
	ref, isStream := classifyPushRef_internal(ref)
	if !isStream && expectOld != "" {
		ok, err := c.IsAncestor(ctx, repoDir, expectOld, sha)
		if err != nil {
			return fmt.Errorf("git refused: cannot tell whether the push to base branch %s fast-forwards: %w", ref, err)
		}
		if !ok {
			return fmt.Errorf("git refused: force-with-lease is forbidden on base branch %s: base pushes must fast-forward", ref)
		}
	}
	args := []string{"push", "origin", sha + ":" + ref}
	if isStream || expectOld != "" {
		args = []string{"push", fmt.Sprintf("--force-with-lease=%s:%s", ref, expectOld), "origin", sha + ":" + ref}
	}
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.PushTimeout, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return &PushRefusedError{Ref: ref, SHA: sha, Reason: c.failed_internal(code, stdout, stderr, nil, args...).Error()}
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

// Clone makes dir a working clone of repo (a clone address: CloneURL), once: a dir that already holds a
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
	stdout, stderr, code, err := c.runGit_internal(ctx, "", c.cfg.RebaseTimeout, "clone", "--no-tags", repo, dir)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%w; check the stream's repo: nova-sprint check", c.failed_internal(code, stdout, stderr, nil, "clone", "--no-tags", repo, dir))
	}
	return nil
}
