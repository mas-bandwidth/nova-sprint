package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestFleetTrackMarksLocalEndpointLanes checks that lanes carry route information
// when the machine has cards on local-endpoint routes, and do not carry remote routes.
func TestFleetTrackMarksLocalEndpointLanes(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")

	// Set up a local-endpoint route and a remote route.
	ta.m.SetRoutes([]sprint.Route{
		{Name: "local-a", Tier: "flash", Provider: "endpoint", Model: "localhost:8080", Tokens: 0, Deadline: 600, Enabled: true},
		{Name: "remote-api", Tier: "flash", Provider: "anthropic", Model: "claude-3-5-sonnet", Tokens: 1000, Deadline: 600, Enabled: true},
	})
	ta.ok("add --stream s1 --count 2 --one")

	// Deal two cards: first to m1 on local-a, second to m2 on remote-api.
	ta.deal(2)

	// Take lanes so they show up in the lanes list.
	ta.ok("lane take go --machine m1 --as worker-a")
	ta.ok("lane take go --machine m2 --as worker-b")

	// Get the where --json --cards output.
	output := ta.ok("where --json --cards")

	// Parse the JSON to check lanes.
	var result struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Should have two lanes for m1/go and m2/go.
	require.Len(t, result.Lanes, 2, "should have two lanes")
	lanesMap := make(map[string]struct {
		Kind     string `json:"kind"`
		Machine  string `json:"machine"`
		Route    string `json:"route,omitempty"`
		Endpoint string `json:"endpoint,omitempty"`
	})
	for _, laneRaw := range result.Lanes {
		var lane struct {
			Kind     string `json:"kind"`
			Machine  string `json:"machine"`
			Route    string `json:"route,omitempty"`
			Endpoint string `json:"endpoint,omitempty"`
		}
		require.NoError(t, json.Unmarshal(laneRaw, &lane))
		lanesMap[lane.Machine+"/"+lane.Kind] = lane
	}

	// m1/go should have the local-a route with endpoint
	m1Lane := lanesMap["m1/go"]
	require.Equal(t, "go", m1Lane.Kind, "m1 lane kind is go")
	require.Equal(t, "m1", m1Lane.Machine, "m1 machine is m1")
	require.Equal(t, "local-a", m1Lane.Route, "m1 lane route should be set to local-endpoint route")
	require.Equal(t, "localhost:8080", m1Lane.Endpoint, "m1 lane endpoint should be the model URL")

	// m2/go should NOT have a route (remote route should not be included)
	m2Lane := lanesMap["m2/go"]
	require.Equal(t, "go", m2Lane.Kind, "m2 lane kind is go")
	require.Equal(t, "m2", m2Lane.Machine, "m2 machine is m2")
	require.Equal(t, "", m2Lane.Route, "m2 lane route should be empty (remote route not included)")
	require.Equal(t, "", m2Lane.Endpoint, "m2 lane endpoint should be empty")
}
