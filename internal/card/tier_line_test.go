package card

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The tier on line 1 is one rule in both forms (docs/CLI.md, add, "The tier on line 1"): a
// card brief (one with a PATHS: line) names its tier on line 1, as `<title> tier: <t>` or on
// the RESULT line `RESULT: <id> sha=<sha12> tier: <t>`; the lint reads it (no tier-line
// finding) and the store reads the same tier from the same line (cardhdr.ReadModel, which
// sprint.LineOneTier and the deal read); a card brief whose line 1 names none is refused
// (the tier-line finding). A brief with no PATHS: line is a free task: neither read nor
// refused here. The seat, 2026-10-12: six cards whose RESULT line said tier: heavy were dealt
// on flash.
func TestTheTierLineIsReadOrRefusedByOneRuleInBothForms(t *testing.T) {
	t.Parallel()
	const rest = "\nREPO: o/r\nPATHS: internal/x/*.go\n\nTHE TASK.\n"
	for _, tc := range []struct {
		line, tier string
		refused    bool
	}{
		{"RESULT: a-card sha=000000000000 tier: heavy", "heavy", false},
		{"RESULT: a-card sha=0123456789ab tier: pro", "pro", false},
		{"RESULT: a-card sha=000000000000 tier: flash", "flash", false},
		{"RESULT: a-card sha=000000000000 tier: frontier", "frontier", false},
		{"c: the work (s1) tier: heavy", "heavy", false},
		{"RESULT: a-card sha=000000000000", "", true},
		{"c: the work (s1)", "", true},
	} {
		brief := tc.line + rest
		var checks []string
		for _, f := range Checks("a-card", brief, Options{}) {
			checks = append(checks, f.Check)
		}
		if tc.refused {
			assert.Equal(t, []string{"tier-line"}, checks, "%q: a card brief whose line 1 names no tier is refused", tc.line)
		} else {
			assert.Empty(t, checks, "%q: read, not refused", tc.line)
		}
		m, _ := cardhdr.ReadModel(brief)
		assert.Equal(t, tc.tier, m.Tier, "%q: the tier the store reads is the tier the lint reads", tc.line)
	}
	assert.Empty(t, Checks("a-card", "RESULT: a-card sha=000000000000\n\nTHE TASK.\n", Options{}), "no PATHS: a free task, not held to a tier")
}
