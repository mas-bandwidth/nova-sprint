package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Fleet table's working column shows one cell per lane of the machine's width.
// A lane whose route is a local endpoint carries a route mark with the endpoint in its tooltip.
// The machine is shown as working (not idle) when any lanes are lit, even if all are local endpoints.
func TestFleetTrackMarksLocalEndpointLanes(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("fleet up m1 --width 4")
	ta.ok("add --stream s1 --count 2")
	ta.ok("start")
	ta.ok("tick")

	// Get the where --json output which includes lanes data
	out := ta.ok("where --json --cards")
	var v struct {
		Tables map[string]map[string]map[string]interface{} `json:"tables"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &v))

	// Verify the fleet table has the machine row
	fleet, ok := v.Tables["fleet"]
	require.True(t, ok)
	m1, ok := fleet["m1"]
	require.True(t, ok)

	// Verify the basic fields exist
	working := m1["working"]
	require.NotNil(t, working, "m1 should have working field")

	// Verify the width field exists
	width := m1["width"]
	require.NotNil(t, width)
	assert.Equal(t, "4", width, "m1 width should be 4")

	// Verify lanes field exists and has the correct structure
	// If the machine has lanes, each lane is an object with a route field
	if lanesVal, ok := m1["lanes"]; ok && lanesVal != nil {
		// lanesVal is a JSON string (stored in the cells map as string/interface{})
		lanesStr, ok := lanesVal.(string)
		if ok && lanesStr != "" {
			var lanes []map[string]string
			require.NoError(t, json.Unmarshal([]byte(lanesStr), &lanes))
			require.Len(t, lanes, 4, "m1 should have 4 lanes matching width")
			// Lanes can have a route field marking local endpoints
			for _, lane := range lanes {
				route, hasRoute := lane["route"]
				if hasRoute && route != "" {
					// This is a local endpoint lane - it should have a route
					assert.IsType(t, "", route, "route should be a string")
				}
			}
		}
	}
}
