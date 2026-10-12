package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// The routes read on a store with no routes: the sprint row's nova-decide bars
// and git_identity are read even when SMEMBERS returns empty, exactly as the
// twin's Routes returns its bars, so a store with no routes does not lose the
// landing's git identity (and the decide bars the tick answers by).

// TestRedisRoutesWithNoRoutes pins the shape of an empty store: no routes, no
// tiers, no bars, and one round trip (the set and the bars ride the one
// pipeline).
func TestRedisRoutesWithNoRoutes(t *testing.T) {
	t.Parallel()
	r, ctx := coverRedis(t, 0)
	set, trips, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Empty(t, set.Routes, "no routes when none are configured")
	assert.Empty(t, set.Tiers, "no tiers when no routes")
	assert.Equal(t, Bars{}, set.Bars, "no bars when none are applied")
	assert.Equal(t, int64(1), trips, "the set and the bars read in one round trip")
}

// TestRedisRoutesWithNoRoutesAndBars pins that the sprint row's bars are read
// even when no route exists: a store with no routes still returns git_identity
// and the decide bars, matching the twin's behavior (routes.go:257).
func TestRedisRoutesWithNoRoutesAndBars(t *testing.T) {
	t.Parallel()
	r, ctx := coverRedis(t, 0)
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldGitIdentity), "Test User <test@example.com>", 0).Err())
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldDecideBounce), "0.5", 0).Err())
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldDecideReview), "0.3", 0).Err())
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldAnswerRulesOff), "rule-a,rule-b", 0).Err())
	set, trips, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Empty(t, set.Routes)
	assert.Equal(t, "Test User <test@example.com>", set.Bars.GitIdentity)
	assert.Equal(t, "0.5", set.Bars.Bounce)
	assert.Equal(t, "0.3", set.Bars.Review)
	assert.Equal(t, "rule-a,rule-b", set.Bars.RulesOff)
	assert.Equal(t, int64(1), trips, "the bars ride the one pipeline with the set")
}

// TestRedisRoutesWithNoRoutesMatchesTheTwin pins that a Redis store with no
// routes reads the same bars the twin's Routes returns for the same applied
// bars: the two stores never disagree about git_identity or the decide bars.
func TestRedisRoutesWithNoRoutesMatchesTheTwin(t *testing.T) {
	t.Parallel()
	m := NewMem()
	m.SetDecideBars(Bars{Bounce: "0.5", Review: "0.3"})
	m.SetRulesOff("rule-a")
	m.SetGitIdentity("Test User <test@example.com>")
	twin, _, err := m.Routes(context.Background())
	require.NoError(t, err)

	r, ctx := coverRedis(t, 0)
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldGitIdentity), "Test User <test@example.com>", 0).Err())
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldDecideBounce), "0.5", 0).Err())
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldDecideReview), "0.3", 0).Err())
	require.NoError(t, r.C.Set(ctx, config.SprintKey(config.FieldAnswerRulesOff), "rule-a", 0).Err())
	redis, trips, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), trips)
	assert.Empty(t, redis.Routes)
	assert.Empty(t, redis.Tiers)
	assert.Equal(t, twin.Bars, redis.Bars, "the Redis store with no routes reads the bars the twin returns")
}

// TestRedisRoutesTripsNoRoutes pins the store's round trips with no routes:
// one (the set and the bars ride the one pipeline), where a store with routes
// makes two.
func TestRedisRoutesTripsNoRoutes(t *testing.T) {
	t.Parallel()
	r, ctx := coverRedis(t, 0)
	set, trips, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Empty(t, set.Routes)
	assert.Equal(t, int64(1), trips)
}
