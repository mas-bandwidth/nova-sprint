//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// The server's own land loop is machine work, not a coordinator command: it must land
// accepted cards whether or not the seat is hooked (a server restart, a dropped stream, a
// seat away). On a real server and store: the seat hooks in, a card is accepted, the hook
// lapses (its live-until passes with no prove) and the card lands anyway within one tick of
// the loop. A hand land by the coordinator is still refused until the seat hooks in again.
func TestLandingUnhookedLandsOnARealServer(t *testing.T) {
	t.Parallel()
	wall := time.Now()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	require.NoError(t, c.Set(context.Background(), "sprint:git_identity", config.DefaultGitIdentity, 0).Err())
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "landing-unhooked",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	require.Empty(t, func() string { s, _ := a.namedServer(); return s }(), "no sprint server is named in the test's environment")

	dir := t.TempDir()
	remote, worker, clone := filepath.Join(dir, "remote.git"), filepath.Join(dir, "worker"), filepath.Join(dir, "clone")
	g := func(wd string, args ...string) string {
		res, err := gitrun.Run(context.Background(), gitrun.Options{C: wd, Env: []string{"GIT_AUTHOR_NAME=nova-sprint", "GIT_AUTHOR_EMAIL=nova-sprint@example.invalid", "GIT_COMMITTER_NAME=nova-sprint", "GIT_COMMITTER_EMAIL=nova-sprint@example.invalid"}, OwnRepo: wd != ""}, args...)
		require.NoError(t, err, "git %v: %s", args, res.Stderr)
		return strings.TrimSpace(string(res.Stdout))
	}
	g("", "init", "-q", "--bare", "-b", "main", remote)
	g("", "clone", "-q", remote, worker)
	require.NoError(t, os.WriteFile(filepath.Join(worker, "README"), []byte("base\n"), 0o600))
	g(worker, "add", "README")
	g(worker, "commit", "-q", "-m", "base")
	g(worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	g(worker, "fetch", "-q", "origin")
	g("", "clone", "-q", remote, clone)
	a.gitEnv = stripGOFLAGS()
	a.landRoot = func() (string, error) { return filepath.Join(dir, "land"), nil }

	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(split(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	run("init --readers reader-a,reader-b --members m1")
	declareFunctionalRoutes(t, c, "flash")
	// a card's work as a member makes it: one commit on a sprint branch, pushed to origin
	g(worker, "switch", "-q", "--no-track", "-c", "sprint/s1-1", "refs/remotes/origin/main")
	require.NoError(t, os.WriteFile(filepath.Join(worker, "a.txt"), []byte("a\n"), 0o600))
	g(worker, "add", "a.txt")
	g(worker, "commit", "-q", "-m", "work of s1-1")
	head := g(worker, "rev-parse", "HEAD")
	g(worker, "push", "-q", "origin", "refs/heads/sprint/s1-1:refs/heads/sprint/s1-1")

	for _, l := range []string{
		"set --read-cards off", // this script consumes standalone legacy readers

		"add --stream s1 --count 1 --one",
		"fleet beat m1", "start", "tick", "tick",
		"take --as m1 --limit 5 --epoch 0", "finish --as m1 s1-1.w1@1 --head " + head + " --epoch 0 --usage 'input=1000 output=100'",
		"queue --as reader-a", "queue --as reader-b",
		"ask",
		"read --as reader-a --begin --limit 10 --epoch 0", "read --as reader-a --ok --limit 10 --epoch 0 --usage 'input=1000 output=100'",
		"read --as reader-b --begin --limit 10 --epoch 0", "read --as reader-b --ok --limit 10 --epoch 0 --usage 'input=1000 output=100'",
		"accept s1-1", "stream set s1 --land-protected " + sprint.LandProtectedAny,
	} {
		run(l)
	}
	run("tick")

	// the seat's hook lapses: its live-until passes with no prove renewing it. Arm the hook
	// gate for the coordinator with no live hook record, as the server holds it once hooked.
	hookTests.Store("landing-unhooked", true)
	defer hookTests.Delete("landing-unhooked")

	// a hand land by the coordinator is refused while the seat is unhooked
	var out, errb bytes.Buffer
	code := runInto(a, []string{"land", "--repo-dir", clone, "--base", "main", "--redis", addr}, &out, &errb)
	require.NotZero(t, code, "a hand land must be refused while the seat is unhooked")
	require.Contains(t, out.String()+errb.String(), "hook")

	// the server's land loop lands the accepted card within one tick even unhooked
	out.Reset()
	errb.Reset()
	require.Zero(t, a.landRound(context.Background(), addr, []string{"--repo-dir", clone, "--base", "main"}, &out), out.String())
	require.Contains(t, out.String(), "LAND OK stream=s1 cards=1")
	t.Logf("wall time %s", time.Since(wall).Round(time.Millisecond))
}

// stripGOFLAGS is the functional test's git environment: the machine's, minus the test
// runner's GOFLAGS, so a go run of the gate does not inherit it.
func stripGOFLAGS() []string {
	var out []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOFLAGS=") {
			out = append(out, e)
		}
	}
	return out
}
