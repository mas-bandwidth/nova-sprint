//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// A machine's ok% counts only the work the machine did, never an end it did not cause.
// On a private store (a redis-server of its own, the function library loaded, the app's
// home in the test's own directory and every verb naming the store with --redis): five
// cards are dealt to m1. One is dropped mid-run, one the provider refused, one died a 134
// environment death, one failed the work for real, and one landed ok. The machine's ok%
// counts exactly the one real failure: the drop, the refusal and the death leave the done
// cells, never the failed cell.
func TestOkPercentCountsOnlyTheWorkAMachineDid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	require.NoError(t, admin.SAdd(ctx, config.RoutesKey, "flash-a").Err())
	require.NoError(t, admin.HSet(ctx, config.RouteKey("flash-a"), "name", "flash-a", "tier", "flash", "provider", "p", "model", "m",
		"tokens", "100000", "deadline", "900", "enabled", "true").Err())

	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "seat-ok", "HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(append(split(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	load := func() *sprint.Snapshot {
		t.Helper()
		_, cm := a.verbSetup("log")
		st, err := a.store(*cm)
		require.NoError(t, err)
		s, err := st.Load(ctx, store.All, nil)
		require.NoError(t, err)
		return s
	}

	run("init --readers reader-a --members m1:5")
	run("add --stream s1 --count 5")
	run("fleet beat m1")
	run("start")
	run("tick")
	run("tick") // the fleet update brings m1 up; the next pump deals every card
	s := load()
	require.Equal(t, 5, s.Fleet.Count("m1", sprint.Ready), "all five cards are dealt ready")
	run("take --as m1 --limit 5 --epoch 0")

	// a provider refusal, a 134 environment death, a real failure, an ok land, and a drop
	run("finish --as m1 s1-1.w1@1 --epoch 0 --failed --report 'provider failure: provider: class=provider-5xx status=503 msg=upstream unavailable'")
	run("finish --as m1 s1-2.w1@1 --epoch 0 --failed --report 'exit status 134'")
	run("drop s1-3 --reason 'obsolete'")
	run("finish --as m1 s1-4.w1@1 --epoch 0 --failed --report 'verdict not-done'")
	run("finish --as m1 s1-5.w1@1 --epoch 0 --head abc123def")

	// The machine's ok% counts the work ends alone: ok over (ok + failed work + no result).
	// The provider refusal and the 134 death withdraw the card (never the failed cell), the
	// drop removes it, and only the real failure finishes failed work.
	s = load()
	withdrawn := map[string]bool{}
	for _, fc := range s.Fleet.Cell("m1", sprint.Withdrawn) {
		withdrawn[fc.ID] = true
	}
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-3.w1"} {
		assert.NotContains(t, s.Fleet.Cell("m1", sprint.DoneFailed), id, "%s is never failed work", id)
		assert.NotContains(t, s.Fleet.Cell("m1", sprint.Finished), id, "%s is never finished work", id)
	}
	assert.True(t, withdrawn["s1-1.w1"], "the provider refusal withdraws the card")
	assert.True(t, withdrawn["s1-2.w1"], "the 134 environment death withdraws the card, never failed work")
	failed, okDone := 0, 0
	for _, fc := range s.Fleet.Cell("m1", sprint.Finished) {
		switch fc.ID {
		case "s1-4.w1":
			assert.Equal(t, "no", fc.F("ok"), "the real failure's own word is failed")
			failed++
		case "s1-5.w1":
			assert.Equal(t, "yes", fc.F("ok"), "the ok card's own word is ok")
			okDone++
		}
	}
	assert.Equal(t, 1, failed, "exactly one real failure")
	assert.Equal(t, 1, okDone, "exactly one ok")
}
