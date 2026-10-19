//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// A heavy card is dealt on heavy (the seat, 2026-10-12 ~00:05Z: six cards admitted with
// "tier: heavy" on their RESULT line were stored with no tier and dealt on flash). A real
// store (a redis-server of its own, the function library loaded) with a flash route and a
// heavy route as nova-config's apply writes them; a card whose RESULT line says tier: heavy
// is admitted by add, which says tier=heavy, and the tick's first deal of it draws the heavy
// route, never the flash one. The environment is the store's address and the seat's name,
// HOME and XDG_CONFIG_HOME in the test's own directory, nothing else; the app's home is that
// directory and its forward fails the test.
func TestAHeavyCardIsDealtOnAHeavyRouteOnAStore(t *testing.T) {
	t.Parallel()
	began := time.Now()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	for _, x := range [][2]string{{"flash-a", "flash"}, {"heavy-a", "heavy"}} {
		require.NoError(t, admin.SAdd(ctx, config.RoutesKey, x[0]).Err())
		require.NoError(t, admin.HSet(ctx, config.RouteKey(x[0]), "name", x[0], "tier", x[1], "provider", "p-"+x[0], "model", "m",
			"tokens", "100000", "deadline", "900", "enabled", "true").Err())
	}
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "seat-tl", "HOME": home, "XDG_CONFIG_HOME": home}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	// the app's home is the test's own, and its forward fails the test: every verb names the
	// private store with --redis, and none can reach a live sprint server (forward.go)
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	do := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(split(line), "--redis", addr), &out, &errb)
		require.Equal(t, 0, code, "%s: %s%s", line, out.String(), errb.String())
		return out.String()
	}
	brief := filepath.Join(t.TempDir(), "heavy.md")
	require.NoError(t, os.WriteFile(brief, []byte("RESULT: h1-1 sha=000000000000 tier: heavy\n\nThe task.\n\n"+tierLineRules), 0o600))
	do("init --readers reader-a --members m1")
	do("fleet beat m1 --load 0")
	do("fleet up m1 --width 4")
	out := do("add --stream h1 --one h1-1 --brief-file " + brief)
	assert.Contains(t, out, "tier=heavy", "add says the tier it stored")
	do("start")
	do("fleet beat m1 --load 0")
	do("tick")
	st, err := a.store(common{redis: addr, actor: "seat-tl"})
	require.NoError(t, err)
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Fleet}, nil)
	require.NoError(t, err)
	wc := s.Fleet.Card(sprint.WorkCardID("h1-1", 1))
	require.NotNil(t, wc, "the tick dealt it")
	assert.Equal(t, "heavy-a", wc.F(sprint.FieldRoute), "its first deal draws a heavy route")
	assert.Equal(t, "heavy", s.Work.Card("h1-1").F(sprint.FieldTierNow), "dealt on heavy")
	assert.Less(t, time.Since(began), 30*time.Second)
}
