package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestFleetTrackMarksLocalEndpointLanes checks that lanes carry route information
// when the machine has cards on local-endpoint routes.
func TestFleetTrackMarksLocalEndpointLanes(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a")

	// Set up a local-endpoint route.
	ta.m.SetRoutes([]sprint.Route{
		{Name: "local-a", Tier: "flash", Provider: "endpoint", Model: "localhost:8080", Tokens: 0, Deadline: 600, Enabled: true},
	})
	ta.ok("add --stream s1 --count 1 --one")

	// Deal a card to bench-a on the local-endpoint route.
	ta.ok("deal --to bench-a --on local-a")

	// Take a lane so it shows up in the lanes list.
	ta.ok("lane take go --machine bench-a --as worker-a")

	// Get the where --json --cards output.
	output := ta.ok("where --json --cards")

	// Parse the JSON to check lanes.
	var result struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Should have one lane for bench-a/go.
	require.Len(t, result.Lanes, 1, "should have one lane")
	var lane struct {
		Kind    string `json:"kind"`
		Machine string `json:"machine"`
		Route   string `json:"route,omitempty"`
	}
	require.NoError(t, json.Unmarshal(result.Lanes[0], &lane))
	require.Equal(t, "go", lane.Kind, "lane kind is go")
	require.Equal(t, "bench-a", lane.Machine, "machine is bench-a")
	// The route field should be set to the local-endpoint route name
	require.Equal(t, "local-a", lane.Route, "lane route should be set to the local-endpoint route")
}
