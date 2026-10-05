package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFleetTrackMarksLocalEndpointLanes checks that lanes carry route marks when the machine has cards on routes.
// The fixture is a where --json fixture with local routes and lanes, from PR 5307 after the lanes collapse.
func TestFleetTrackMarksLocalEndpointLanes(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a")

	// Set up routes and add cards.
	ta.ok("config set --store-key routes --json '[\"flash\"]'")
	ta.ok("config set --store-key route:flash --json '{\"name\":\"flash\",\"tier\":\"flash\",\"provider\":\"opencode\",\"model\":\"opencode\",\"tokens\":0,\"deadline\":600,\"enabled\":true}'")
	ta.ok("add --stream s1 --count 1 --route flash")

	// Take a lane so it shows up in the lanes list.
	ta.ok("lane take --machine bench-a --who worker-a --kind go")

	// Get the where --json output.
	output := ta.ok("where --json --cards")

	// Parse the JSON to check lanes.
	var result struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Every lane should have a route field.
	require.Len(t, result.Lanes, 1, "should have one lane")
	var lane struct {
		Kind    string `json:"kind"`
		Machine string `json:"machine"`
		Route   string `json:"route"`
	}
	require.NoError(t, json.Unmarshal(result.Lanes[0], &lane))
	require.Equal(t, "go", lane.Kind, "lane kind is go")
	require.Equal(t, "bench-a", lane.Machine, "machine is bench-a")
	require.NotEmpty(t, lane.Route, "lane route should not be empty when machine has cards")
}
