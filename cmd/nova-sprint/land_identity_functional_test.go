//go:build functional

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// TestLandCommitIdentityOnARealStore runs a real landing on a private store, with an explicit
// --redis and a scrubbed environment (HOME and XDG_CONFIG_HOME in a TempDir with a planted
// global git config, no NOVA_*), and reads the landing commit's author and committer from
// `git log --format='%an <%ae>|%cn <%ce>'`: they are the sprint row's git_identity, never the
// planted global config (docs/SPEC-SPRINT.md, the machine's commit identity).
func TestLandCommitIdentityOnARealStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	// The sprint row's git_identity, as nova-config's apply writes it.
	require.NoError(t, c.Set(context.Background(), config.SprintKey(config.FieldGitIdentity), config.DefaultGitIdentity, 0).Err())

	// A scrubbed environment: HOME and XDG_CONFIG_HOME in a TempDir with a planted global git
	// config naming a different identity, and no NOVA_* variable of the machine's.
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Owner\n\temail = owner@example.com\n"), 0o600))
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "land-identity",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }

	// A bare origin and a worker's clone, as a member's work is staged.
	dir := t.TempDir()
	remote, worker, clone := filepath.Join(dir, "remote.git"), filepath.Join(dir, "worker"), filepath.Join(dir, "clone")
	git := func(d string, args ...string) string {
		t.Helper()
		res, err := gitrun.Run(context.Background(), gitrun.Options{C: d, Env: []string{"HOME=" + home, "GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=worker", "GIT_AUTHOR_EMAIL=worker@example.com", "GIT_COMMITTER_NAME=worker", "GIT_COMMITTER_EMAIL=worker@example.com"}, OwnRepo: d != ""}, args...)
		require.NoError(t, err, "git %v: %s", args, res.Stderr)
		return strings.TrimSpace(string(res.Stdout))
	}
	git("", "init", "-q", "--bare", "-b", "main", remote)
	git("", "clone", "-q", remote, worker)
	require.NoError(t, os.WriteFile(filepath.Join(worker, "README"), []byte("base\n"), 0o600))
	git(worker, "add", "README")
	git(worker, "commit", "-q", "-m", "base")
	git(worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	git(worker, "fetch", "-q", "origin")
	git("", "clone", "-q", remote, clone)

	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(strings.Fields(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}

	// One card, worked and pushed by the member, then taken through the sprint to the merge
	// queue and landed.
	git(worker, "switch", "-q", "--no-track", "-c", "sprint/s1-1", "refs/remotes/origin/main")
	require.NoError(t, os.WriteFile(filepath.Join(worker, "a.txt"), []byte("a\n"), 0o600))
	git(worker, "add", "a.txt")
	git(worker, "commit", "-q", "-m", "work of s1-1")
	head := git(worker, "rev-parse", "HEAD")
	git(worker, "push", "-q", "origin", "refs/heads/sprint/s1-1:refs/heads/sprint/s1-1")

	run("init --readers reader-a,reader-b --members m1")
	run("set --read-cards off") // this landing fixture consumes standalone legacy readers
	run("add --stream s1 --count 1 --one")
	run("fleet beat m1")
	run("start")
	run("tick")
	run("tick")
	run("take --as m1 --limit 5 --epoch 0")
	run("finish --as m1 s1-1.w1@1 --head " + head + " --epoch 0")
	run("queue --as reader-a")
	run("queue --as reader-b")
	run("ask")
	run("read --as reader-a --ok --limit 10 --epoch 0")
	run("read --as reader-b --ok --limit 10 --epoch 0")
	run("accept --read-ok")
	run("stream set s1 --land-protected any")
	run("tick")
	run("land --repo-dir " + clone + " --base main")

	// The landing commit's author and committer are the configured identity, not the planted
	// global config and not the worker's.
	got := git(remote, "log", "-1", "--format=%an <%ae>|%cn <%ce>", "main")
	require.Equal(t, "nova-sprint <nova-sprint@example.invalid>|nova-sprint <nova-sprint@example.invalid>", got)
}
