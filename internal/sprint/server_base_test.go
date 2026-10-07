package sprint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerSwitchRefusesABinaryBuiltOffTheSprintBase: a binary stamped by the
// toolchain from a side commit is refused, one stamped from the base is
// switched, and the tick raises one judgment while a running server is off
// the base. The base branch is created by name. The candidate is a real Go
// binary; its build commit is the vcs revision, not a version line in a file.
func TestServerSwitchRefusesABinaryBuiltOffTheSprintBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a binary built off the base is refused and not copied", func(t *testing.T) {
		t.Parallel()
		clone := newSprintBaseRepo(t)
		require.Equal(t, "main", gitLine(t, clone, "rev-parse", "--abbrev-ref", "HEAD"))
		mainCommit := gitLine(t, clone, "rev-parse", "main")

		gitOK(t, clone, "checkout", "-q", "-b", "side")
		require.NoError(t, os.WriteFile(filepath.Join(clone, "side.txt"), []byte("side\n"), 0o644))
		gitOK(t, clone, "add", "side.txt")
		gitOK(t, clone, "commit", "-q", "-m", "side")
		sideCommit := gitLine(t, clone, "rev-parse", "HEAD")
		require.NotEqual(t, mainCommit, sideCommit)

		dir := filepath.Dir(clone)
		candidate := filepath.Join(dir, "candidate")
		buildCandidate(t, clone, candidate)
		stamped, _, ok, err := buildinfo.BinaryRevision(candidate)
		require.NoError(t, err)
		require.True(t, ok, "the binary carries no build commit")
		require.Equal(t, sideCommit, stamped, "the build commit is the side commit, not text written into the file")

		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old server"), 0o755))
		err = ServerSwitch(ctx, ServerSwitchOptions{
			Binary:  candidate,
			Target:  target,
			RepoDir: clone,
			BaseRef: "main",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server switch REFUSED")
		assert.Contains(t, err.Error(), "not an ancestor")
		assert.Contains(t, err.Error(), sideCommit)
		assert.Contains(t, err.Error(), "origin/main")
		assert.Contains(t, err.Error(), "build from origin/main at its tip, then switch")
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "old server", string(got), "a refused candidate is not copied onto the target")

		fact, err := ObserveServerBase(ctx, candidate, clone, "main")
		require.NoError(t, err)
		require.True(t, fact.Checked)
		require.True(t, fact.Off)
		assert.Contains(t, fact.What, sideCommit)
		assert.Contains(t, fact.What, "origin/main")
		assert.Contains(t, fact.What, "build from origin/main at its tip, then switch")
		assertOneServerBaseJudgment(t, fact)
	})

	t.Run("a binary built from the base is switched", func(t *testing.T) {
		t.Parallel()
		clone := newSprintBaseRepo(t)
		mainCommit := gitLine(t, clone, "rev-parse", "main")
		dir := filepath.Dir(clone)
		candidate := filepath.Join(dir, "candidate")
		buildCandidate(t, clone, candidate)
		stamped, _, ok, err := buildinfo.BinaryRevision(candidate)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, mainCommit, stamped)

		body, err := os.ReadFile(candidate)
		require.NoError(t, err)
		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old server"), 0o755))
		require.NoError(t, ServerSwitch(ctx, ServerSwitchOptions{
			Binary:  candidate,
			Target:  target,
			RepoDir: clone,
			BaseRef: "main",
		}))
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, body, got)

		fact, err := ObserveServerBase(ctx, candidate, clone, "main")
		require.NoError(t, err)
		require.True(t, fact.Checked)
		assert.False(t, fact.Off, "on the base: %s", fact.What)
	})

	t.Run("a binary with no source commit is refused", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		clone := newSprintBaseRepo(t)
		candidate := filepath.Join(dir, "candidate")
		// version text in a file is not a build commit, and it is not executed
		require.NoError(t, os.WriteFile(candidate, []byte("nova-sprint devel linux/amd64 go1.22 revision=abcdef\n"), 0o755))
		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old server"), 0o755))
		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary:  candidate,
			Target:  target,
			RepoDir: clone,
			BaseRef: "main",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server switch REFUSED")
		assert.Contains(t, err.Error(), "no source commit")
		assert.Contains(t, err.Error(), "build from origin/main at its tip, then switch")
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "old server", string(got))
	})
}

