package main

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
)

// The dashboard's working cell is the live runs (v1.2.6; tla/LiveRuns.tla): of a fleet row's
// working cards, those its fresh live set names running; held and stale beside it.
// 2026-10-10 ~18:25Z it showed 30 working on six fleet hosts with no harness run.
func TestTheWorkingCellIsTheRowsLiveRuns(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 18, 25, 0, 0, time.UTC)
	rowWorking := map[string][]string{"space": {"a@1", "b@1", "c@2"}, sprint.FriendRow("zhi"): {"z@1", "y@1"}}
	beats := map[string]sprint.Beat{
		"space": {At: now, LiveKnown: true, Live: []string{"a@1", "b@1:held"}},
		"zhi":   {At: now, LiveKnown: true, Live: []string{}},
	}
	cells := map[string]any{"working": "3"}
	liveCells(cells, rowWorking, beats, "space", now)
	assert.Equal(t, map[string]any{"working": "1", "held": "1", "stale": "1"}, cells)
	cells = map[string]any{"working": "2"}
	liveCells(cells, rowWorking, beats, sprint.FriendRow("zhi"), now)
	assert.Equal(t, map[string]any{"working": "0", "held": "0", "stale": "2"}, cells, "her daemon runs no lane: none of her takes is working")
	cells = map[string]any{"working": "3"}
	liveCells(cells, nil, beats, "space", now)
	assert.Equal(t, map[string]any{"working": "3"}, cells, "without the where record the table's count stands")
}

func TestTheServedFleetBeatTakesALiveSet(t *testing.T) {
	t.Parallel()
	for _, ok := range [][]string{
		{"fleet", "beat", "m1", "--load", "1.5"},
		{"fleet", "beat", "m1", "--stop-returns", "0", "--load", "1.5"},
		{"fleet", "beat", "m1", "--stop-returns", "2", "--live", "a@1,b@2:held", "--load", "1.5"},
		{"fleet", "beat", "m1", "--live", "-", "--load", "0"},
	} {
		as, _, why := workerVerb(ok)
		assert.Empty(t, why, "%v", ok)
		assert.Equal(t, "m1", as)
	}
	for _, bad := range [][]string{
		{"fleet", "beat", "m1", "--live", "a@1", "--stop-returns", "0", "--load", "1"},
		{"fleet", "beat", "m1", "--live", "a", "--load", "1"},
		{"fleet", "beat", "m1", "--live", "", "--load", "1"},
		{"fleet", "beat", "m1", "--load", "1", "--live", "a@1"},
	} {
		_, _, why := workerVerb(bad)
		assert.NotEmpty(t, why, "%v", bad)
	}
	_, _, why := workerVerb([]string{"friend", "beat", "zhi", "--live", "z@1:held"})
	assert.Empty(t, why)
	_, _, why = workerVerb([]string{"friend", "beat", "zhi", "--live", "z@0"})
	assert.NotEmpty(t, why)
}
