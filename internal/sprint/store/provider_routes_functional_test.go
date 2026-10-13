//go:build functional

package store

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/redisacl"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisProvidersRideTheRouteRead(t *testing.T) {
	t.Parallel()
	st, c := liveStore(t)
	ctx := context.Background()
	require.NoError(t, c.SAdd(ctx, config.RoutesKey, "r").Err())
	require.NoError(t, c.HSet(ctx, config.RouteKey("r"), "provider", "p", "tier", "pro", "enabled", "true").Err())
	require.NoError(t, c.SAdd(ctx, config.ProvidersKey, "p", "unfunded").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("p"), "billing", "both", "funded", "true").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("unfunded"), "billing", "metered", "funded", "false").Err())
	require.NoError(t, c.HSet(ctx, config.TierKey("pro"), "routes", "r").Err())
	set, trips, err := st.B.(RouteReader).Routes(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), trips)
	require.Len(t, set.Providers, 2)
	assert.True(t, set.Providers["p"].Funded)
	assert.Equal(t, "both", set.Providers["p"].Billing)
	assert.False(t, set.Providers["unfunded"].Funded)
	assert.Equal(t, []string{"r"}, set.Tiers["pro"])
}

func TestRedisProvidersWithoutRoutesAreStillShown(t *testing.T) {
	t.Parallel()
	st, c := liveStore(t)
	ctx := context.Background()
	require.NoError(t, c.SAdd(ctx, config.ProvidersKey, "p").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("p"), "billing", "plan", "funded", "false").Err())
	require.NoError(t, c.HSet(ctx, config.TierKey("heavy"), "routes", "").Err())
	set, trips, err := st.B.(RouteReader).Routes(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), trips)
	assert.Empty(t, set.Routes)
	assert.Contains(t, set.Tiers, "heavy", "an applied empty routes field preserves the tier row")
	assert.NotContains(t, set.Tiers, "pro", "an absent routes field does not fabricate a configured tier")
	snapshot := &sprint.Snapshot{}
	set.into(snapshot)
	_, tiers := snapshot.ProviderGaps("")
	assert.Equal(t, []string{"heavy"}, tiers)
	assert.Equal(t, "plan", set.Providers["p"].Billing)
	require.NoError(t, c.HDel(ctx, config.ProviderKey("p"), "funded").Err())
	_, _, err = st.B.(RouteReader).Routes(ctx)
	require.ErrorContains(t, err, "missing or invalid funded")
}

func TestRedisRouteReadersCanReadButNotWriteProviderRows(t *testing.T) {
	t.Parallel()
	_, c := liveStore(t)
	ctx := context.Background()
	require.NoError(t, c.SAdd(ctx, config.ProvidersKey, "p").Err())
	require.NoError(t, c.HSet(ctx, config.ProviderKey("p"), "billing", "plan", "funded", "true").Err())
	for _, role := range redisacl.Roles() {
		if role.Name == redisacl.Coordinator {
			continue
		}
		args := []string{"reset", "on", ">provider-test-only", "+@read", "+@write", "+@connection"}
		args = append(args, role.Keys...)
		require.NoError(t, c.ACLSetUser(ctx, "provider-read-"+role.Name, args...).Err())
		r := redis.NewClient(&redis.Options{Addr: c.Options().Addr, Username: "provider-read-" + role.Name, Password: "provider-test-only"})
		t.Cleanup(func() { _ = r.Close() })
		names, err := r.SMembers(ctx, config.ProvidersKey).Result()
		require.NoError(t, err, role.Name)
		assert.Equal(t, []string{"p"}, names)
		hash, err := r.HGetAll(ctx, config.ProviderKey("p")).Result()
		require.NoError(t, err, role.Name)
		assert.Equal(t, "true", hash["funded"])
		require.ErrorContains(t, r.HSet(ctx, config.ProviderKey("p"), "funded", "false").Err(), "NOPERM", role.Name)
	}
}
