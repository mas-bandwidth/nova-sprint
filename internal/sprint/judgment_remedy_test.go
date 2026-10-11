package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The brief's bound judgment (the tick's, not the finish's) names the exact command that
// answers it: brief --brief-file corrects the brief in place, and drop names the card, so the
// coordinator runs the remedy as printed instead of brief --tier (which re-tiers and changes
// nothing) or rework (which a brief defect refuses). The text is BriefBound.Why's, the same
// line a rework's refusal says.
func TestTheBriefBoundJudgmentNamesItsRemedy(t *testing.T) {
	t.Parallel()
	w := capWorld(t)
	takeEndedAtTheBound(w, "s1-1.w4")
	capDeal(w)
	open := boundJudgments(w.s, "s1-1")
	require.Len(t, open, 1, "the cap's judgment for the coordinator")
	require.Contains(t, open[0].Note.What, "nova-sprint brief s1-1 --brief-file <path>", "the remedy names the brief command")
	require.Contains(t, open[0].Note.What, "nova-sprint drop s1-1 --reason '<why>'", "the remedy names the drop command")
	w.clean("the brief bound names its remedy")
}
