package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAReadNeverDrawsARestingRouteWhileAnotherServes: with the read mix's used models
// avoided and the only route left resting, a read draws a route that serves, its model
// used again, never the resting one. Before, the walk lapsed the rests with the avoided
// models and drew the resting route; the next tick's take-back retired the read by rest,
// which spends no reader, and the deal asked it of the next member, every tick (2026-11-05:
// deterministic-desync-local's and r3-l3-model-lists's third reads went round the fleet on
// heavy-minimaxm3-direct, retired_by=rest, 3123 asks in 20 minutes). The clock is 1000 s.
func TestAReadNeverDrawsARestingRouteWhileAnotherServes(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	s := &Snapshot{
		Now:   now,
		Fleet: NewTable(Fleet),
		Work:  NewTable(Work),
		Routes: []Route{
			{Name: "heavy-rest", Tier: "heavy", Provider: "r", Model: "rm", Enabled: true},
			{Name: "heavy-used", Tier: "heavy", Provider: "u", Model: "um", Enabled: true},
		},
		Tiers:     map[string][]string{"heavy": {"heavy-rest", "heavy-used"}},
		Providers: map[string]Provider{"r": {Name: "r", Funded: true}, "u": {Name: "u", Funded: true}},
	}
	s.rests = map[string]RouteRest{"heavy-rest": {Route: "heavy-rest", Provider: "r", At: now, Until: now.Add(RouteRestFor), Cards: []string{"x"}, Cause: RestProvider}}
	pr := &Card{ID: "h-1", Fields: map[string]string{"brief": "tier: heavy\n"}}

	set := s.readRouteOf(routeIndexesOf(s), pr, []string{"heavy-used"})
	require.NotEmpty(t, set[FieldRoute])
	assert.Equal(t, "heavy-used", set[FieldRoute], "the avoided model lapses, the rest does not")
	_, rests := s.resting(set[FieldRoute])
	assert.False(t, rests, "a read is never dealt on a resting route while one serves")

	// unchanged: nothing avoided, the resting route is left out as before
	assert.Equal(t, "heavy-used", s.readRouteOf(routeIndexesOf(s), pr, nil)[FieldRoute])
}
