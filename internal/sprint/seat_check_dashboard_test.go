package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSeatCheckDashboardFreshIsUp tests that a dashboard serving data under the bound is OK.
func TestSeatCheckDashboardFreshIsUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	t.Run("data one second old is up", func(t *testing.T) {
		t.Parallel()
		m := upMeasures()
		m.Dashboard = DashM{
			Addr:   "127.0.0.1:7390",
			Status: 200,
			Build:  "3f2a",
			At:     now.Add(-time.Second),
		}
		r := JudgeSeatCheck(m, now)
		require.Zero(t, r.Down, "dashboard with 1s-old data must be up")
		text := r.Text()
		for _, l := range r.Lines {
			if l.Thing == SeatCheckDashboard {
				require.True(t, l.Up, "dashboard line must be UP: %s", l.String())
				require.Contains(t, text, "status=200")
				return
			}
		}
		require.Fail(t, "dashboard line not found")
	})

	t.Run("data past bound is down", func(t *testing.T) {
		t.Parallel()
		m := upMeasures()
		bound := 2 * time.Second
		m.Dashboard = DashM{
			Addr:   "127.0.0.1:7390",
			Status: 200,
			Build:  "3f2a",
			At:     now.Add(-bound),
		}
		r := JudgeSeatCheck(m, now)
		require.NotZero(t, r.Down, "dashboard with data past bound (%s) must be DOWN", bound)
		text := r.Text()
		for _, l := range r.Lines {
			if l.Thing == SeatCheckDashboard && !l.Up {
				require.Contains(t, text, "status=200")
				return
			}
		}
		require.Fail(t, "dashboard DOWN line not found")
	})

	t.Run("no answer is down", func(t *testing.T) {
		t.Parallel()
		m := upMeasures()
		m.Dashboard = DashM{
			Addr: "127.0.0.1:7390",
			Err:  "connection refused",
		}
		r := JudgeSeatCheck(m, now)
		require.NotZero(t, r.Down, "dashboard with no answer must be DOWN")
	})
}
