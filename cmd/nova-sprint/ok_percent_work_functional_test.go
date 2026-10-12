//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
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
// coordination end; a move of a card mid-run is refused, so it records no end at all), one
// the provider refused, one died a 134 environment death, one failed the work for real, and
// one landed ok. The machine's stored okpct — the field the dashboard's ok% cell reads, ok
// over (ok + failed work + no result) — is exactly one failure over the two work ends: the
// drop, the refused move, the provider refusal, the death and the staging refusal are never
// failed work (the staging refusal files in coordination; the refusal and the death return to
// ready at the same attempt; the drop and the refused move record no end).
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
	refused := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(append(split(line), "--redis", addr), &out, &errb)
		require.NotZero(t, code, "%s: expected a refusal\n%s%s", line, out.String(), errb.String())
		return errb.String()
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
	run("queue --as reader-a") // a reader beats by asking for its own queue: reader-a is up
	run("start")
	run("tick")
	run("tick") // the fleet update brings m1 up; the next pump deals every card
	s := load()
	require.Equal(t, 6, s.Fleet.Count("m1", sprint.Ready), "all six cards are dealt ready")
	run("take --as m1 --limit 6 --epoch 0")
	run("tick") // the take moves the cards working; the finishes below land at the next tick

	// a card moved mid-run is refused: the machine is RUNNING, so the coordinator's move
	// records no end and never fails the machine (the RULE's "a card moved mid-run").
	moveErr := refused("move s1-4 --stream s2")
	assert.Contains(t, moveErr, "the machine is RUNNING", "a move of a card mid-run is refused: %s", moveErr)

	// a provider refusal, a 134 environment death, a drop, a staging refusal, one real
	// failure, and one ok land
	run("finish --as m1 s1-1.w1@1 --epoch 0 --failed --report 'provider failure: provider: class=provider-5xx status=503 msg=upstream unavailable'")
	run("finish --as m1 s1-2.w1@1 --epoch 0 --failed --exit 134 --report 'no result: aborted'")
	run("drop s1-3 --reason 'obsolete'")
	run("finish --as m1 s1-4.w1@1 --epoch 0 --failed --report 'staging refused: no bench mirror'")
	run("finish --as m1 s1-5.w1@1 --epoch 0 --head abc123def")
	run("finish --as m1 s1-6.w1@1 --epoch 0 --head abc123def")
	run("tick") // a finish is queued until the next tick drains it, and the ask asks the readers

	// The not-its-fault ends never file in the failed cell: the staging refusal files in
	// coordination, the provider refusal and the 134 death return to ready at the same
	// attempt (their take recorded in their own named columns, never a failure), and the
	// drop removes the card.
	s = load()
	assert.True(t, inCell(s, sprint.Coordination)["s1-4.w1"], "the staging refusal files in the coordination column")
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-4.w1"} {
		assert.NotContains(t, inCell(s, sprint.DoneFailed), id, "%s is never failed work", id)
		assert.NotContains(t, inCell(s, sprint.Finished), id, "%s is never finished work", id)
	}

	// The readers' verdicts decide the two work ends: a broken read is the one real failure
	// (failed work), an ok read the one ok.
	run("read --as reader-a --broken s1-5.r1.reader-a --epoch 0 --finding 'internal/x.go:1: tests red' --usage 'input=10 cache_read=0 cache_write=0 output=10 model=p/m'")
	run("read --as reader-a --ok s1-6.r1.reader-a --epoch 0 --finding 'fine' --usage 'input=10 cache_read=0 cache_write=0 output=10 model=p/m'")
	run("tick") // the verdicts move the finished cards to the ok and failed cells

	// The machine's stored okpct field — the field the dashboard's ok% cell reads — counts
	// the work ends alone: ok over (ok + failed work + no result). One ok and one real
	// failure, so 50.0%; the provider refusal, the 134 death and the staging refusal are
	// outside the denominator, and the drop and the refused move are off the table.
	s = load()
	assert.Equal(t, 1, s.Fleet.Count("m1", sprint.DoneOK), "the ok card is ok work")
	assert.Equal(t, 1, s.Fleet.Count("m1", sprint.DoneFailed), "exactly one real failure")
	assert.Zero(t, s.Fleet.Count("m1", sprint.NoResult), "no work end left no result")
	var view struct {
		Tables map[string]map[string]map[string]any `json:"tables"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("where --json")), &view))
	assert.Equal(t, "50.0%", view.Tables["fleet"]["m1"]["okpct"], "the stored okpct field counts only the work ends")
}
