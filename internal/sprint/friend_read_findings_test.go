package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFriendReadReportCarriesEveryFinding pins read-findings-carry-every-bullet:
// a broken read carries every finding of the report to the next attempt, not
// the first (docs/SPEC-SPRINT.md section 8, the read-broken rule).
func TestFriendReadReportCarriesEveryFinding(t *testing.T) {
	t.Parallel()

	report := "Verdict: HOLD\n" +
		"The work is close, but three things are wrong.\n\n" +
		"- internal/x/a.go:10 drops the error from Close\n" +
		"- internal/x/b.go:20 ignores the context\n" +
		"  it returns before the call\n" +
		"- internal/x/c_test.go:30 misses t.Parallel()\n"

	t.Run("every bullet is a finding, in the order written", func(t *testing.T) {
		t.Parallel()
		verdict, finding, why := ParseFriendReadReport(report)
		require.Equal(t, "broken", verdict)
		require.Empty(t, why)
		require.NotContains(t, finding, "The work is close", "a prose line that names no defect is not a finding")
		var missing []string
		for _, bullet := range []string{
			"internal/x/a.go:10 drops the error from Close",
			"internal/x/b.go:20 ignores the context",
			"it returns before the call",
			"internal/x/c_test.go:30 misses t.Parallel()",
		} {
			if !strings.Contains(finding, bullet) {
				missing = append(missing, bullet)
			}
		}
		require.Empty(t, missing, "the finding is missing %v", missing)
		first := strings.Index(finding, "internal/x/a.go:10")
		second := strings.Index(finding, "internal/x/b.go:20")
		third := strings.Index(finding, "internal/x/c_test.go:30")
		require.True(t, first >= 0 && first < second && second < third, "the findings keep the report's order: %q", finding)
	})

	t.Run("the next attempt's finding and fix carry every bullet", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		finished(w, "s1-1", false)
		askReaders(t, w, workerSeatFixtures([]FriendSeat{frontierSeat("amy", 2, Up, t.TempDir())}))
		rc := w.s.Fleet.Card(ReadCardID("s1-1", 1, "amy"))
		require.NotNil(t, rc, "the read of attempt 1 is asked of the friend")
		w.must(FriendReadCloseChecked(w.s, "amy", "s1-1", "", report, nil))
		require.Len(t, openOf(w, NReadBroken, "s1-1"), 1)

		p, _ := TickRuleRework(w.s, on())
		require.NotEmpty(t, p.Units)
		w.must(p)

		pr := w.s.Work.Card("s1-1")
		var missing []string
		for _, bullet := range []string{
			"internal/x/a.go:10 drops the error from Close",
			"internal/x/b.go:20 ignores the context",
			"internal/x/c_test.go:30 misses t.Parallel()",
		} {
			if !strings.Contains(pr.F("finding"), bullet) {
				missing = append(missing, "finding: "+bullet)
			}
			if !strings.Contains(pr.F("fix"), bullet) {
				missing = append(missing, "fix: "+bullet)
			}
		}
		require.Empty(t, missing, "the next attempt is missing %v", missing)
	})

	t.Run("LAND and a HOLD with no defect behave as before", func(t *testing.T) {
		t.Parallel()
		verdict, finding, why := ParseFriendReadReport("Verdict: LAND\n")
		require.Equal(t, "ok", verdict)
		require.Empty(t, finding)
		require.Empty(t, why)

		verdict, finding, why = ParseFriendReadReport("Verdict: HOLD\nThe work is not done.\n")
		require.Empty(t, verdict)
		require.Empty(t, finding)
		require.Contains(t, why, "a finding names a file, a line or a rule")
	})
}
