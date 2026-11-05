//go:build functional

package main

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// workerWall is the stand-in for nova-sandbox: it drops the wall's own words and runs
// what follows its "--", as the wall does once it has walled the child.
const workerWall = `#!/bin/sh
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift
exec "$@"
`

// workerHarness is the stand-in for opencode: `models` lists none; `run` works the card
// its --title names: ok* commits and writes RESULT.md, fail* dies, slow* outlives its
// deadline.
const workerHarness = `#!/bin/sh
[ "$1" = models ] && exit 0
title=
while [ "$#" -gt 0 ]; do [ "$1" = --title ] && title=$2; shift; done
case "$title" in
ok*)
	cd repo || exit 9
	echo done > work.txt
	git add work.txt && git commit -q -m "the work" || exit 8
	head=$(git rev-parse HEAD)
	printf 'head: %s\nbranch: -\nverdict: ok\ngate: -\noutput: -\nreport: did the work\n' "$head" > ../RESULT.md
	;;
fail*)
	echo "harness: boom"
	exit 1
	;;
slow*)
	echo "harness: thinking"
	sleep 120
	;;
*)
	printf 'head: -\nbranch: -\nverdict: ok\ngate: -\noutput: -\nreport: reads clean\n' > RESULT.md
	;;
esac
`

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// nova-sprint worker against a private store and the real server: three cards taken by
// the worker's pass, each run by the stand-in harness under the stand-in wall. The one
// that commits is pushed to origin's branch and finished with its head; the one whose
// harness dies is finished failed with its exit; the one that outlives its deadline is
// ended at it and finished failed for it.
func TestTheWorkerTakesRunsAndFinishesOverTheServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))

	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	seed := filepath.Join(dir, "seed")
	require.NoError(t, exec.Command("git", "init", "-q", "--bare", "-b", "main", origin).Run())
	require.NoError(t, exec.Command("git", "init", "-q", "-b", "main", seed).Run())
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README"), []byte("seed\n"), 0o644))
	gitIn(t, seed, "add", "README")
	gitIn(t, seed, "commit", "-q", "-m", "seed")
	gitIn(t, seed, "push", "-q", origin, "main", "main:sprint/s1")

	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	wall, harness := filepath.Join(bin, "wall"), filepath.Join(bin, "opencode")
	require.NoError(t, os.WriteFile(wall, []byte(workerWall), 0o755))
	require.NoError(t, os.WriteFile(harness, []byte(workerHarness), 0o755))
	briefs := filepath.Join(dir, "briefs")
	require.NoError(t, os.MkdirAll(briefs, 0o755))
	for name, deadline := range map[string]string{"ok1": "600", "fail1": "600", "slow1": "2"} {
		brief := name + ": the work (s1) tier: pro\nmodel: x/y\ntokens: 5000\ndeadline: " + deadline + "\nREPO: " + origin + "\nBASE: sprint/s1\n\nDo the work.\n\n" + swarm.ChildRulesParagraph() + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(briefs, name+".md"), []byte(brief), 0o644))
	}

	env := map[string]string{"NOVA_SPRINT_ACTOR": "boss", "HOME": dir, "NOVA_SPRINT_REDIS": addr}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	a.cardCheckGate = false
	do := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		require.Equal(t, 0, runOnStore(a, addr, args, &out, &errb), "%v: %s %s", args, out.String(), errb.String())
		return out.String()
	}
	do("init", "--members", "m1:4", "--readers", "reader-m1,reader-x")
	do("set", "--read-cards", "off")
	do("add", "--stream", "s1", "--brief-dir", briefs)
	do("fleet", "beat", "m1", "--load", "0")
	do("fleet", "up", "m1")
	do("start")
	a.serveAddr = addr
	a.lanes = &serveLanes{off: true}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	st, _, code := a.machineVerb("run", nil, &bytes.Buffer{})
	require.NotNil(t, st, "run exit=%d", code)
	loopCtx, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.runLoop(loopCtx, st, 20, 0, io.Discard, io.Discard) }()
	t.Cleanup(func() { stop(); wg.Wait() })

	root := filepath.Join(dir, "worker")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "identity.tsv"), []byte("owner\tname\temail\nswarm\tWorker 1\tworker-1@example.com\n"), 0o644))
	wenv := map[string]string{"HOME": dir, ServerEnv: strings.TrimPrefix(srv.URL, "http://")}
	w := newApp(func(k string) string { return wenv[k] })
	t.Cleanup(w.close)
	w.workerWall, w.workerTicks = wall, 200
	w.after = func(time.Duration) <-chan time.Time { return time.After(20 * time.Millisecond) }
	var out, errb bytes.Buffer
	require.Equal(t, 0, runInto(w, []string{"worker", "--as", "m1", "--harness", harness, "--root", root}, &out, &errb), "%s%s", out.String(), errb.String())
	log := out.String()
	t.Log(log)

	assert.NotContains(t, log, "not in the readers table", "the work row is no reader")
	assert.Contains(t, log, "start ok1.w1 attempt=1 gen=1")
	assert.Contains(t, log, "finish ok1.w1 ok=true exit=0")
	assert.Contains(t, log, "finish fail1.w1 ok=false exit=1")
	assert.Contains(t, log, "finish slow1.w1 ok=false")
	assert.Contains(t, log, "NOTE finish slow1.w1 failed: deadline: ")

	branch := gitIn(t, origin, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads/sprint/ok1*")
	require.NotEmpty(t, branch, "the commit is on origin's branch")
	_, head, _ := strings.Cut(branch, " ")
	assert.Equal(t, "the work", gitIn(t, origin, "log", "-1", "--format=%s", head))
	story := do("card", "ok1")
	assert.Contains(t, story, "head "+head)
	assert.Contains(t, story, "pushed="+head)
	assert.Contains(t, do("card", "fail1"), "no RESULT.md shape")
	assert.Contains(t, do("card", "slow1"), "deadline: ")
	ents, err := os.ReadDir(filepath.Join(root, "work", "slots"))
	require.NoError(t, err)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	assert.NotContains(t, strings.Join(names, " "), "ok1.w1", "a launch that did its work is removed")
	assert.Contains(t, strings.Join(names, " "), "fail1.w1", "a failed launch is kept")
}
