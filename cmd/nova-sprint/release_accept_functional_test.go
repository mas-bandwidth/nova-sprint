//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// The release acceptance set (docs/TESTS.md, "Release acceptance"; make release-accept): the
// checks a point release passed by a ten-minute live watch on the server and a canary are
// tests here, each on its own store, in parallel. This file holds the ones that need a real
// store or the release's binary; the rest are named in the Makefile's RELEASE_ACCEPT list.
// Each test pins behaviour through the verbs, never internals, so it runs unchanged against
// the next release.

// ReleaseBinEnv names the nova-sprint binary the canary runs: the build a release ships.
// Unset, the test builds this tree's binary into its own temp directory.
const ReleaseBinEnv = "NOVA_SPRINT_BIN"

// releaseStore is a fresh real store (redis-server on a loopback port of its own, the
// function library loaded) and an app on it acting as actor.
func releaseStore(t *testing.T, actor string) (string, *app, func(args ...string) string, func(args ...string) (int, string, string)) {
	t.Helper()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, fn.Load(context.Background(), c))
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": actor}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	run := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := a.run(args, &out, &errb)
		return code, out.String(), errb.String()
	}
	do := func(args ...string) string {
		t.Helper()
		code, out, errs := run(args...)
		require.Equal(t, 0, code, "%v: exit %d\n%s%s", args, code, out, errs)
		return out
	}
	return addr, a, do, run
}

// releaseCard is a card's view on the store.
func releaseCard(t *testing.T, do func(args ...string) string, id string) cardView {
	t.Helper()
	var v cardView
	out := do("card", id, "--json")
	require.NoError(t, json.Unmarshal([]byte(out), &v), out)
	require.NotNil(t, v.Primary, out)
	return v
}

// TestReleaseAcceptTicksOfABusySprintNeverFail is the ticks check: a busy sprint on a real
// store (four members of width 4, three readers, three streams of 80 cards), the world played one round between
// ticks (takes, finishes with failures, reads with broken verdicts), and every one of the
// ticks exits 0, never panics and never prints a refusal of its drain; the store's own
// invariants hold after the run (check).
func TestReleaseAcceptTicksOfABusySprintNeverFail(t *testing.T) {
	t.Parallel()
	const ticks = 40
	_, _, do, run := releaseStore(t, "coordinator")
	do("init", "--readers", "reader-a,reader-b,reader-c", "--members", "m1:4,m2:4,m3:4,m4:4")
	do("add", "--stream", "a,b,c", "--count", "80")
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		do("fleet", "beat", m, "--load", "0")
	}
	do("start")
	moved := 0
	for i := 1; i <= ticks; i++ {
		var code int
		var out, errs string
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("tick %d panicked: %v", i, p)
				}
			}()
			code, out, errs = run("tick")
		}()
		require.Equal(t, 0, code, "tick %d failed: %s%s", i, out, errs)
		assert.NotContains(t, out+errs, "REFUSED drain", "tick %d refused its drain: %s%s", i, out, errs)
		assert.NotContains(t, out+errs, "panic", "tick %d: %s%s", i, out, errs)
		if strings.Contains(out, "MOVED") {
			moved++
		}
		// the world: one round of the members and readers (bare merges are a twin's, so
		// cards stop at merging on a real store, which is where this check ends)
		code, out, errs = run("play", "--every", "0s", "--ticks", "1", "--broken", "0.1", "--fail", "0.1",
			"--stuck", "0", "--cross", "0", "--down", "0", "--up", "0")
		require.Equal(t, 0, code, "play after tick %d: %s%s", i, out, errs)
	}
	// the world drains the deal in about twenty ticks; the rest tick a sprint whose cards
	// wait on merges a real store does not take bare, which is still a tick that must not fail
	assert.GreaterOrEqual(t, moved, ticks/4, "a busy sprint moves cards tick after tick:\n%s", do("where"))
	code, out, errs := run("check")
	assert.Equal(t, 0, code, "check after %d ticks: %s%s", ticks, out, errs)
}

