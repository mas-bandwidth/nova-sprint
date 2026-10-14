package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestRateCoverLandingRate pins LandingRate: the landings per hour of running
// time (rate.go). The window's rate is returned when enough landings have
// landed in the last RateWindow of running time; otherwise the total is spread
// over all running time since the first start, or zero.
func TestRateCoverLandingRate(t *testing.T) {
	t.Parallel()
	now := coverT0
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	mins := func(ms ...int) []time.Time {
		var out []time.Time
		for _, m := range ms {
			out = append(out, ago(time.Duration(m)*time.Minute))
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		landed []time.Time
		total  int64
		spans  []Span
		first  time.Time
		want   float64
	}{
		{"refusal: not started, first zero", []time.Time{now}, 1, nil, time.Time{}, 0},
		{"refusal: now equal to first, no time advanced", []time.Time{now}, 1, nil, now, 0},
		{"main: window rate with enough landings in the last hour", mins(50, 40, 30, 20, 10), 10, nil, ago(2 * time.Hour), 5},
		{"main: window rate with a stop shrinking running time", mins(70, 60, 50, 40, 20), 10,
			[]Span{{From: ago(30 * time.Minute), To: ago(10 * time.Minute)}}, ago(2 * time.Hour), 5},
		{"main: fallback to total over all running time", mins(50, 40, 30), 10, nil, ago(2 * time.Hour), 5},
		{"refusal: too few landings and no total over running time", mins(50, 40), 0, nil, ago(2 * time.Hour), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := LandingRate(tc.landed, tc.total, tc.spans, tc.first, now)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRateCoverRunningStart pins runningStart: the clock reading from which
// the running time to now spans w, walking STOPPED spans back from now
// (rate.go). The window starts at first when running time since first is less
// than w; otherwise it starts at now minus w, pushed back past any stops.
func TestRateCoverRunningStart(t *testing.T) {
	t.Parallel()
	now := coverT0
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	for _, tc := range []struct {
		name  string
		spans []Span
		first time.Time
		want  time.Time
	}{
		{"main: a stop inside the window pushes the start past the stop",
			[]Span{{From: ago(30 * time.Minute), To: ago(10 * time.Minute)}}, ago(3 * time.Hour), ago(80 * time.Minute)},
		{"refusal: run less than w since first starts the window at first",
			[]Span{{From: ago(time.Hour), To: ago(30 * time.Minute)}}, ago(30 * time.Minute), ago(30 * time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runningStart(tc.spans, tc.first, now, time.Hour)
			assert.Equal(t, tc.want, got)
		})
	}
}
