package sprint_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

const syncBase = "sprint/mechanical-2026-10-02"

type syncRepo struct {
	t                 *testing.T
	origin, dev, land string
	env               []string
	mu                sync.Mutex
	cmds              []*exec.Cmd
}

func newSyncRepo(t *testing.T) *syncRepo {
	t.Helper()
	root := t.TempDir()
	gitconfig := filepath.Join(root, ".gitconfig")
	require.NoError(t, os.WriteFile(gitconfig, []byte("[gc]\n\tautoDetach = false\n\tauto = 0\n[maintenance]\n\tautoDetach = false\n"), 0o644))
	r := &syncRepo{
		t:      t,
		origin: filepath.Join(root, "origin.git"),
		dev:    filepath.Join(root, "dev"),
		land:   filepath.Join(root, "land"),
		env: append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+gitconfig,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=sync-test", "GIT_AUTHOR_EMAIL=sync-test@example.com",
			"GIT_COMMITTER_NAME=sync-test", "GIT_COMMITTER_EMAIL=sync-test@example.com",
		),
	}
	t.Cleanup(func() { _ = r.Close() })
	r.git(root, "init", "-q", "--bare", "-b", "main", r.origin)
	r.git(r.origin, "config", "gc.autoDetach", "false")
	r.git(r.origin, "config", "gc.auto", "0")
	r.git(root, "clone", "-q", r.origin, r.dev)
	r.git(r.dev, "config", "gc.autoDetach", "false")
	r.git(r.dev, "config", "gc.auto", "0")
	require.NoError(t, os.WriteFile(filepath.Join(r.dev, "README.md"), []byte("# repo\n"), 0o644))
	r.git(r.dev, "add", "README.md")
	r.git(r.dev, "commit", "-q", "-m", "initial commit")
	r.git(r.dev, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/dev", "HEAD:refs/heads/"+syncBase)
	r.git(root, "clone", "-q", r.origin, r.land)
	r.git(r.land, "config", "gc.autoDetach", "false")
	r.git(r.land, "config", "gc.auto", "0")
	return r
}

func (r *syncRepo) track(cmd *exec.Cmd) *exec.Cmd {
	r.mu.Lock()
	r.cmds = append(r.cmds, cmd)
	r.mu.Unlock()
	return cmd
}

// Close waits for all started git child processes to exit before returning, so that
// directory cleanup never races a running git child.
func (r *syncRepo) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for _, cmd := range r.cmds {
		if cmd.Process != nil && cmd.ProcessState == nil {
			if err := cmd.Wait(); err != nil && !errors.Is(err, os.ErrProcessDone) && !strings.Contains(err.Error(), "already called Wait") {
				errs = append(errs, err)
			}
		}
	}
	r.cmds = nil
	return errors.Join(errs...)
}

func (r *syncRepo) cmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(r.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = r.env
	return r.track(cmd)
}

func (r *syncRepo) git(dir string, args ...string) string {
	r.t.Helper()
	cmd := r.cmd(dir, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// commit is a developer's commit of file on branch, pushed.
func (r *syncRepo) commit(branch, file, content, msg string) {
	r.t.Helper()
	r.git(r.dev, "fetch", "-q", "origin")
	r.git(r.dev, "checkout", "-q", "-B", branch, "origin/"+branch)
	require.NoError(r.t, os.WriteFile(filepath.Join(r.dev, file), []byte(content), 0o644))
	r.git(r.dev, "add", file)
	r.git(r.dev, "commit", "-q", "-m", msg)
	r.git(r.dev, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
}

func (r *syncRepo) tip(branch string) string { return r.git(r.origin, "rev-parse", branch) }

func (r *syncRepo) has(branch, rev string) bool {
	cmd := r.cmd(r.origin, "merge-base", "--is-ancestor", rev, branch)
	return cmd.Run() == nil
}

// branchHead is a developer's commit of file on a new branch cut from the base, pushed:
// the card's head.
func (r *syncRepo) branchHead(branch, file, content string) string {
	r.t.Helper()
	r.git(r.dev, "fetch", "-q", "origin")
	r.git(r.dev, "checkout", "-q", "-B", branch, "origin/"+syncBase)
	require.NoError(r.t, os.WriteFile(filepath.Join(r.dev, file), []byte(content), 0o644))
	r.git(r.dev, "add", file)
	r.git(r.dev, "commit", "-q", "-m", branch)
	r.git(r.dev, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return r.tip(branch)
}

// syncRig is the twin store with streams s1 and s2, and the twin repository.
type syncRig struct {
	t    *testing.T
	ctx  context.Context
	st   *store.Store
	repo *syncRepo
	mu   sync.Mutex
	now  time.Time
	gate []string // the trees the tree gate saw: the README of each
	red  error
}

func newSyncRig(t *testing.T) *syncRig {
	t.Helper()
	repo := newSyncRepo(t)
	r := &syncRig{t: t, ctx: t.Context(), repo: repo, now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	t.Cleanup(func() { _ = r.Close() })
	m := store.NewMem()
	n := 0
	r.st = &store.Store{B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	for _, s := range []string{"s1", "s2"} {
		res, err := r.st.Run(r.ctx, store.AddStep(sprint.AddReq{Stream: s, Count: 1}))
		require.NoError(t, err)
		require.Empty(t, res.Refused)
	}
	return r
}

func (r *syncRig) Close() error {
	if r.repo != nil {
		return r.repo.Close()
	}
	return nil
}

func (r *syncRig) tick(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

func (r *syncRig) load() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, []string{sprint.Work, sprint.Merge}, nil)
	require.NoError(r.t, err)
	return s
}

// cycle is one land cycle's dev sync as the land round runs it: LandCycleSync on the
// snapshot the round read, in its clone through its tree gate, then the facts recorded in
// one step on the store.
func (r *syncRig) cycle() (sprint.DevSyncFacts, bool, error) {
	r.t.Helper()
	req := sprint.DevSyncReq{RepoDir: r.repo.land, Base: syncBase, Env: r.repo.env,
		Check: func(_ context.Context, dir string) error {
			b, err := os.ReadFile(filepath.Join(dir, "feature.go"))
			if err != nil {
				return err
			}
			r.gate = append(r.gate, string(b))
			return r.red
		}}
	f, due, err := sprint.LandCycleSync(r.ctx, r.load(), req)
	if !due || err != nil {
		return f, due, err
	}
	res, err := r.st.Run(r.ctx, store.Step{Verb: "dev-sync", Load: []string{sprint.Work, sprint.Merge},
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.DevSynced(s, f, req.Streams) }})
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused)
	return f, true, nil
}

func (r *syncRig) open() []sprint.Open {
	var out []sprint.Open
	for _, o := range r.load().Open {
		if o.Note.Type == sprint.NDevSyncConflict {
			out = append(out, o)
		}
	}
	return out
}