// TestReleaseAcceptNeedsHoldACardOffTheDealOnTheStore is the needs check on a real store
// (#96; the unit tests in internal/sprint pin the rest): a card dealt before its need was
// named goes back working -> waiting at the next tick, is never in a member's take, and a
// take of it by id is refused.
func TestReleaseAcceptNeedsHoldACardOffTheDealOnTheStore(t *testing.T) {
	t.Parallel()
	_, _, do, run := releaseStore(t, "coordinator")
	do("init", "--readers", "reader-a,reader-b", "--members", "m1:4")
	do("add", "--stream", "s1", "--count", "2")
	do("fleet", "beat", "m1", "--load", "0")
	do("start")
	do("tick")
	do("tick")
	require.Equal(t, "working", releaseCard(t, do, "s1-2").Primary.Col, "both cards are dealt first")
	do("needs", "set", "s1-2", "--needs", "s1-1")
	do("tick")
	assert.Equal(t, "waiting", releaseCard(t, do, "s1-2").Primary.Col, "a dealt card whose need has not landed is withdrawn to waiting")
	assert.Equal(t, "working", releaseCard(t, do, "s1-1").Primary.Col)
	var take output
	require.NoError(t, json.Unmarshal([]byte(do("take", "--as", "m1", "--limit", "4", "--epoch", "0", "--json")), &take))
	for _, p := range take.Packets {
		assert.NotEqual(t, "s1-2", p.Primary, "a card with unlanded needs is never taken: %+v", p)
	}
	for range 2 {
		do("tick")
		assert.Equal(t, "waiting", releaseCard(t, do, "s1-2").Primary.Col, "never dealt while its need is unlanded")
	}
	code, out, errs := run("take", "--as", "m1", "s1-2.w1@1", "--epoch", "0")
	assert.Contains(t, out+errs, "s1-2", "a take by id of the withdrawn card says so: exit %d", code)
	assert.Equal(t, "waiting", releaseCard(t, do, "s1-2").Primary.Col)
}

// releaseBinary is the binary under acceptance: NOVA_SPRINT_BIN, else this tree's, built
// once per test binary.
var releaseBuild struct {
	once sync.Once
	path string
	err  error
	out  []byte
}

func releaseBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv(ReleaseBinEnv); p != "" {
		_, err := os.Stat(p)
		require.NoError(t, err, "%s names no binary", ReleaseBinEnv)
		return p
	}
	releaseBuild.once.Do(func() {
		dir, err := os.MkdirTemp("", "release-accept-bin-")
		if err != nil {
			releaseBuild.err = err
			return
		}
		releaseBuild.path = filepath.Join(dir, "nova-sprint")
		cmd := exec.Command("go", "build", "-o", releaseBuild.path, ".")
		releaseBuild.out, releaseBuild.err = cmd.CombinedOutput()
	})
	require.NoError(t, releaseBuild.err, "go build: %s", releaseBuild.out)
	return releaseBuild.path
}

// TestReleaseAcceptCanaryOneMemberFinishesOneCardOnTheBuild is the canary: one member, the
// release's own binary, takes the one card dealt to it on a real store and finishes it ok,
// and the next tick moves the card to review with its work card done.
func TestReleaseAcceptCanaryOneMemberFinishesOneCardOnTheBuild(t *testing.T) {
	t.Parallel()
	bin := releaseBinary(t)
	addr, _, do, _ := releaseStore(t, "coordinator")
	do("init", "--readers", "reader-a,reader-b", "--members", "m1:1")
	do("add", "--stream", "canary", "--count", "1", "--one")
	do("fleet", "beat", "m1", "--load", "0")
	do("start")
	do("tick")
	do("tick")
	member := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = append(os.Environ(), "NOVA_SPRINT_REDIS="+addr, "NOVA_SPRINT_ACTOR=m1")
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		require.NoError(t, err, "%s %v: %s%s", bin, args, out.String(), errb.String())
		return out.String()
	}
	var take output
	raw := member("take", "--as", "m1", "--limit", "1", "--epoch", "0", "--json")
	require.NoError(t, json.Unmarshal([]byte(raw), &take), raw)
	require.Len(t, take.Packets, 1, "the member takes the one card: %s", raw)
	p := take.Packets[0]
	assert.Equal(t, "canary-1", p.Primary)
	head := strings.Repeat("c", 40)
	fin := member("finish", "--as", "m1", p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0", "--head", head, "--report", "canary")
	assert.Contains(t, fin, "done ok", fin)
	do("tick")
	v := releaseCard(t, do, "canary-1")
	assert.Equal(t, "review", v.Primary.Col, "the finished card waits on its reads")
	require.NotEmpty(t, v.Work)
	w := v.Work[len(v.Work)-1]
	assert.Contains(t, []string{"finished", "done"}, w.Col, fmt.Sprintf("%+v", w))
	assert.Equal(t, "yes", w.Fields["ok"], "the work card finished ok: %+v", w)
}
