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
// home in the test's own directory and every verb naming the store with --redis): cards are
// dealt to m1. One is dropped mid-run, one the provider refused, one died a 134 environment
// death, one failed the work for real, and one landed ok. The machine's ok% counts exactly
// the one real failure over (ok + failed work + no result): the drop, the refusal and the
// death file in their own named columns, never the failed cell.
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
	inCell := func(s *sprint.Snapshot, col string) map[string]bool {
		m := map[string]bool{}
		for _, fc := range s.Fleet.Cell("m1", col) {
			m[fc.ID] = true
		}
		return m
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
	run("tick") // the take moves the cards working; the finishes below land at the next tick

	// a provider refusal, a 134 environment death, a drop, a real failure, and an ok land
	run("finish --as m1 s1-1.w1@1 --epoch 0 --failed --report 'provider failure: provider: class=provider-5xx status=503 msg=upstream unavailable'")
	run("finish --as m1 s1-2.w1@1 --epoch 0 --failed --exit 134 --report 'no result: aborted'")
	run("drop s1-3 --reason 'obsolete'")
	run("finish --as m1 s1-4.w1@1 --epoch 0 --failed --report 'verdict not-done'")
	run("finish --as m1 s1-5.w1@1 --epoch 0 --head abc123def")
	run("tick") // a finish is queued until the next tick drains it

	// The machine's ok% counts the work ends alone: ok over (ok + failed work + no result).
	// The provider refusal files in provider, the 134 death in environment, the drop removes
	// the card, and only the real failure finishes failed work.
	s = load()
	provider := inCell(s, sprint.ProviderFailures)
	environment := inCell(s, sprint.Environment)
	assert.True(t, provider["s1-1.w1"], "the provider refusal files in the provider column")
	assert.True(t, environment["s1-2.w1"], "the 134 environment death files in the environment column")
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-3.w1"} {
		assert.NotContains(t, inCell(s, sprint.DoneFailed), id, "%s is never failed work", id)
		assert.NotContains(t, inCell(s, sprint.Finished), id, "%s is never finished work", id)
	}
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

	// ok% is ok over (ok + failed work + no result), the work ends alone: the refusal, the
	// death and the drop are outside the denominator, so the machine's ok% counts exactly the
	// one failure and the one ok, nothing else.
	work := okDone + failed + len(inCell(s, sprint.NoResult))
	assert.Equal(t, 2, work, "the work ends are the one ok and the one real failure alone")
	assert.Equal(t, 1, failed, "ok% counts exactly one failure")
}