// assertOneServerBaseJudgment: the tick writes the judgment once while the
// server is off the base, writes it no second time, and closes it when the
// server is back on.
func assertOneServerBaseJudgment(t *testing.T, fact ServerBaseFact) {
	t.Helper()
	s := &Snapshot{
		Now:         time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		Work:        NewTable(Work),
		Readers:     NewTable(Readers),
		Merge:       NewTable(Merge),
		Fleet:       NewTable(Fleet),
		Coordinator: "coordinator",
	}
	r := TickReq{Who: MachineActor, ServerBase: fact}
	p, _ := TickDeadlines(s, r)
	notes := notesOfType(p.Notes, NServerOffBase)
	require.Len(t, notes, 1, "off the base raises one judgment, got %+v", p.Notes)
	assert.Equal(t, Judgment, notes[0].Kind)
	assert.Equal(t, fact.What, notes[0].What)
	notes[0].ID = "j-off.1"
	s.Open = []Open{{Key: OpenKey(notes[0].ID, StreamSubject(notes[0].Stream)), Note: notes[0]}}

	p, _ = TickDeadlines(s, r)
	assert.Empty(t, notesOfType(p.Notes, NServerOffBase), "the same episode is not raised again")
	assert.Empty(t, notesOfType(closesOf(p), NServerOffBase), "it stays open while the server is off")

	r.ServerBase.Off = false
	r.ServerBase.What = ""
	p, _ = TickDeadlines(s, r)
	assert.Empty(t, notesOfType(p.Notes, NServerOffBase), "back on the base raises nothing")
	closed := notesOfType(closesOf(p), NServerOffBase)
	require.Len(t, closed, 1, "back on the base closes the judgment, closes %+v", p.Closes)
	assert.Equal(t, "j-off.1", closed[0].ID)
}

func notesOfType(ns []Note, typ string) []Note {
	var out []Note
	for _, n := range ns {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

func closesOf(p Plan) []Note {
	var out []Note
	for _, o := range p.Closes {
		out = append(out, o.Note)
	}
	return out
}

// newSprintBaseRepo is a clone whose branch main was created by name, with
// origin/main at that commit. It does not depend on git init's default branch.
func newSprintBaseRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	clone := filepath.Join(root, "clone")
	gitOK(t, root, "init", "-q", "--bare", "-b", "main", origin)
	gitOK(t, root, "init", "-q", "-b", "main", clone)
	require.NoError(t, os.WriteFile(filepath.Join(clone, "go.mod"), []byte("module example.com/candidate\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(clone, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	gitOK(t, clone, "add", "go.mod", "main.go")
	gitOK(t, clone, "commit", "-q", "-m", "base")
	gitOK(t, clone, "remote", "add", "origin", origin)
	gitOK(t, clone, "push", "-q", "origin", "HEAD:refs/heads/main")
	gitOK(t, clone, "fetch", "-q", "origin", "main")
	require.Equal(t, "main", gitLine(t, clone, "rev-parse", "--abbrev-ref", "HEAD"))
	return clone
}

func buildCandidate(t *testing.T, dir, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = buildEnv()
	got, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build: %s", got)
}

func buildEnv() []string {
	gof := os.Getenv("GOFLAGS")
	if !strings.Contains(gof, "-buildvcs=") {
		if strings.TrimSpace(gof) != "" {
			gof += " "
		}
		gof += "-buildvcs=true"
	}
	skip := map[string]bool{"GOFLAGS": true, "CGO_ENABLED": true, "GOTOOLCHAIN": true, "GOPROXY": true, "GOSUMDB": true}
	for _, name := range gitRepoVars {
		skip[name] = true
	}
	env := []string{"GOFLAGS=" + gof, "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off"}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !skip[name] {
			env = append(env, kv)
		}
	}
	return env
}

func gitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
}

func gitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

var gitRepoVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CEILING_DIRECTORIES",
}

func gitEnv() []string {
	drop := map[string]bool{
		"GIT_CONFIG_GLOBAL": true, "GIT_CONFIG_NOSYSTEM": true,
		"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true,
		"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true,
	}
	for _, name := range gitRepoVars {
		drop[name] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=server-base",
		"GIT_AUTHOR_EMAIL=server-base@example.invalid",
		"GIT_COMMITTER_NAME=server-base",
		"GIT_COMMITTER_EMAIL=server-base@example.invalid",
	)
}
