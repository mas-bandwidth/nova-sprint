package sprint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/diffcheck"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The work lint's git: one clone of the card's repository, read with no checkout (the
// head and the base tip fetched by name, the diff, the commits, git merge-tree and the
// files at the head). It runs outside the plan's state: what it reads is the commit's.

// headRE is a head as a finish names a pushed one: a commit id, abbreviated or whole.
var headRE = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// WorkLintBudget bounds one attempt's whole read, its fetches included.
const WorkLintBudget = 60 * time.Second

// ReadWorkView reads the work lint's view of head against the base in the clone dir:
// the head and the base fetched from origin when the clone lacks them, the merge-base, the
// diff and commits from it to the head, the merge onto the base tip, and the head's files.
// A head that is no commit id is not pushed, and the view says so with nothing else read;
// a git that fails (a fetch, a base the remote lacks) is an error: no finding is made of it.
func ReadWorkView(ctx context.Context, dir, base, head string, env []string) (WorkView, error) {
	v := WorkView{Head: head}
	if !headRE.MatchString(head) {
		return v, nil
	}
	o := gitrun.Options{C: dir, Env: env, OwnRepo: true}
	git := func(args ...string) (string, error) { return gitrun.Output(ctx, o, args...) }
	if _, err := git("cat-file", "-e", head+"^{commit}"); err != nil {
		if _, err := git("fetch", "--no-tags", "-q", "origin", head); err != nil {
			return v, fmt.Errorf("work lint: fetch of the head %s in %s: %w", head, dir, err)
		}
		if _, err := git("cat-file", "-e", head+"^{commit}"); err != nil {
			return v, nil
		}
	}
	v.Pushed = true
	if _, err := git("fetch", "--no-tags", "-q", "origin", "+refs/heads/"+base+":refs/remotes/origin/"+base); err != nil {
		return v, fmt.Errorf("work lint: fetch of the base %s in %s: %w", base, dir, err)
	}
	tip, err := git("rev-parse", "--verify", "refs/remotes/origin/"+base+"^{commit}")
	if err != nil {
		return v, fmt.Errorf("work lint: the base tip of %s in %s: %w", base, dir, err)
	}
	mb, err := git("merge-base", tip, head)
	if err != nil {
		return v, fmt.Errorf("work lint: no merge-base of %s and %s in %s: %w", base, head, dir, err)
	}
	v.MergeBase = mb
	res, err := gitrun.Run(ctx, o, "diff", "--no-color", "--no-ext-diff", "-M", mb, head)
	if err != nil {
		return v, fmt.Errorf("work lint: diff %s..%s in %s: %w", mb, head, dir, err)
	}
	v.Diff = string(res.Stdout)
	tracked, err := git("ls-tree", "-r", "--name-only", mb)
	if err != nil {
		return v, fmt.Errorf("work lint: the tree at %s in %s: %w", mb, dir, err)
	}
	v.Tracked = strings.Split(tracked, "\n")
	log, err := git("log", "--format=%B%x00", mb+".."+head)
	if err != nil {
		return v, fmt.Errorf("work lint: the commits %s..%s in %s: %w", mb, head, dir, err)
	}
	for _, m := range strings.Split(log, "\x00") {
		if m = strings.TrimSpace(m); m != "" {
			v.Messages = append(v.Messages, m)
		}
	}
	// the merge onto the base tip, with no checkout: exit 1 is a merge with conflicts,
	// its tree's id first and the conflicted files after
	mt, err := gitrun.Run(ctx, o, "merge-tree", "--write-tree", "--name-only", "--no-messages", tip, head)
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		lines := strings.Split(strings.TrimSpace(string(mt.Stdout)), "\n")
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" {
				v.Conflicts = append(v.Conflicts, l)
			}
		}
		if v.Conflicts == nil {
			v.Conflicts = []string{"(the merge-tree named no file)"}
		}
	default:
		return v, fmt.Errorf("work lint: merge-tree %s %s in %s: %w", tip, head, dir, err)
	}
	v.Show = func(p string) ([]byte, bool) {
		r, err := gitrun.Run(ctx, o, "cat-file", "blob", head+":"+p)
		if err != nil {
			return nil, false
		}
		return r.Stdout, true
	}
	v.Ls = func(d string) []string {
		out, err := git("ls-tree", "--name-only", head, strings.TrimSuffix(d, "/")+"/")
		if err != nil || out == "" {
			return nil
		}
		return strings.Split(out, "\n")
	}
	return v, nil
}

// WorkLintGit is where the work lint finds a card's repository: Clone is the clone of the
// repository a brief names (its REPO or base-repo line), an error when there is none; Env
// the git children's environment, nil inheriting.
type WorkLintGit struct {
	Clone func(repo string) (string, error)
	Env   []string
	// Gate runs the TEST line's test at a commit-ish of the clone, the machine lint's pin
	// check (gatelint.go). nil runs the check in the clone itself (runGateInClone): the
	// bench-run verb is the production seam, and a fake is the tests'.
	Gate GateTestRun
	// TTL is how long one head's findings are kept against the same base tip's read; zero
	// is two minutes. Now is the clock the cache is kept by; nil is the wall clock.
	TTL time.Duration
	Now func() time.Time
}

