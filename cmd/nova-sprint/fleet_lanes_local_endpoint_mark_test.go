package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestFleetTrackMarksLocalEndpointLanes checks that lanes carry route information
// when the machine has local-endpoint routes, and that the dashboard renders them.
func TestFleetTrackMarksLocalEndpointLanes(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")

	// Set up a local-endpoint route
	ta.m.SetRoutes([]sprint.Route{
		{Name: "local-a", Tier: "flash", Provider: "endpoint", Model: "localhost:8080", Tokens: 0, Deadline: 600, Enabled: true},
	})
	ta.ok("add --stream s1 --count 1 --one")

	// Deal the cards
	ta.deal(1)

	// Take a lane on bench-a machine so it shows up in the lanes list
	ta.ok("lane take go --machine bench-a --as worker-a")

	// Get the where --json --cards output and verify lanes carry route info
	output := ta.ok("where --json --cards")

	var result struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	require.Greater(t, len(result.Lanes), 0, "should have at least one lane")

	// Verify the lane structure includes the route field with correct value
	for _, laneMsg := range result.Lanes {
		var lane struct {
			Kind    string   `json:"kind"`
			Machine string   `json:"machine"`
			Width   int      `json:"width"`
			Held    []string `json:"held"`
			Waiting []string `json:"waiting"`
			Route   string   `json:"route,omitempty"`
		}
		require.NoError(t, json.Unmarshal(laneMsg, &lane))
		require.Equal(t, "go", lane.Kind)
		if lane.Machine == "bench-a" {
			require.Equal(t, "local-a", lane.Route, "bench-a lane should have local-a route")
		}
	}
}
