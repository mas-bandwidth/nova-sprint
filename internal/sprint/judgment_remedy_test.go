package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The attempt cap's judgment (the tick's, not the finish's) names the exact commands that
// answer it, each a coordinator decision: brief --brief-file corrects the brief in place,
// rework --tier re-tiers it (the way to a friend tier since the cap's friend deal was retired,
// 2026-10-12), rework --carry carries its last head, and drop names the card, so the
// coordinator runs the remedy as printed. The text is BriefBound.Why's, the same line a
// rule's refusal says.
func TestTheBriefBoundJudgmentNamesItsRemedy(t *testing.T) {
	t.Parallel()
	w := capWorld(t)
	w.part(TickDeal, TickReq{})
	open := capJudgmentsOn(w.s, "s1-1")
	require.Len(t, open, 1, "the cap's judgment for the coordinator")
	require.Contains(t, open[0].Note.What, "nova-sprint brief s1-1 --brief-file <path>", "the remedy names the brief command")
	require.Contains(t, open[0].Note.What, "nova-sprint rework s1-1 --fix <text> --tier <a higher tier>", "the remedy names the re-tier")
	require.Contains(t, open[0].Note.What, "nova-sprint rework s1-1 --carry --fix <text>", "the remedy names the carry")
	require.Contains(t, open[0].Note.What, "nova-sprint drop s1-1 --reason '<why>'", "the remedy names the drop command")
	w.clean("the brief bound names its remedy")
}
