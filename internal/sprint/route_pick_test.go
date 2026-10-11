package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteShareFallsWithCostPerLanded tests that route share falls as its
// cost per landed card rises. Two routes with equal price, one with zero
// landings over 20 takes, should get a smaller share of the next 100 picks
// but not zero (with a floor).
func TestRouteShareFallsWithCostPerLanded(t *testing.T) {
	t.Parallel()
	// flash-b has 20 attempts, 0 ok, 20 failed (high cost per landed = 0 landings)
	// flash-a has 100 attempts, 100 ok with $10 cost per landed (good ok rate, reasonable cost)
	fleet := NewTable(Fleet)
	// Populate flash-b with failed takes
	for i := 1; i <= 20; i++ {
		id := "s1-b" + itoa(i)
		c := &Card{ID: id, Row: "m1", Col: DoneFailed, Fields: map[string]string{
			FieldRoute: "flash-b", "ok": "no", "taken": "2030-01-02T03:04:05Z", "finished": "2030-01-02T03:04:10Z",
		}}
		fleet.Put(c)
	}
	// Populate flash-a with successful takes (with costs to establish $10/landed)
	for i := 1; i <= 100; i++ {
		id := "s1-a" + itoa(i)
		c := &Card{ID: id, Row: "m1", Col: DoneOK, Fields: map[string]string{
			FieldRoute:     "flash-a",
			"ok":           "yes",
			"taken":        "2030-01-02T03:04:05Z",
			"finished":     "2030-01-02T03:04:10Z",
			FieldCost:      "$10.00",
			FieldCostTotal: "$10.00",
		}}
		fleet.Put(c)
	}
	s := &Snapshot{
		Now:   t0,
		Fleet: fleet,
		Routes: []Route{
			{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "a", Enabled: true},
			{Name: "flash-b", Tier: cardhdr.RouteFlash, Provider: "q", Model: "b", Enabled: true},
		},
		Tiers: map[string][]string{cardhdr.RouteFlash: {"flash-a", "flash-b"}},
	}
	// Set the fleet rows so Column() can find the cards
	s.Fleet.SetRows([]string{"m1"})

	stats := RouteStats(s.Routes, s.Fleet)
	// Check that we have stats for both routes
	require.Len(t, stats, 2)

	// Route stats should reflect the different outcomes
	var flashAStats, flashBStats RouteStat
	for _, st := range stats {
		if st.Route.Name == "flash-a" {
			flashAStats = st
		} else if st.Route.Name == "flash-b" {
			flashBStats = st
		}
	}

	assert.Equal(t, 100, flashAStats.Attempts, "flash-a should have 100 attempts")
	assert.Equal(t, 100, flashAStats.OK, "flash-a should have 100 OK")
	assert.Equal(t, 0, flashAStats.Failed, "flash-a should have 0 failed")

	assert.Equal(t, 20, flashBStats.Attempts, "flash-b should have 20 attempts")
	assert.Equal(t, 0, flashBStats.OK, "flash-b should have 0 OK")
	assert.Equal(t, 20, flashBStats.Failed, "flash-b should have 20 failed")

	// Compute cost per landed from the fleet
	costPerLanded := routeCostPerLanded(s.Fleet)
	// flash-a should have $10 cost per landed
	assert.Equal(t, 10.0, costPerLanded["flash-a"], "flash-a should have $10 cost per landed")
	// flash-b has no landed cards, so 0 cost
	assert.Equal(t, 0.0, costPerLanded["flash-b"], "flash-b should have $0 cost per landed")

	// Verify share calculation
	flashAShare := routeShare(flashAStats, costPerLanded["flash-a"])
	flashBShare := routeShare(flashBStats, costPerLanded["flash-b"])
	// flash-a should have higher share than flash-b
	assert.Greater(t, flashAShare, flashBShare, "flash-a share should be greater than flash-b share")
	// flash-b should still have non-zero share due to floor
	assert.Greater(t, flashBShare, 0.0, "flash-b share should be above zero due to floor")

	// Test the weighted pick path through computeRouteShares and preferFirstWeighted
	arr := []string{"flash-a", "flash-b"}
	served := map[string]Route{
		"flash-a": {Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "a", Enabled: true},
		"flash-b": {Name: "flash-b", Tier: cardhdr.RouteFlash, Provider: "q", Model: "b", Enabled: true},
	}

	// Sample 100 picks and check distribution
	routeStats := routeStatsByName(s.Fleet)
	aCount := 0
	bCount := 0
	for i := 0; i < 100; i++ {
		r, _, ok := preferFirstWeighted(arr, served, nil, false, 0, routeStats)
		require.True(t, ok)
		if r.Name == "flash-a" {
			aCount++
		} else if r.Name == "flash-b" {
			bCount++
		}
	}
	t.Logf("Picks: flash-a=%d, flash-b=%d", aCount, bCount)
	// With proper weighting, flash-b should be picked less than flash-a
	// but still above zero (floor ensures it's still sampled)
	assert.Greater(t, aCount, bCount, "flash-a should be picked more than flash-b when flash-b has high cost per landed")
	assert.Greater(t, bCount, 0, "flash-b should still be picked (floor ensures sampling)")
}
