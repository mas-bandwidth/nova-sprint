//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// The start-of-tick level on a real store (docs/SPEC-SPRINT.md, the rebalance;
// the owner, 2026-10-11 ~19:05-19:20Z: "cards only may accrue per-fleet or
// friend, if there are not other machines/friends ready to do that work right
// now!"; observed that day: batman 0/4 and superman 2/8 stayed low while the
// level moved the cards dealt to them, within a second of the deal, onto
// members that were busier). A private redis-server: m1 (width 4) and m2
// (width 16) are dealt twelve cards; m1 takes all its cards but one and m2
// takes every card, so m1 holds one ready card it will start at its next pass
// and m2 holds fewer cards than m1 less their widths. The ticks that follow
// leave that card on m1 at its generation (round.levelTo; tla/Level.tla
// NeverOffAFreeLane (lands with #127)). Then m1's width falls to 2 while it holds four cards:
// it cannot start its ready card, and the next tick moves it onto m2, the
// member with a free lane.
func TestTheStartLevelLeavesADealtCardOnAMemberThatCanStartItOnTheStore(t *testing.T) {
	t.Parallel()
	wall := time.Now()
	ctx := context.Background()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(ctx, c))
	// A scrubbed environment: the private store, the actor, and a home and
	// config directory of the test's own; no other variable of the machine's
	// (no NOVA_SPRINT_SERVER, no seat record) is seen. Every verb names the
	// private store with --redis, which never forwards (forward.go), and the
	// app's forward fails the test: no verb can reach a live sprint server.
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	require.Empty(t, func() string { s, _ := a.namedServer(); return s }(), "no sprint server is named in the test's environment")
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(strings.Fields(line), "--redis", addr), &out, &errb)
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

	for _, l := range []string{"init --readers reader-a,reader-b --members m1:4,m2:16", "add --stream s1 --count 12",
		"fleet beat m1", "fleet beat m2", "start",
		"tick", "tick"} { // the fleet update brings the members up; the next pump deals
		run(l)
	}
	s := load()
	d1, d2 := s.Fleet.Count("m1", sprint.Ready), s.Fleet.Count("m2", sprint.Ready)
	require.GreaterOrEqual(t, d1, 1, "the deal gave m1 a card (m1 %d, m2 %d)", d1, d2)
	require.Equal(t, 12, d1+d2, "every card is dealt (m1 %d, m2 %d)", d1, d2)

	// m1 takes all its cards but one, m2 takes every card it was dealt
	if d1 > 1 {
		run("take --as m1 --limit " + strconv.Itoa(d1-1) + " --epoch 0")
	}
	run("take --as m2 --limit " + strconv.Itoa(d2) + " --epoch 0")
	s = load()
	left := s.Fleet.Cell("m1", sprint.Ready)
	require.Len(t, left, 1, "m1 holds one ready card")
	id, gen := left[0].ID, left[0].Int("gen")
	require.Equal(t, d1-1, s.Fleet.Count("m1", sprint.Working))
	require.Equal(t, d2, s.Fleet.Count("m2", sprint.Working))

	// 1. m1 is at or below its width: it starts the card at its next pass, so
	// no tick moves it, whatever m2's backlog (the mean rule moved it to m2)
	for i := 0; i < 2; i++ {
		out := run("tick")
		require.NotContains(t, out, id+" m1:ready -> ", "tick %d levelled m1's card off a member that can start it:\n%s", i+1, out)
	}
	s = load()
	require.Equal(t, "m1", s.Fleet.Card(id).Row, "%s stays on m1", id)
	require.Equal(t, gen, s.Fleet.Card(id).Int("gen"), "%s stays at its generation", id)

	// 2. m1's width falls to 2 while it holds d1 cards: it cannot start its
	// ready card, and m2 has a free lane, so the card goes to m2
	require.Greater(t, d1, 2, "the deal gave m1 %d cards: the over-width half needs 3", d1)
	run("fleet up m1 --width 2")
	run("tick")
	s = load()
	require.Equal(t, "m2", s.Fleet.Card(id).Row, "%s moves off m1, over its width, onto m2's free lane", id)
	require.Greater(t, s.Fleet.Card(id).Int("gen"), gen, "%s is dealt again at a new generation", id)
	t.Logf("wall time %s", time.Since(wall).Round(time.Millisecond))
}
