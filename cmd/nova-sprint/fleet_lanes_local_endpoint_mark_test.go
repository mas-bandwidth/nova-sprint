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
	ta.ok("init --readers reader-a --members m1")

	// Set up a local-endpoint route.
	ta.m.SetRoutes([]sprint.Route{
		{Name: "local-a", Tier: "flash", Provider: "endpoint", Model: "localhost:8080", Tokens: 0, Deadline: 600, Enabled: true},
	})
	ta.ok("add --stream s1 --count 1 --one")

	// Deal one card (it will be dealt to m1 on the local-a route).
	ta.deal(1)

	// Take a lane so it shows up in the lanes list.
	ta.ok("lane take go --machine m1 --as worker-a")

	// Get the where --json --cards output.
	output := ta.ok("where --json --cards")

	// Parse the JSON to check lanes.
	var result struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Should have one lane for m1/go.
	require.Len(t, result.Lanes, 1, "should have one lane")
	var lane struct {
		Kind    string `json:"kind"`
		Machine string `json:"machine"`
		Route   string `json:"route,omitempty"`
	}
	require.NoError(t, json.Unmarshal(result.Lanes[0], &lane))
	require.Equal(t, "go", lane.Kind, "lane kind is go")
	require.Equal(t, "m1", lane.Machine, "machine is m1")
	// The route field should be set to the local-endpoint route name
	require.Equal(t, "local-a", lane.Route, "lane route should be set to the local-endpoint route")
}
