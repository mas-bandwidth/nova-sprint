//go:build functional

package main

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// On a real Redis store, every route `nova-sprint routes` prints is resolved by
// `routes rest`, `routes wake` and `routes limit` with and without --dry-run,
// including disabled `pro-glm53-openrouter`; a route listed from the cards that
// is no longer in the store's `routes` set is refused with the gone reason
// naming `nova-config apply --kind route` instead of "names no route".
func TestRoutesRestResolvesListedRouteOnAStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, fn.Load(ctx, c))

	ta := newFunctionalTestApp(t, addr)
	ta.runVerb("init", "--readers", "reader-a,reader-b", "--members", "m1")

	// Apply two routes to the store first so a card can be dealt on pro-gone-openrouter,
	// then drop pro-gone-openrouter from the store's `routes` set and keep disabled
	// pro-glm53-openrouter in the store's `routes` set.
	require.NoError(t, c.SAdd(ctx, config.ProvidersKey, "openrouter").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("openrouter"), "billing", "metered", "funded", "true").Err())
	require.NoError(t, c.SAdd(ctx, config.RoutesKey, "pro-gone-openrouter", "pro-glm53-openrouter").Err())
	require.NoError(t, c.HSet(ctx, config.RouteKey("pro-gone-openrouter"),
		"tier", "flash", "provider", "openrouter", "model", "z-ai/glm-5.3-old", "enabled", "true").Err())
	require.NoError(t, c.HSet(ctx, config.RouteKey("pro-glm53-openrouter"),
		"tier", "pro", "provider", "openrouter", "model", "z-ai/glm-5.3", "enabled", "false").Err())

	ta.runVerb("fleet", "beat", "m1", "--load", "0")
	ta.runVerb("fleet", "up", "m1", "--width", "4")
	ta.runVerb("add", "--stream", "s1", "--count", "1", "--one")
	runOnStore(ta.app(), addr, []string{"start"}, &nopWriter{}, &nopWriter{})
	ta.runVerb("fleet", "beat", "m1", "--load", "0")
	ta.runVerb("tick")

	// Remove pro-gone-openrouter from the store's route set while the dealt work card still names it.
	require.NoError(t, c.SRem(ctx, config.RoutesKey, "pro-gone-openrouter").Err())
	require.NoError(t, c.Del(ctx, config.RouteKey("pro-gone-openrouter")).Err())

	routesOut := ta.runVerb("routes")
	require.Contains(t, routesOut, "ROUTE pro-glm53-openrouter model=openrouter/z-ai/glm-5.3 tier=pro enabled=false")
	require.Contains(t, routesOut, "ROUTE pro-gone-openrouter model=openrouter/z-ai/glm-5.3-old gone")

	// Disabled store route pro-glm53-openrouter resolves with and without --dry-run.
	out, code, errs := ta.runVerbOut("routes", "rest", "pro-glm53-openrouter", "--reason", "r", "--dry-run")
	assert.Zero(t, code, "%s%s", out, errs)
	assert.Contains(t, out, "WOULD routes pro-glm53-openrouter rest until woken")

	out = ta.runVerb("routes", "rest", "pro-glm53-openrouter", "--reason", "r")
	assert.Contains(t, out, "MOVED routes pro-glm53-openrouter rest until woken")

	out, code, errs = ta.runVerbOut("routes", "wake", "pro-glm53-openrouter", "--reason", "r", "--dry-run")
	assert.Zero(t, code, "%s%s", out, errs)
	assert.Contains(t, out, "WOULD routes pro-glm53-openrouter woken")

	out = ta.runVerb("routes", "wake", "pro-glm53-openrouter", "--reason", "r")
	assert.Contains(t, out, "MOVED routes pro-glm53-openrouter woken")

	out, code, errs = ta.runVerbOut("routes", "limit", "pro-glm53-openrouter", "--rpm", "10", "--reason", "r", "--dry-run")
	assert.Zero(t, code, "%s%s", out, errs)
	assert.Contains(t, out, "model openrouter/z-ai/glm-5.3 requests per minute 10")

	out = ta.runVerb("routes", "limit", "pro-glm53-openrouter", "--rpm", "10", "--reason", "r")
	assert.Contains(t, out, "model openrouter/z-ai/glm-5.3 requests per minute 10")

	// Gone route pro-gone-openrouter is refused with the gone reason, never "names no route".
	const wantGone = "pro-gone-openrouter is listed from the cards that ran on it but is not in the sprint's route set: nova-config apply --kind route copies it, or rest its provider"
	for _, argv := range [][]string{
		{"routes", "rest", "pro-gone-openrouter", "--reason", "r", "--dry-run"},
		{"routes", "rest", "pro-gone-openrouter", "--reason", "r"},
		{"routes", "wake", "pro-gone-openrouter", "--reason", "r", "--dry-run"},
		{"routes", "wake", "pro-gone-openrouter", "--reason", "r"},
		{"routes", "limit", "pro-gone-openrouter", "--rpm", "10", "--reason", "r", "--dry-run"},
		{"routes", "limit", "pro-gone-openrouter", "--rpm", "10", "--reason", "r"},
	} {
		out, code, errs := ta.runVerbOut(argv...)
		combined := out + errs
		assert.Equal(t, 1, code, "%v:\n%s", argv, combined)
		assert.Contains(t, combined, wantGone, "%v:\n%s", argv, combined)
		assert.NotContains(t, combined, "names no route", "%v:\n%s", argv, combined)
	}
}

type nopWriter struct{}

func (*nopWriter) Write(p []byte) (int, error) { return len(p), nil }
