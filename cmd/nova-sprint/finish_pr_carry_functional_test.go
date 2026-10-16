//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// A finish-PR card stages from the PR's own head, not from its base (two of the first
// finish-PR attempts, finish-nova-sprint-pr-113 and finish-nova-sprint-pr-132, came back
// as one new commit off main, not built on the PR branch the brief named). The card's
// brief carries the machine-read CARRY: line `brief --widen` writes, and a real store
// (a redis-server of its own, the function library loaded, the environment scrubbed to
// the store, the actor and a home of the test's own) admits it: the deal's packet names
// the PR head as the base the slot is staged from, and a commit built on that staged
// checkout has the PR head as an ancestor. The app's home is the test's own and its
// forward fails the test: every verb names the private store with --redis, and none can
// reach a live sprint server.
func TestAFinishPRCardStagesFromThePRHeadOnAStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))

	// a bare origin, main, and a PR branch whose head is a commit on main
	dir := t.TempDir()
	remote, work := filepath.Join(dir, "origin.git"), filepath.Join(dir, "work")
	gitEnv := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_PARAMETERS='maintenance.auto'='false'")
	git := func(d string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("", "init", "-q", "--bare", "-b", "main", remote)
	git("", "clone", "-q", remote, work)
	require.NoError(t, os.WriteFile(filepath.Join(work, "a.go"), []byte("package a\n"), 0o600))
	git(work, "add", "a.go")
	git(work, "commit", "-q", "-m", "a at the base")
	git(work, "push", "-q", "origin", "HEAD:refs/heads/main")
	git(work, "switch", "-q", "-c", "pr-1", "main")
	require.NoError(t, os.WriteFile(filepath.Join(work, "a.go"), []byte("package a\n\n// the PR's change\n"), 0o600))
	git(work, "add", "a.go")
	git(work, "commit", "-q", "-m", "the PR's work")
	prHead := git(work, "rev-parse", "HEAD")
	git(work, "push", "-q", "origin", "HEAD:refs/heads/pr-1")

	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "seat", "HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	do := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(split(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}

	// the finish-PR brief: THE TASK names the PR's branch, the CARRY: line holds its head,
	// and the base (main) is still merged in by the worker
	brief := filepath.Join(t.TempDir(), "pr-1.md")
	body := "RESULT: pr-1 sha=000000000000 tier: pro\n" +
		"REPO: " + remote + "\n" +
		"BASE: main\n" +
		"PATHS: a.go\n" +
		"PR: finish-nova-sprint-pr-1\n" +
		"CARRY: head=" + prHead + " branch=pr-1\n" +
		"\nTHE TASK. Start from the PR's branch pr-1 at " + prHead + " and finish it, merging the base in.\n"
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief(body)), 0o600))

	do("init --readers reader-a --members m1")
	do("fleet beat m1 --load 0")
	do("fleet up m1 --width 4")
	// the card is cut on main, a protected branch: the first add marks its new stream
	do("add --stream s1 --one pr-1 --land-protected any --brief-file " + brief)
	do("start")
	do("tick")

	var take struct {
		Packets []member.Packet `json:"packets"`
	}
	require.NoError(t, json.Unmarshal([]byte(do("take --as m1 pr-1.w1@1 --epoch 0 --json")), &take))
	require.Len(t, take.Packets, 1)
	p := member.Carried(take.Packets[0])
	assert.Equal(t, prHead, p.BaseHead, "the slot is staged from the PR head, not the base")
	require.NotEmpty(t, p.Branch, "the packet names the work branch")

	// stage the slot as the launcher does: a clone, the PR head fetched and checked out
	slot := filepath.Join(dir, "slot")
	git("", "clone", "-q", remote, slot)
	git(slot, "fetch", "-q", "origin", "pr-1")
	git(slot, "checkout", "-q", "--detach", p.BaseHead)
	assert.Equal(t, prHead, git(slot, "rev-parse", "HEAD"), "the staged slot's HEAD is the PR head")

	// the worker's push: a commit built on the staged head, pushed to the card's branch
	git(slot, "switch", "-q", "-c", p.Branch)
	require.NoError(t, os.WriteFile(filepath.Join(slot, "a.go"), []byte("package a\n\n// the PR's change\n\n// finished\n"), 0o600))
	git(slot, "add", "a.go")
	git(slot, "commit", "-q", "-m", "finish the PR")
	pushed := git(slot, "rev-parse", "HEAD")
	git(slot, "push", "-q", "origin", "HEAD:refs/heads/"+p.Branch)

	cmd := exec.Command("git", "merge-base", "--is-ancestor", prHead, pushed)
	cmd.Dir = slot
	cmd.Env = gitEnv
	require.NoError(t, cmd.Run(), "the PR head %s is an ancestor of the pushed %s", prHead, pushed)
}
