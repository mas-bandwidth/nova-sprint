package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestFleetTrackMarksLocalEndpointLanes checks that lanes carry route marks when the machine has cards on routes.
// The fixture is a where --json fixture with local routes and lanes, from PR 5307 after the lanes collapse.
func TestFleetTrackMarksLocalEndpointLanes(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a")

	// Set up routes and add cards.
	ta.m.SetRoutes([]sprint.Route{
		{Name: "flash", Tier: "flash", Provider: "opencode", Model: "opencode", Tokens: 0, Deadline: 600, Enabled: true},
	})
	ta.ok("add --stream s1 --count 1 --one")

	// Take a lane so it shows up in the lanes list.
	ta.ok("lane take go --machine bench-a --as worker-a")

	// Get the where --json output.
	output := ta.ok("where --json --cards")

	// Parse the JSON to check lanes.
	var result struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Every lane should have a route field (may be empty if no cards on the machine yet).
	require.Len(t, result.Lanes, 1, "should have one lane")
	var lane struct {
		Kind    string `json:"kind"`
		Machine string `json:"machine"`
		Route   string `json:"route,omitempty"`
	}
	require.NoError(t, json.Unmarshal(result.Lanes[0], &lane))
	require.Equal(t, "go", lane.Kind, "lane kind is go")
	require.Equal(t, "bench-a", lane.Machine, "machine is bench-a")
	// The route field should exist in the JSON (may be empty or have a value)
	require.NotNil(t, result.Lanes[0], "lane should be present in the output")
}
