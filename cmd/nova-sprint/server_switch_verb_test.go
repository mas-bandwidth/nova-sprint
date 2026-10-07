package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerGroupAndSwitchHelp(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	// Bare group refuses naming its verbs
	code, _, errs := ta.do("server")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "server wants one of its verbs")
	assert.Contains(t, errs, "server switch")

	// Help for group
	code, out, _ := ta.do("help server")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "server switch")

	// Verb -h
	code, out, _ = ta.do("server switch -h")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "nova-sprint server switch")
	assert.Contains(t, out, "--rollback")
	assert.Contains(t, out, "--window")
	assert.Contains(t, out, "--repo")
	assert.Contains(t, out, "--base")
}

func TestServerSwitchVerbSwitchesAndRollsBack(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	// Refuses with no args and no --rollback
	code, _, errs := ta.do("server switch")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "wants <binary> [--rollback] or --rollback alone")

	dir := t.TempDir()
	clone := verbBaseRepo(t, dir)
	target := filepath.Join(dir, "nova-sprint")
	candidate := filepath.Join(dir, "nova-sprint-candidate")
	require.NoError(t, os.WriteFile(target, []byte("version-1"), 0o755))
	buildVerbCandidate(t, clone, candidate)
	body, err := os.ReadFile(candidate)
	require.NoError(t, err)

	// Switch to a binary built on the base, with rollback enabled.
	code, out, errs := ta.do("server switch " + candidate + " --target " + target + " --repo " + clone + " --base main --rollback")
	assert.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SERVER SWITCH OK")

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, body, content)

	prevContent, err := os.ReadFile(target + ".prev")
	require.NoError(t, err)
	assert.Equal(t, "version-1", string(prevContent))

	// Explicit rollback command restores the previous file and does not install.
	code, out, errs = ta.do("server switch --target " + target + " --rollback")
	assert.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SERVER SWITCH ROLLED BACK")

	restored, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "version-1", string(restored))

	// A binary built on a side branch is refused, and the target stays.
	verbGit(t, clone, "checkout", "-q", "-b", "side")
	require.NoError(t, os.WriteFile(filepath.Join(clone, "side.txt"), []byte("side\n"), 0o644))
	verbGit(t, clone, "add", "side.txt")
	verbGit(t, clone, "commit", "-q", "-m", "side")
	off := filepath.Join(dir, "off")
	buildVerbCandidate(t, clone, off)
	side, _, ok, err := buildinfo.BinaryRevision(off)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, verbGitLine(t, clone, "rev-parse", "HEAD"), side)
	code, _, errs = ta.do("server switch " + off + " --target " + target + " --repo " + clone + " --base main")
	assert.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "server switch REFUSED")
	assert.Contains(t, errs, "not an ancestor")
	assert.Contains(t, errs, side)
	assert.Contains(t, errs, "origin/main")
	assert.Contains(t, errs, "build from origin/main at its tip, then switch")
	restored, err = os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "version-1", string(restored), "an off-base candidate is not copied")
}

// verbBaseRepo is a clone whose main was created by name, with a candidate
// that prints a shadow plan, and origin/main at that commit.
func verbBaseRepo(t *testing.T, root string) string {
	t.Helper()
	origin := filepath.Join(root, "origin.git")
	clone := filepath.Join(root, "clone")
	verbGit(t, root, "init", "-q", "--bare", "-b", "main", origin)
	verbGit(t, root, "init", "-q", "-b", "main", clone)
	mod := "module example.com/candidate\n\ngo 1.22\n"
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(`{\"shadow\":{\"epoch\":0,\"state\":\"STOPPED\",\"parts\":[],\"size\":0,\"took_ns\":1}}`)\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(clone, "go.mod"), []byte(mod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(clone, "main.go"), []byte(src), 0o644))
	verbGit(t, clone, "add", "go.mod", "main.go")
	verbGit(t, clone, "commit", "-q", "-m", "base")
	verbGit(t, clone, "remote", "add", "origin", origin)
	verbGit(t, clone, "push", "-q", "origin", "HEAD:refs/heads/main")
	verbGit(t, clone, "fetch", "-q", "origin", "main")
	require.Equal(t, "main", verbGitLine(t, clone, "rev-parse", "--abbrev-ref", "HEAD"))
	return clone
}

func buildVerbCandidate(t *testing.T, dir, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = verbBuildEnv()
	got, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build: %s", got)
}

func verbBuildEnv() []string {
	gof := os.Getenv("GOFLAGS")
	if !strings.Contains(gof, "-buildvcs=") {
		if strings.TrimSpace(gof) != "" {
			gof += " "
		}
		gof += "-buildvcs=true"
	}
	skip := map[string]bool{"GOFLAGS": true, "CGO_ENABLED": true, "GOTOOLCHAIN": true, "GOPROXY": true, "GOSUMDB": true}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CEILING_DIRECTORIES"} {
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

func verbGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = verbGitEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
}

func verbGitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = verbGitEnv()
	out, err := cmd.Output()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func verbGitEnv() []string {
	drop := map[string]bool{
		"GIT_CONFIG_GLOBAL": true, "GIT_CONFIG_NOSYSTEM": true,
		"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true,
		"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true,
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_OBJECT_DIRECTORY": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_INDEX_FILE": true,
		"GIT_COMMON_DIR": true, "GIT_NAMESPACE": true, "GIT_PREFIX": true,
		"GIT_CEILING_DIRECTORIES": true,
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
