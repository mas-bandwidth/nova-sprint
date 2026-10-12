package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedisRoutesWithNoRoutes tests that a Redis store with no routes still reads
// git_identity and the decide bars, matching the twin's behavior.
func TestRedisRoutesWithNoRoutes(t *testing.T) {
	t.Parallel()
	r, ctx := coverRedis(t, 0)
	set, _, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Empty(t, set.Routes, "no routes when none are configured")
	assert.Empty(t, set.Tiers, "no tiers when no routes")
	// The bars are still read even without routes
	assert.Empty(t, set.Bars.Bounce)
	assert.Empty(t, set.Bars.Review)
	assert.Empty(t, set.Bars.GitIdentity)
}

// TestRedisRoutesWithNoRoutesAndBars tests that bars set via sprint keys are returned
// even when there are no routes.
func TestRedisRoutesWithNoRoutesAndBars(t *testing.T) {
	t.Parallel()
	r, ctx := coverRedis(t, 0)
	// Directly set sprint keys on the underlying db
	r.C.(*coverRedisClient).db.keys["sprint:git_identity"] = &coverVal{kind: coverString, s: "Test User <test@example.com>"}
	r.C.(*coverRedisClient).db.keys["sprint:decide_bounce"] = &coverVal{kind: coverString, s: "bounce"}
	r.C.(*coverRedisClient).db.keys["sprint:decide_review"] = &coverVal{kind: coverString, s: "review"}
	set, _, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Empty(t, set.Routes)
	assert.Equal(t, "Test User <test@example.com>", set.Bars.GitIdentity)
	assert.Equal(t, "bounce", set.Bars.Bounce)
	assert.Equal(t, "review", set.Bars.Review)
}

// TestRedisRoutesTripsNoRoutes verifies that the store's trips==2 test still passes
// when there are no routes (the trips should be 1 when routes are empty but bars still
// read on the first pipeline trip).
func TestRedisRoutesTripsNoRoutes(t *testing.T) {
	t.Parallel()
	r, ctx := coverRedis(t, 0)
	set, trips, err := r.Routes(ctx)
	require.NoError(t, err)
	assert.Empty(t, set.Routes)
	assert.Equal(t, int64(1), trips)
}
