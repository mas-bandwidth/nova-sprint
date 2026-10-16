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
	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// TestANewStreamLandsOnItsOwnBranchWithNoSetting runs a real landing on a private store
// with default landing settings (no --base, no --land-protected): two cards cut on dev, added to a
// new stream s, are finished, accepted and landed, and origin gains sprint/s holding both
// heads, oldest first, while origin's dev is unchanged (docs/SPEC-SPRINT.md section 7, the
// sprint branch).
func TestANewStreamLandsOnItsOwnBranchWithNoSetting(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	// One route of the tier the cards are, and the machine's commit identity, as
	// nova-config's apply writes them: the lander reads the identity with the routes
	// (docs/SPEC-SPRINT.md, the machine's commit identity).
	require.NoError(t, c.SAdd(context.Background(), config.RoutesKey, "flash-a").Err())
	require.NoError(t, c.HSet(context.Background(), config.RouteKey("flash-a"), "name", "flash-a", "tier", "flash", "provider", "p", "model", "m", "tokens", "100000", "deadline", "900", "enabled", "true").Err())
	require.NoError(t, c.HSet(context.Background(), config.TierKey("flash"), "name", "flash", "routes", "flash-a").Err())
	require.NoError(t, c.Set(context.Background(), config.SprintKey(config.FieldGitIdentity), config.DefaultGitIdentity, 0).Err())

	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Owner\n\temail = owner@example.com\n"), 0o600))
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }

	// A bare origin with a dev branch, and a worker's clone, as a member's work is staged.
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
	git(worker, "push", "-q", "origin", "HEAD:refs/heads/dev")
	git(worker, "fetch", "-q", "origin")
	git("", "clone", "-q", remote, clone)

	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, split(line), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}

	// Two cards cut on dev, worked and pushed by the member.
	heads := map[string]string{}
	for _, id := range []string{"s-1", "s-2"} {
		git(worker, "switch", "-q", "--no-track", "-c", "sprint/"+id, "refs/remotes/origin/dev")
		require.NoError(t, os.WriteFile(filepath.Join(worker, id+".txt"), []byte(id+"\n"), 0o600))
		git(worker, "add", id+".txt")
		git(worker, "commit", "-q", "-m", "work of "+id)
		git(worker, "push", "-q", "origin", "refs/heads/sprint/"+id+":refs/heads/sprint/"+id)
		heads[id] = git(worker, "rev-parse", "HEAD")
	}
	devTip := git(remote, "rev-parse", "dev")

	briefFile := func(id string) string {
		path := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(path, []byte(passingBrief("RESULT: "+id+" sha=000000000000 tier: flash\nREPO: "+remote+"\nBASE: dev\n\nWrite "+id+".txt.")), 0o600))
		return path
	}

	run("init --readers reader-a,reader-b --members m1")
	run("set --read-cards off") // this landing fixture consumes standalone legacy readers
	run("add --stream s s-1 --one --brief-file " + briefFile("s-1"))
	run("add --stream s s-2 --one --brief-file " + briefFile("s-2"))
	run("fleet beat m1")
	run("start")
	run("tick")
	run("tick")
	run("take --as m1 --limit 5 --epoch 0")
	run("finish --as m1 s-1.w1@1 --head " + heads["s-1"] + " --epoch 0")
	run("finish --as m1 s-2.w1@1 --head " + heads["s-2"] + " --epoch 0")
	run("queue --as reader-a")
	run("queue --as reader-b")
	run("tick")
	run("read --as reader-a --begin --limit 10 --epoch 0")
	run("read --as reader-a --ok --limit 10 --epoch 0 --usage 'input=10 cache_read=0 cache_write=0 output=10 model=p/m'")
	run("read --as reader-b --begin --limit 10 --epoch 0")
	run("read --as reader-b --ok --limit 10 --epoch 0 --usage 'input=10 cache_read=0 cache_write=0 output=10 model=p/m'")
	run("accept s-1 s-2")
	run("land --repo-dir " + clone)

	// origin gains sprint/s holding both heads, oldest first; dev is unchanged.
	log := git(remote, "log", "--first-parent", "--format=%s", "sprint/s")
	require.Equal(t, "land s-2 (sprint stream s)", firstLineOf(log), "the newest head is on top: %s", log)
	require.Contains(t, log, "land s-1 (sprint stream s)", "both heads landed: %s", log)
	require.Equal(t, devTip, git(remote, "rev-parse", "dev"), "origin's dev is unchanged")
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
