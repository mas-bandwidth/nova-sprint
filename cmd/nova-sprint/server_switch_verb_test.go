package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
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

// switchBase is a twin of the server's repository for server switch: origin (bare) whose main
// is the sprint base, with a side branch cut from it, and the server's clone, where the
// candidate's build commit is checked (docs/SPEC-SPRINT.md section 14, "server-from-base-only-w-ns-bb.w1").
type switchBase struct {
	repo, tip, side string
}

// args are the switch's words that name the base: --repo <clone> --base main.
func (b switchBase) args() string { return " --repo " + b.repo + " --base main" }

func newSwitchBase(t *testing.T) switchBase {
	t.Helper()
	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=twin", "-c", "user.email=twin@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	origin, work, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "work"), filepath.Join(root, "server")
	git(root, "init", "-q", "--bare", "-b", "main", origin)
	git(root, "clone", "-q", origin, work)
	git(work, "checkout", "-q", "-b", "main")
	git(work, "commit", "-q", "--allow-empty", "-m", "base")
	git(work, "push", "-q", "origin", "main")
	git(work, "checkout", "-q", "-b", "side")
	git(work, "commit", "-q", "--allow-empty", "-m", "side")
	side := git(work, "rev-parse", "HEAD")
	git(work, "push", "-q", "origin", "side")
	git(root, "clone", "-q", origin, repo)
	git(work, "checkout", "-q", "main")
	git(work, "commit", "-q", "--allow-empty", "-m", "tip")
	tip := git(work, "rev-parse", "HEAD")
	git(work, "push", "-q", "origin", "main")
	return switchBase{repo: repo, tip: tip, side: side}
}

// candidateAt is a candidate binary built at commit: its version verb names the commit, and
// any other words run body (a script with no #! line).
func candidateAt(t *testing.T, path, commit, body string) string {
	t.Helper()
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo 'nova-sprint v9.9.9 linux/amd64 go1.26 commit=" + commit + "'; exit 0; fi\n" + body
	// write beside then rename into place: a file a concurrent exec still holds open for
	// writing is not the file switched in, and rename replaces the inode (ETXTBSY)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	require.NoError(t, err)
	_, err = tmp.WriteString(script)
	require.NoError(t, err)
	require.NoError(t, tmp.Chmod(0o755))
	require.NoError(t, tmp.Close())
	require.NoError(t, os.Rename(tmp.Name(), path))
	return path
}

// TestServerSwitchVerbRefusesACandidateOffTheBase: server switch checks the candidate's build
// commit against origin's sprint base before its shadow tick and before anything on disk
// changes (docs/SPEC-SPRINT.md section 14, "server-from-base-only-w-ns-bb.w1"): a candidate from a
// side branch is refused naming the commit, the base and the remedy; a switch with no base
// named is refused naming the flags; one from the base's tip passes with a BASE OK line.
func TestServerSwitchVerbRefusesACandidateOffTheBase(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	b := newSwitchBase(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "nova-sprint")
	require.NoError(t, os.WriteFile(target, []byte("the running server"), 0o755))
	shadowOK := "echo '" + `{"shadow":{"epoch":0,"state":"STOPPED","parts":[],"size":0,"took_ns":1}}` + "'\n"
	kept := func(why string) {
		t.Helper()
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "the running server", string(got), why)
		_, err = os.Stat(target + ".prev")
		assert.True(t, os.IsNotExist(err), why)
	}

	side := candidateAt(t, filepath.Join(dir, "side"), b.side, shadowOK)
	code, out, errs := ta.do("server switch " + side + " --target " + target + b.args())
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "server switch REFUSED: "+side+" was built from commit "+b.side[:12]+", not from the sprint base origin/main (tip "+b.tip[:12]+")")
	assert.Contains(t, errs, "remedy: build nova-sprint from origin/main at its tip, then run: nova-sprint server switch <that binary>")
	assert.Contains(t, errs, "the old server keeps running")
	assert.NotContains(t, out, "SHADOW TICK OK", "refused before the shadow tick")
	kept("side")

	code, _, errs = ta.do("server switch " + side + " --target " + target + " --rollback")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "the base check of "+side+" could not be made")
	assert.Contains(t, errs, "--base <branch>")
	kept("no base named")

	tip := candidateAt(t, filepath.Join(dir, "tip"), b.tip, shadowOK)
	code, out, errs = ta.do("server switch " + tip + " --target " + target + b.args())
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "BASE OK binary="+tip+" commit="+b.tip+" base=origin/main tip="+b.tip)
	assert.Contains(t, out, "SERVER SWITCH OK")
	rec, ok, err := sprint.ReadBaseRecord(target)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, sprint.BaseRecord{Repo: b.repo, Base: "main"}, rec)
}

func TestServerSwitchVerbSwitchesAndRollsBack(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	b := newSwitchBase(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "nova-sprint")
	candidate := filepath.Join(dir, "nova-sprint-candidate")

	require.NoError(t, os.WriteFile(target, []byte("version-1"), 0o755))
	// version 2 passes the canary: its shadow tick prints a plan (shadow.go)
	candidateAt(t, candidate, b.tip, "echo '"+`{"shadow":{"epoch":0,"state":"STOPPED","parts":[],"size":0,"took_ns":1}}`+"'\n")
	version2, err := os.ReadFile(candidate)
	require.NoError(t, err)

	// Refuses with no args and no --rollback
	code, _, errs := ta.do("server switch")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "wants <binary> [--rollback] or --rollback alone")

	// Switch to candidate with rollback enabled
	code, out, _ := ta.do("server switch " + candidate + " --target " + target + " --rollback" + b.args())
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "SERVER SWITCH OK")

	// Verify target is now version-2 and target.prev is version-1
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, string(version2), string(content))

	prevContent, err := os.ReadFile(target + ".prev")
	require.NoError(t, err)
	assert.Equal(t, "version-1", string(prevContent))

	// Explicit rollback command
	code, out, _ = ta.do("server switch --target " + target + " --rollback")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "SERVER SWITCH ROLLED BACK")

	restored, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "version-1", string(restored))
}