// NewWorkLinter is the work lint over g: each primary's view read once per head and
// brief while its findings are fresh (the tick plans a part more than once, and the no-stall
// rule asks what the ask does), then judged (WorkLint).
func NewWorkLinter(g WorkLintGit) WorkLinter {
	type kept struct {
		at  time.Time
		fs  []LintFinding
		err error
	}
	var mu sync.Mutex
	cache := map[string]kept{}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	now := g.Now
	if now == nil {
		now = time.Now
	}
	return func(s *Snapshot, pr *Card) ([]LintFinding, error) {
		in := LintInputOf(s, pr)
		cb := swarm.ReadCardBase([]byte(in.Brief))
		base := cb.Ref
		if base == "" {
			base = "main"
		}
		head := pr.F("head")
		key := strings.Join([]string{cb.Repo, base, head, in.Brief, in.Model, in.Harness}, "\x00")
		mu.Lock()
		if k, ok := cache[key]; ok && now().Sub(k.at) < ttl {
			mu.Unlock()
			return k.fs, k.err
		}
		mu.Unlock()
		fs, err := func() ([]LintFinding, error) {
			if cb.Repo == "" || g.Clone == nil {
				// no repository, no commit to judge: a sprint of cards with no code
				return nil, fmt.Errorf("work lint: the brief of %s names no repository", pr.ID)
			}
			if !headRE.MatchString(head) {
				// a finish with no head keeps its work card's id as the head (Finish)
				return WorkLint(in, WorkView{}), nil
			}
			dir, err := g.Clone(cb.Repo)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), WorkLintBudget)
			defer cancel()
			v, err := ReadWorkView(ctx, dir, base, head, g.Env)
			if err != nil {
				return nil, err
			}
			// the machine lint's pin checks run the TEST line's test at the merge-base
			// and at the head with the change's non-test hunks reverted, through g.Gate
			// (the bench-run verb) or, with none, in the clone itself
			gate := g.Gate
			if gate == nil {
				gate = func(ctx context.Context, ref string, argv []string) (BenchResult, error) {
					return runGateInClone(ctx, dir, ref, argv, g.Env)
				}
			}
			v.GateRun = func(ref string, argv []string) (BenchResult, error) { return gate(ctx, ref, argv) }
			v.Reverted = func() (string, error) {
				return revertNonTestHunks(ctx, dir, v.MergeBase, head, v.Diff, g.Env)
			}
			return WorkLint(in, v), nil
		}()
		mu.Lock()
		if len(cache) > 4096 {
			clear(cache)
		}
		cache[key] = kept{at: now(), fs: fs, err: err}
		mu.Unlock()
		return fs, err
	}
}

// runGateInClone runs argv at ref in a worktree of the clone dir and returns the result:
// the machine lint's pin check when no bench seam is set. A command that never reached git
// or go is an error (the check is skipped); a command that ran gives its exit status and
// combined output. The worktree is removed and the clone's own checkout untouched.
func runGateInClone(ctx context.Context, dir, ref string, argv []string, env []string) (BenchResult, error) {
	if ref == "" || len(argv) == 0 {
		return BenchResult{}, fmt.Errorf("machine lint: no ref or command")
	}
	tmp, err := os.MkdirTemp("", "nova-lint-")
	if err != nil {
		return BenchResult{}, err
	}
	defer os.RemoveAll(tmp)
	tree := filepath.Join(tmp, "tree")
	o := gitrun.Options{C: dir, Env: env, OwnRepo: true}
	if _, err := gitrun.Run(ctx, o, "worktree", "add", "--detach", tree, ref); err != nil {
		return BenchResult{}, err
	}
	defer gitrun.Run(context.Background(), o, "worktree", "remove", "--force", tree)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = tree
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		return BenchResult{}, err
	}
	host, _ := os.Hostname()
	return BenchResult{Host: host, Code: code, Out: string(out)}, nil
}

// revertNonTestHunks makes a commit at head with every non-test file the diff changes
// restored to its content at mergeBase (a file the change adds is removed), the change's
// test files kept, and returns the commit's id. The reverted head is what the pin-back
// check runs the TEST line's test at: it must fail there. "" is no reverted tree.
func revertNonTestHunks(ctx context.Context, dir, mergeBase, head, diff string, env []string) (string, error) {
	if mergeBase == "" || head == "" {
		return "", nil
	}
	var revert []string
	for _, f := range diffcheck.Parse(diff) {
		if f.New == "" || strings.HasSuffix(f.New, "_test.go") {
			continue
		}
		revert = append(revert, f.New)
	}
	tmp, err := os.MkdirTemp("", "nova-lint-revert-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	tree := filepath.Join(tmp, "tree")
	o := gitrun.Options{C: dir, Env: env, OwnRepo: true}
	if _, err := gitrun.Run(ctx, o, "worktree", "add", "--detach", tree, head); err != nil {
		return "", err
	}
	defer gitrun.Run(context.Background(), o, "worktree", "remove", "--force", tree)
	own := gitrun.Options{C: tree, Env: env, OwnRepo: true}
	for _, p := range revert {
		if _, err := gitrun.Run(ctx, own, "cat-file", "-e", mergeBase+":"+p); err != nil {
			// the change adds the file: reverting its hunk removes it
			if err := os.Remove(filepath.Join(tree, p)); err != nil && !os.IsNotExist(err) {
				return "", err
			}
			continue
		}
		if _, err := gitrun.Run(ctx, own, "checkout", mergeBase, "--", p); err != nil {
			return "", err
		}
	}
	if _, err := gitrun.Run(ctx, own, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := gitrun.Run(ctx, own, "-c", "user.name=nova-sprint", "-c", "user.email=nova-sprint@localhost",
		"commit", "--allow-empty", "-q", "-m", "the change's non-test hunks reverted"); err != nil {
		return "", err
	}
	out, err := gitrun.Output(ctx, own, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
