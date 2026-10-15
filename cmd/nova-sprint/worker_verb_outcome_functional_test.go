//go:build functional

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
	"github.com/mas-bandwidth/nova-tools/pkg/testredis"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// The worker's verbs carry their outcome over the real server and a private
// store, named with an explicit --redis on every verb and a scrubbed
// environment (no NOVA_SPRINT_REDIS to fall back on): the server's answer tells
// a completed verb (accepted) from a definite refusal (refused) without the
// worker reading the printed words (docs/SPEC-SPRINT.md, the server;
// docs/TESTS.md, "every verb names the private store with --redis").
//
// The unknown outcome is the store's word that a write was not confirmed
// (ErrUnknown), that a step was cut (CutError), or that a clear closed the step
// as it finished (ClearedError with Finished). A healthy private store confirms
// every write and cuts nothing, so no worker's verb over it answers unknown
// from the store. The server's own unknown — a caller gone before its verb ran
// (goneResult) — is checked below over the real store and the server's entry
// point; it cannot go over the wire, because the gone caller by definition
// reads no answer. The store's typed unknown paths are covered by
// TestResultOutcomeTable (worker_verb_outcome_test.go).
func TestWorkerVerbOutcomeOverRealServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	// the environment names no store: every verb is pointed at the private one
	// with --redis, so no seat record or live server on the host can take a verb
	env := func(k string) string {
		return map[string]string{"NOVA_SPRINT_ACTOR": "boss"}[k]
	}
	a := newApp(env)
	t.Cleanup(a.close)
	do := func(args ...string) {
		t.Helper()
		args = append(append([]string{}, args...), "--redis", addr)
		var out, errb bytes.Buffer
		require.Equal(t, 0, runOnStore(a, addr, args, &out, &errb), "%v: %s %s", args, out.String(), errb.String())
	}
	do("init", "--members", "m1:2,m2:2", "--readers", "reader-a,reader-b")
	do("add", "--stream", "s1", "--count", "4")
	for i := 1; i <= 2; i++ {
		m := "m" + strconv.Itoa(i)
		do("fleet", "beat", m, "--load", "0")
		do("fleet", "up", m)
	}
	do("start")
	do("tick")
	a.serveAddr = addr
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	link := testredis.FarLink(t, strings.TrimPrefix(srv.URL, "http://"), time.Millisecond)
	remote := sprintwire.Client{Addr: link.Addr(), HTTP: &http.Client{Timeout: 30 * time.Second}}

	send := func(verbs ...[]string) []sprintwire.Result {
		t.Helper()
		res, err := remote.Do(ctx, verbs...)
		require.NoError(t, err)
		return res
	}

	accepted := send([]string{"take", "--as", "m1", "--limit", "1", "--epoch", "0"})[0]
	require.Equal(t, 0, accepted.Code, accepted.Stderr)
	require.Equal(t, "accepted", accepted.Outcome)
	require.Equal(t, "yes", accepted.Changed)

	// the budget verbs are worker verbs the server runs too: a successful
	// take/renew/release is accepted over the real server, never refused/no.
	paid := send([]string{"budget", "take", "m1/gpt", "--as", "m1", "--holder", "h1"})[0]
	require.Equal(t, 0, paid.Code, paid.Stderr)
	require.Equal(t, "accepted", paid.Outcome)
	require.Equal(t, "yes", paid.Changed)

	refused := send([]string{"finish", "--as", "m1", "s1-1.w1@99", "--epoch", "0"})[0]
	require.Equal(t, "refused", refused.Outcome)
	require.Equal(t, "no", refused.Changed)

	// the unknown outcome over the real server: a caller gone before its verbs
	// ran answers the goneResult path, unknown/no, on the beat and read lanes
	// alike (serve.go, servelanes.go)
	gctx, gcancel := context.WithCancel(ctx)
	gcancel()
	gone := a.serveCtx(gctx, sprintwire.Request{Verbs: [][]string{{"friend", "beat", "amy"}, {"queue", "--as", "m1"}}}, false)
	require.Len(t, gone.Results, 2)
	for _, res := range gone.Results {
		require.Equal(t, 2, res.Code, res.Stderr)
		require.Contains(t, res.Stderr, "its caller went away while it waited")
		require.Equal(t, "unknown", res.Outcome)
		require.Equal(t, "no", res.Changed)
	}
}
