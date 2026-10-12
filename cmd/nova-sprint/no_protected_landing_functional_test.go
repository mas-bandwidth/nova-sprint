//go:build functional

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// No verb puts a card on a protected branch, dev or main (docs/SPEC-SPRINT.md section 7,
// the protected branches): `stream set --promotion`, `stream set --base dev` and
// `add --land-protected any` are each refused, exit 2, quoting sprint.LandingFlow; and a
// store row written by hand with land_protected=any and a queued card landed on dev is
// refused by land before any git, origin's dev and main unchanged.
func TestNoVerbPutsACardOnDevOrMain(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	// A route and the sprint row's git_identity, as nova-config's apply writes them: the
	// lander reads the identity with the routes (the sprint row is read only when a route
	// exists) and refuses to commit without one.
	require.NoError(t, c.SAdd(context.Background(), config.RoutesKey, "flash-a").Err())
	require.NoError(t, c.HSet(context.Background(), config.RouteKey("flash-a"), "name", "flash-a", "tier", "flash", "provider", "p", "model", "m", "enabled", "true").Err())
	require.NoError(t, c.Set(context.Background(), config.SprintKey(config.FieldGitIdentity), config.DefaultGitIdentity, 0).Err())

	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }

	// a local bare origin with dev and main, and a worker's clone
	dir := t.TempDir()
	remote, worker, clone := filepath.Join(dir, "remote.git"), filepath.Join(dir, "worker"), filepath.Join(dir, "clone")
	git := func(wd string, args ...string) string {
		t.Helper()
		res, err := gitrun.Run(context.Background(), gitrun.Options{C: wd}, args...)
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

	run := func(line string) (string, int, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := runInto(a, append(split(line), "--redis", addr), &out, &errb)
		return out.String(), code, errb.String()
	}
	runOK := func(line string) string {
		t.Helper()
		out, code, errb := run(line)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out, errb)
		return out
	}

	runOK("init --readers reader-a,reader-b --members m1")
	runOK("add --stream s1 --count 1 --one")
	devTip := git(remote, "rev-parse", "dev")
	mainTip := git(remote, "rev-parse", "main")

	// the three verbs are refused, exit 2, quoting the flow, nothing written
	for _, line := range []string{
		"stream set s1 --promotion",
		"stream set s1 --base dev",
		"add --stream s2 x1 --one --land-protected any",
	} {
		out, code, errb := run(line)
		require.Equal(t, 2, code, "%s: %s%s", line, out, errb)
		require.Contains(t, out+errb, sprint.LandingFlow, line)
	}
	require.Equal(t, devTip, git(remote, "rev-parse", "dev"), "dev unchanged")
	require.Equal(t, mainTip, git(remote, "rev-parse", "main"), "main unchanged")

	// a card queued and finished, its head pushed to a branch, then accepted
	git(worker, "switch", "-q", "--no-track", "-c", "sprint/s1-1", "refs/remotes/origin/dev")
	require.NoError(t, os.WriteFile(filepath.Join(worker, "a.txt"), []byte("a\n"), 0o600))
	git(worker, "add", "a.txt")
	git(worker, "commit", "-q", "-m", "work of s1-1")
	head := git(worker, "rev-parse", "HEAD")
	git(worker, "push", "-q", "origin", "refs/heads/sprint/s1-1:refs/heads/sprint/s1-1")
	git(worker, "fetch", "-q", "origin")

	runOK("fleet beat m1")
	runOK("start")
	runOK("tick")
	runOK("tick")
	runOK("take --as m1 --limit 5 --epoch 0")
	runOK("finish --as m1 s1-1.w1@1 --head " + head + " --epoch 0")
	runOK("queue --as reader-a")
	runOK("queue --as reader-b")
	runOK("tick")
	runOK("read --as reader-a --begin --limit 10 --epoch 0")
	runOK("read --as reader-a --ok --limit 10 --epoch 0 --usage 'input=1 output=1'")
	runOK("read --as reader-b --begin --limit 10 --epoch 0")
	runOK("read --as reader-b --ok --limit 10 --epoch 0 --usage 'input=1 output=1'")
	runOK("accept --read-ok")

	// a store row written by hand: land_protected=any, as an old store carries it (the verb
	// that wrote it is refused now), and the queued card landed on dev is refused anyway
	markStreamByHand(t, a, addr, "s1")
	out, code, errb := run("land --repo-dir " + clone + " --base dev")
	require.NotZero(t, code, "land on dev: %s%s", out, errb)
	require.Contains(t, out+errb, sprint.LandingFlow, "land on dev quotes the flow")
	require.Equal(t, devTip, git(remote, "rev-parse", "dev"), "nothing pushed to dev")
	require.Equal(t, mainTip, git(remote, "rev-parse", "main"), "nothing pushed to main")
}

// markStreamByHand writes land_protected=any directly on a stream's control card, the store
// row an old store carried before the mark stopped being a landing permission
// (docs/SPEC-SPRINT.md section 7, the protected branches): the verb that wrote it
// (`stream set --land-protected any`) is refused now.
func markStreamByHand(t *testing.T, a *app, addr, stream string) {
	t.Helper()
	st, err := a.store(common{redis: addr, actor: "coordinator"})
	require.NoError(t, err)
	step := store.Step{
		Verb: "raw mark",
		Load: []string{sprint.Merge},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			ctl := s.StreamCtl(stream)
			return sprint.Plan{Units: []sprint.Unit{{Key: ctl.ID, Stream: stream,
				Changes: []sprint.Change{{Table: sprint.Merge, Entry: ntable.BatchMemberEntry{
					ID:     ctl.ID,
					Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(ctl.Rev, 10), Place: &ntable.PlaceExpect{Row: ctl.Row, Col: ctl.Col}},
					Set:    map[string]string{sprint.FieldLandProtected: sprint.LandProtectedAny},
				}}}}}}
		},
	}
	_, err = st.Run(context.Background(), step)
	require.NoError(t, err)
}
