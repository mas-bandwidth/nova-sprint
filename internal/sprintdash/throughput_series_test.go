package sprintdash

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// whereSeries is a where --json body that carries a landedSeries with a lastHour.
func whereSeries(friends, fleet int) []byte {
	return []byte(fmt.Sprintf(`{"at":"2026-10-02T19:00:00Z","landed":0,"all":100,"summary":"x","tables":{"work":{}},"landedSeries":{"lastHour":{"friends":%d,"fleet":%d}}}`, friends, fleet))
}

// The throughput is a function of the copy: where --json carries landedSeries whose
// lastHour holds the cards landed in the 3600 s before the copy's time, split friends and
// fleet (docs/SPEC-SPRINT-DASHBOARD.md). A copy that carries one shows that sum at once,
// no warm-up; a copy with none keeps the samples.
func TestThroughputReadsTheLandingLog(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.next = func() ([]byte, error) { return whereSeries(13, 6), nil }
	v := r.api()
	assert.InDelta(t, 19, v["throughput"], 0, "the landing log's last hour: friends plus fleet, on the first read")
	assert.InDelta(t, 60, v["throughputMinutes"], 0, "no warm-up: the minutes are the log's hour")

	// PageSnapshot (the page) and Snapshot (the snapshot) agree.
	want := []struct {
		name string
		body []byte
	}{
		{"snapshot", r.s.Snapshot()},
		{"page", r.s.PageSnapshot()},
	}
	for _, w := range want {
		var snap struct {
			Throughput        *float64 `json:"throughput"`
			ThroughputMinutes float64  `json:"throughputMinutes"`
		}
		require.NoError(t, json.Unmarshal(w.body, &snap), w.name)
		require.NotNil(t, snap.Throughput, w.name)
		assert.InDelta(t, 19, *snap.Throughput, 0, w.name)
		assert.InDelta(t, 60, snap.ThroughputMinutes, 0, w.name)
	}

	// A last hour with no landings is 0, never null.
	r0 := newRig(t)
	r0.next = func() ([]byte, error) { return whereSeries(0, 0), nil }
	v0 := r0.api()
	require.NotNil(t, v0["throughput"], "zero landings in the last hour: 0, not null")
	assert.InDelta(t, 0, v0["throughput"], 0)
	assert.InDelta(t, 60, v0["throughputMinutes"], 0)

	// A copy with no landedSeries (an older server) keeps today's sampling: null until
	// ten minutes of samples, as TestDashboardThroughput pins.
	r1 := newRig(t)
	landed := 100
	r1.next = func() ([]byte, error) { return where(landed), nil }
	v1 := r1.api()
	assert.Nil(t, v1["throughput"], "no landedSeries: one sample, no rate")
	assert.InDelta(t, 0, v1["throughputMinutes"], 0)
	r1.advance(9 * time.Minute)
	landed = 109
	v1 = r1.api()
	assert.Nil(t, v1["throughput"], "nine minutes of samples: not yet")
	r1.advance(time.Minute)
	landed = 110
	v1 = r1.api()
	assert.InDelta(t, 60, v1["throughput"], 0, "10 cards in 10 minutes: 60 an hour")
}
