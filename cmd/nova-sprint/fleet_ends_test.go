package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetEndProjectionUsesOnlyTypedWorkEnds(t *testing.T) {
	t.Parallel()
	var fleet ntable.Table
	for _, def := range (sprint.Names{}).Definitions() {
		if def.Name == sprint.Fleet { fleet = def }
	}
	require.NotEmpty(t, fleet.Columns)
	row := ntable.Row{Key: "m1", Cells: make([]ntable.Cell, len(fleet.Columns))}
	for i, col := range fleet.Columns {
		if col.Name == sprint.DoneOK { row.Cells[i].Count = 40 }
		if col.Name == sprint.DoneFailed { row.Cells[i].Count = 2 }
	}
	fleet.Rows = []ntable.Row{row}
	fleet = fleetEndProjection(fleet, map[string]sprint.EndCounts{"m1": {
		OK: 1, Failed: 1, Provider: 1, Coordination: 2, Environment: 1, Harness: 1, Clear: 1, Unknown: 1,
	}})
	cells := map[string]string{}
	for i, col := range fleet.Columns { cells[col.Name] = ntable.CellText(fleet.Columns, fleet.Rows[0], i) }
	assert.Equal(t, "50.0%", cells[sprint.OkPct])
	assert.Equal(t, "1", cells["end_provider"])
	assert.Equal(t, "2", cells["end_coordination"])
	assert.Equal(t, "1", cells["end_unknown"])
	assert.Equal(t, "40", cells[sprint.DoneOK], "verdict placement is kept independently")
}
