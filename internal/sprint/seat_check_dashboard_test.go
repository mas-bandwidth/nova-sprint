package sprint

import (
	"strings"
	"testing"
	"time"
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
		if r.Down != 0 {
			t.Fatalf("dashboard with 1s-old data must be up, got DOWN count=%d", r.Down)
		}
		text := r.Text()
		for _, l := range r.Lines {
			if l.Thing == SeatCheckDashboard {
				if !l.Up {
					t.Fatalf("dashboard line must be UP, got DOWN: %s", l.String())
				}
				if !strings.Contains(text, "status=200") {
					t.Fatalf("dashboard UP line must include status=200, got: %s", l.String())
				}
				return
			}
		}
		t.Fatal("dashboard line not found")
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
		if r.Down == 0 {
			t.Fatalf("dashboard with data past bound (%s) must be DOWN, got OK", bound)
		}
		text := r.Text()
		for _, l := range r.Lines {
			if l.Thing == SeatCheckDashboard && !l.Up {
				if !strings.Contains(text, "status=200") {
					t.Fatalf("dashboard DOWN line must include status=200, got: %s", l.String())
				}
				return
			}
		}
		t.Fatal("dashboard DOWN line not found")
	})

	t.Run("no answer is down", func(t *testing.T) {
		t.Parallel()
		m := upMeasures()
		m.Dashboard = DashM{
			Addr: "127.0.0.1:7390",
			Err:  "connection refused",
		}
		r := JudgeSeatCheck(m, now)
		if r.Down == 0 {
			t.Fatal("dashboard with no answer must be DOWN, got OK")
		}
	})
}
