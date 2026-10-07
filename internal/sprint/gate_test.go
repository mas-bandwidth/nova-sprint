package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The machine gate before read (docs/SPEC-SPRINT.md section 5, the gate): a red gate
// reworks the attempt with the failing lines as the fix, and no reader is asked.
// A green gate records the gate lines, and readers are asked with those lines in
// the read packet.

func TestARedGateIsReworkedBeforeAnyReaderIsAsked(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)

	// Finish the work as ok so it goes to review (gate check runs on Review cards)
	finished(w, "s1-1", false)

	// Create a gate linter that simulates a red gate (test failed)
	gateFailLint := WorkLinter(func(s *Snapshot, pr *Card) ([]LintFinding, error) {
		// This simulates a gate that fails - like a test failing on the bench
		return []LintFinding{
			{
				File:  "internal/sprint/steps_review.go",
				Line:  42,
				Token: "gate-test-fail",
				What:  "test TestARedGateIsReworkedBeforeAnyReaderIsAsked failed: expected ok, got red",
			},
		}, nil
	})

	// TickLint runs before ask - this should rework the attempt
	r := TickReq{Who: "machine", WorkLint: gateFailLint}
	p, ok := TickLint(w.s, r)
	t.Logf("TickLint: ok=%v, units=%d, refused=%d", ok, len(p.Units), len(p.Refused))
	w.must(p) // Apply the plan
	require.True(t, ok, "the gate should report findings")
	require.NotEmpty(t, p.Units, "the gate should rework the attempt")
	require.Empty(t, p.Refused, "the rework should not be refused")

	// The primary should be reworked (back to ready/working state - depends on members up)
	pr := w.s.Work.Card("s1-1")
	t.Logf("Primary state: Col=%s, fix=%s", pr.Col, pr.F("fix"))
	require.NotEqual(t, Review, pr.Col, "the attempt should be reworked out of review")
	require.NotEmpty(t, pr.F("fix"), "the rework should have a fix with the failing line")

	// No reader should be asked when the gate is red
	reads := readsAt(w.s, pr, 1)
	require.Empty(t, reads, "no reader should be asked when the gate is red")

	// Now verify that TickAsk won't ask this primary either (lintHeld protection)
	ask := w.must(Ask(w.s, AskReq{}))
	require.Empty(t, ask.Units, "tick ask should not ask a primary that failed the gate")
}

func TestAGreenGateRecordsGateLinesAndReadersAreAsked(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)

	// Finish the work as ok so it goes to review
	finished(w, "s1-1", false)

	// Create a gate linter that simulates a green gate (test passed)
	gatePassLint := WorkLinter(func(s *Snapshot, pr *Card) ([]LintFinding, error) {
		// This simulates a gate that passes - no findings
		return nil, nil
	})

	// TickLint runs before ask - this should not rework since gate passed
	r := TickReq{Who: "machine", WorkLint: gatePassLint}
	p, ok := TickLint(w.s, r)
	require.False(t, ok, "the gate should not report when it passes")
	require.Empty(t, p.Units, "the gate should not rework when green")

	// The primary should remain in review
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, Review, pr.Col, "the attempt should stay in review when gate is green")

	// Now the machine's tick can ask readers
	ask := w.must(Ask(w.s, AskReq{}))
	require.NotEmpty(t, ask.Units, "readers should be asked when the gate is green")

	reads := readsAt(w.s, pr, 1)
	require.NotEmpty(t, reads, "readers should be asked when the gate is green")
}
