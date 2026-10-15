//go:build functional

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
)

// --- a landing stalled on ledger-only conflicts (#100) ---

// newLandRigOnTheStore is newLandRig on a real store: the coordinator's verbs, the lander
// among them, on a redis-server of its own beside the rig's local git origin.
func newLandRigOnTheStore(t *testing.T) *landRig {
	t.Helper()
	addr, _, _, _ := releaseStore(t, "coordinator")
	r := &landRig{testApp: newTestApp(t), dir: t.TempDir()}
	r.live = nil       // the members beat on the real store, by the verb
	r.a.now = time.Now // the store's clock: a beat and the deal read one time
	r.a.backend = func(ctx context.Context, _ string, names sprint.Names) (store.Backend, error) {
		return r.a.redisBackend(ctx, addr, names)
	}
	r.env = append(slices.DeleteFunc(os.Environ(), func(e string) bool { return strings.HasPrefix(e, "GOFLAGS=") }), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=lander", "GIT_AUTHOR_EMAIL=lander@example.invalid", "GIT_COMMITTER_NAME=lander", "GIT_COMMITTER_EMAIL=lander@example.invalid")
	r.remote, r.worker, r.clone = filepath.Join(r.dir, "remote.git"), filepath.Join(r.dir, "worker"), filepath.Join(r.dir, "clone")
	r.git("", "init", "-q", "--bare", "-b", "main", r.remote)
	r.git("", "clone", "-q", r.remote, r.worker)
	r.commit("README", "base\n", "base")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.git("", "clone", "-q", r.remote, r.clone)
	r.a.gitEnv = r.env
	r.a.landRoot = func() (string, error) { return filepath.Join(r.dir, "land"), nil }
	r.ok("init --readers reader-a,reader-b --members m1")
	r.ok("set --read-cards off") // this ledger fixture consumes standalone legacy readers
	r.ok("fleet beat m1 --load 0")
	r.ok("fleet up m1") // up on a stopped machine, as the mem twin's beat leaves it
	// A route and the sprint row's git_identity, as nova-config's apply writes them: the
	// lander reads the identity with the routes (the sprint row is read only when a route
	// exists) and refuses to commit without one.
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.SAdd(context.Background(), config.RoutesKey, "flash-a").Err())
	require.NoError(t, c.HSet(context.Background(), config.RouteKey("flash-a"), "name", "flash-a", "tier", "flash", "provider", "p", "model", "m", "enabled", "true").Err())
	require.NoError(t, c.Set(context.Background(), config.SprintKey(config.FieldGitIdentity), config.DefaultGitIdentity, 0).Err())
	return r
}

// TestStallALedgerOnlyConflictLandsOnTheStore is #100 on a real store: about 300 landing
// refusals against 35 landings in an hour of the live sprint, most of them conflicts in the
// shared ledgers alone (tla/RUNS.tsv, tla/COVERAGE.tsv), so streams stalled in merging. Two
// cards each add a row to both ledgers; both land, the second with both sides' rows, and both
// cards reach landed.
func TestStallALedgerOnlyConflictLandsOnTheStore(t *testing.T) {
	t.Parallel()
	r := newLandRigOnTheStore(t)
	const runs = "config\tmodule\tstarted_utc\tresult\n"
	const cov = "machine\tcode\tmodule\tstatus\towed_by\tnote\n"
	run := func(cfg string) string { return cfg + "\tM.tla\t2026-10-11T00:00:00Z\tPASS\n" }
	row := func(m string) string { return m + "\tinternal/x.go\t-\tmissing\t-\tnote " + m + "\n" }
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the ledgers", map[string]string{"tla/RUNS.tsv": runs + run("MCBase.cfg"), "tla/COVERAGE.tsv": cov + row("base")})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	side := func(id string) map[string]string {
		return map[string]string{
			"tla/RUNS.tsv":     runs + run("MCBase.cfg") + run("MC"+id+".cfg"),
			"tla/COVERAGE.tsv": cov + row("base") + row(id),
		}
	}
	heads := map[string]string{"s1-1": r.card("s1-1", side("one")), "s1-2": r.card("s1-2", side("two"))}
	// The queue, as r.queued does it, but with the readers' queue after the ask: on a real
	// store a reader's read card is asked to it and sits held until the reader queues, so a
	// read before the queue leaves the card asked and the accept sees no ok read.
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(2)
	r.ok("take --as m1 --limit 100")
	r.ok("finish --as m1 s1-1.w1@1 --head " + heads["s1-1"])
	r.ok("finish --as m1 s1-2.w1@1 --head " + heads["s1-2"])
	r.ok("queue --as reader-a") // a reader's queue is its beat, before the ask
	r.ok("queue --as reader-b")
	r.ok("ask")
	r.ok("queue --as reader-a") // and again: pulls the asked read card into its queue
	r.ok("queue --as reader-b")
	r.ok("read --as reader-a --ok --max 100 --epoch 0 --usage 'input=1 cache_read=0 cache_write=0 output=1 model=p/m'")
	r.ok("read --as reader-b --ok --max 100 --epoch 0 --usage 'input=1 cache_read=0 cache_write=0 output=1 model=p/m'")
	r.ok("accept --read-ok")
	r.markProtected()
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	require.Equal(t, 0, code, out+errs)
	require.Contains(t, out, "LAND OK stream=s1 cards=2 base=main", out+errs)
	assert.Equal(t, strings.TrimSuffix(runs+run("MCBase.cfg")+run("MCone.cfg")+run("MCtwo.cfg"), "\n"), r.git(r.remote, "show", "main:tla/RUNS.tsv"), "both sides' rows")
	assert.Equal(t, strings.TrimSuffix(cov+row("base")+row("one")+row("two"), "\n"), r.git(r.remote, "show", "main:tla/COVERAGE.tsv"), "both sides' rows")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"), "no card stalls in merging")
}
