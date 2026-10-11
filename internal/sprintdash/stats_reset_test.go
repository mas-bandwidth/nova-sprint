package sprintdash

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stats reset on the page (nova-sprint stats reset): the cost tile counts from the mark,
// and so must the cards its cost per card is over, in the release the page shows by default
// as in the whole sprint; the pie reads the spend by tier where where --json carries it.

// resetCopy is where --json after a reset: two streams of release v1 and v2, the mark's
// cards landed since by stream (2 and 3, 5 in all), and the spend since the mark.
const resetCopy = `{"landed":100,"all":120,
"streams":[{"Stream":"a","Release":"v1"},{"Stream":"b","Release":"v2"}],
"releases":{"v1":10,"v2":10},
"tables":{"work":{"a":{"landed":"60","waiting":"10","cost":"$2.00","per_landed":"$1.00"},"b":{"landed":"40","waiting":"10","cost":"$9.00","per_landed":"$3.00"}}},
"stream_costs":{"a":{"total_cost":"$2.00","cost_by_tier":{"flash":"$2.00"}},"b":{"total_cost":"$9.00","cost_by_tier":{"pro":"$9.00"}}},
"stats_reset":{"at":"2026-10-09T21:30:00Z","reason":"count from now","landed":5,"streams":{"a":2,"b":3}}}`

// The release the page shows by default (the earliest with cards left, v1 here) narrows
// stats_reset to its streams: its landed is the release's cards since the mark.
func TestAReleaseViewCountsTheResetsLandedOverItsStreams(t *testing.T) {
	t.Parallel()
	v := viewOf(json.RawMessage(resetCopy), "")
	require.Equal(t, "v1", v.Release, "the current release is shown by default")
	var d struct {
		Landed     int64 `json:"landed"`
		StatsReset struct {
			Landed  int64            `json:"landed"`
			Streams map[string]int64 `json:"streams"`
			Reason  string           `json:"reason"`
		} `json:"stats_reset"`
	}
	require.NoError(t, json.Unmarshal(v.Data, &d))
	assert.Equal(t, int64(60), d.Landed)
	assert.Equal(t, int64(2), d.StatsReset.Landed, "the release's cards since the mark, not the sprint's 5")
	assert.Equal(t, map[string]int64{"a": 2}, d.StatsReset.Streams)
	assert.Equal(t, "count from now", d.StatsReset.Reason, "the mark's other fields pass through")

	all := viewOf(json.RawMessage(resetCopy), AllReleases)
	require.NoError(t, json.Unmarshal(all.Data, &d))
	assert.Equal(t, int64(5), d.StatsReset.Landed, "all: the sprint's")

	// a copy with no reset is left as it was
	none := viewOf(json.RawMessage(`{"streams":[{"Stream":"a","Release":"v1"}],"tables":{"work":{"a":{"landed":"1"}}}}`), "v1")
	assert.NotContains(t, string(none.Data), "stats_reset")
}
