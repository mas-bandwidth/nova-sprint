//go:build functional

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerFunctionalApp(t *testing.T) (*functionalTestApp, *redis.Client) {
	t.Helper()
	ctx := context.Background()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, fn.Load(ctx, c))
	a := newFunctionalTestApp(t, addr)
	a.a.cardCheckGate = true
	a.runVerb("init", "--readers", "reader-a", "--members", "m1")
	return a, c
}

func TestRedisFullCheckNamesFundedRouteGapsAndLiveStartFundingChange(t *testing.T) {
	t.Parallel()
	a, c := providerFunctionalApp(t)
	ctx := context.Background()
	require.NoError(t, c.SAdd(ctx, config.ProvidersKey, "p").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("p"), "billing", "plan", "funded", "true").Err())
	var names []string
	for i := 1; i <= 7; i++ {
		n := fmt.Sprintf("r%d", i)
		names = append(names, n)
		require.NoError(t, c.SAdd(ctx, config.RoutesKey, n).Err())
		require.NoError(t, c.HSet(ctx, config.RouteKey(n), "tier", "pro", "provider", "p", "enabled", "true").Err())
	}
	require.NoError(t, c.HSet(ctx, config.TierKey("pro"), "routes", "r1").Err())
	out, code, errs := a.runVerbOut("check", "cards")
	assert.Equal(t, 1, code, out+errs)
	assert.Equal(t, 6, strings.Count(out, "CHECK routes r"))
	require.NoError(t, c.HSet(ctx, config.TierKey("pro"), "routes", strings.Join(names, ",")).Err())
	a.runVerb("check", "cards")
	require.NoError(t, c.HSet(ctx, config.ProviderKey("p"), "funded", "false").Err())
	out, code, errs = a.runVerbOut("start") // direct gate, never runOnStore's synthetic record helper
	assert.Equal(t, 0, code, out+errs)
	assert.Contains(t, errs, "CHECK routes tier:pro holds no route of a funded provider")
	assert.Contains(t, errs, "WARN 1 provider finding(s); the machine starts anyway")
	require.NoError(t, c.HSet(ctx, config.ProviderKey("p"), "funded", "true").Err())
	a.runVerb("check", "cards")
	a.runVerb("start")
}

func TestRedisFullCheckNamesAbsentAndMalformedProvidersOnce(t *testing.T) {
	t.Parallel()
	a, c := providerFunctionalApp(t)
	ctx := context.Background()
	out, code, errs := a.runVerbOut("check", "cards")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, out, "CHECK routes - no provider rows applied")
	require.NoError(t, c.SAdd(ctx, config.ProvidersKey, "missing", "invalid", "unpopulated").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("missing"), "billing", "plan").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("invalid"), "billing", "plan", "funded", "garbage").Err())
	out, code, errs = a.runVerbOut("check", "cards")
	assert.Equal(t, 1, code, out+errs)
	for _, n := range []string{"missing", "invalid", "unpopulated"} {
		assert.Equal(t, 1, strings.Count(out, "CHECK routes provider:"+n+" "))
	}
	assert.NotContains(t, out, "no provider rows applied")
	where := a.runVerb("where")
	assert.Contains(t, where, "invalid unknown")
	assert.Contains(t, where, "missing unknown")
	assert.Contains(t, where, "unpopulated unknown")
}
