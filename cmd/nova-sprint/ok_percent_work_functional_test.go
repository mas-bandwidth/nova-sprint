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
// home in the test's own directory and every verb naming the store with --redis): six cards
// are dealt to m1. One is dropped mid-run, one the coordinator's staging refused (a
// coordination end), one the provider refused, one died a 134 environment death, one failed
// the work for real, and one landed ok. The machine's okpct — ok over (ok + failed work +
// no result) — is exactly one failure over the two work ends: the drop, the refusal and the
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

	run("init --readers reader-a --members m1:6")
	run("add --stream s1 --count 6")
	run("fleet beat m1")
	run("start")
	run("tick")
	run("tick") // the fleet update brings m1 up; the next pump deals every card
	s := load()
	require.Equal(t, 6, s.Fleet.Count("m1", sprint.Ready), "all six cards are dealt ready")
	run("take --as m1 --limit 6 --epoch 0")
	run("tick") // the take moves the cards working; the finishes below land at the next tick

	// a provider refusal, a 134 environment death, a drop, a staging refusal, one real
	// failure, and one ok land
	run("finish --as m1 s1-1.w1@1 --epoch 0 --failed --report 'provider failure: provider: class=provider-5xx status=503 msg=upstream unavailable'")
	run("finish --as m1 s1-2.w1@1 --epoch 0 --failed --exit 134 --report 'no result: aborted'")
	run("drop s1-3 --reason 'obsolete'")
	run("finish --as m1 s1-4.w1@1 --epoch 0 --failed --report 'staging refused: no bench mirror'")
	run("finish --as m1 s1-5.w1@1 --epoch 0 --head abc123def")
	run("finish --as m1 s1-6.w1@1 --epoch 0 --head abc123def")
	run("tick") // a finish is queued until the next tick drains it, and the ask asks the readers

	// The not-its-fault ends file in their own named columns, never the failed cell: the
	// provider refusal in provider, the 134 death in environment, the staging refusal in
	// coordination, and the drop removes the card.
	s = load()
	provider := inCell(s, sprint.ProviderFailures)
	environment := inCell(s, sprint.Environment)
	coordination := inCell(s, sprint.Coordination)
	assert.True(t, provider["s1-1.w1"], "the provider refusal files in the provider column")
	assert.True(t, environment["s1-2.w1"], "the 134 environment death files in the environment column")
	assert.True(t, coordination["s1-4.w1"], "the staging refusal files in the coordination column")
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-4.w1"} {
		assert.NotContains(t, inCell(s, sprint.DoneFailed), id, "%s is never failed work", id)
		assert.NotContains(t, inCell(s, sprint.Finished), id, "%s is never finished work", id)
	}

	// The readers' verdicts decide the two work ends: a broken read is the one real failure
	// (failed work), an ok read the one ok.
	run("read --as reader-a --broken s1-5.r1.reader-a --finding 'tests red'")
	run("read --as reader-a --ok s1-6.r1.reader-a --finding 'fine'")
	run("tick") // the verdicts move the finished cards to the ok and failed cells

	// The machine's okpct counts the work ends alone: ok over (ok + failed work + no result).
	// One ok and one real failure, so 50%; the provider refusal, the 134 death and the
	// staging refusal are outside the denominator, and the drop is off the table.
	s = load()
	ok := s.Fleet.Count("m1", sprint.DoneOK)
	failed := s.Fleet.Count("m1", sprint.DoneFailed)
	noResult := s.Fleet.Count("m1", sprint.NoResult)
	assert.Equal(t, 1, ok, "the ok card is ok work")
	assert.Equal(t, 1, failed, "exactly one real failure")
	assert.Zero(t, noResult, "no work end left no result")
	work := ok + failed + noResult
	assert.Equal(t, 2, work, "the work ends are the one ok and the one real failure alone")
	assert.Equal(t, 50, 100*ok/work, "okpct is 50%: the not-its-fault ends never count")
}
