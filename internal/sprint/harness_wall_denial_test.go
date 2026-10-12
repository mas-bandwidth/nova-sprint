package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A wall denial is its own harness fault class (docs/SPEC-SPRINT.md section 8, the rules
// table's row failed; tla/SprintRules.tla, Part "answers": it is a harness fault like the
// others, so HarnessFaultBounded holds unchanged). A report that carries a wall's
// denied_path= (any wall but "-" and "none-by-flag"), or that begins wall: denied_path=, is
// this class, matched before deadline; a report with no wall is not. The rework names the
// denied path and the remedy, so the next attempt is not sent the same trap.

// wallDenialIncidentReport is the report of the incident this class comes from: the
// coordinator's record of the card the-nova-config-timeout-test-stalls-after-the-connection
// attempt 1 on 2026-10-12, read at the SOURCE line.
const wallDenialIncidentReport = "attempt 1 failed: deadline: no RESULT.md shape; native refused: the-nova-config-timeout-test-stalls-after-the-connection.w1 a denial the card's shell reported went unread, so this run's disposition is refused rather than OK: step=- rc=-1 wall=landlock denied_path=/tmp/test_stall.go operation=unverified ..."

func TestAWallDenialIsItsOwnHarnessFaultClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		report string
		class  string
	}{
		{"the incident's report is a wall denial, not a deadline", wallDenialIncidentReport, ClassWallDenial},
		{"the shell's wall: denied_path= line is a wall denial", "wall: denied_path=/tmp/x: no RESULT.md shape", ClassWallDenial},
		{"a report with no wall is not a wall denial", strings.Replace(wallDenialIncidentReport, "wall=landlock", "wall=none-by-flag", 1), "deadline"},
		{"a report with no wall= at all is a deadline", "deadline: no RESULT.md shape", "deadline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.class, HarnessFault(tc.report))
		})
	}
}

func TestAWallDenialReworkNamesThePathAndTheRemedy(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, harnessCardBrief)
	dealWith(w)
	takeAndFinish(t, w, WorkCardID("s1-1", 1), wallDenialIncidentReport)
	require.Len(t, openOf(w, NWorkFailed, "s1-1"), 1, "work came back failed: a judgment")

	rules(w, on())
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, 1, pr.Int("reworks"), "a wall denial is reworked by rule")
	assert.Equal(t, ClassWallDenial, pr.F(FieldHarnessClass), "the rework carries the wall-denial class")
	assert.Contains(t, pr.F("fix"), "/tmp/test_stall.go", "the rework names the denied path")
	assert.Contains(t, pr.F("fix"), WallDenialScratchRemedy, "the rework carries the scratch remedy")
}
