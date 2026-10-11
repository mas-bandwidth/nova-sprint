package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The bound's judgment names the exact command that answers it, not only that a rework is
// refused: a second bound on a tier says which tier above to rework on and the drop command
// whole, so the coordinator runs the remedy as printed instead of guessing (the dogfood of
// 2026-10-10 evening: a fleet-only restart bounded several cards, and answering them took
// four tries -- brief --tier, redo, rework, rework --group -- because the text named none).
func TestTheBoundJudgmentNamesItsRemedy(t *testing.T) {
	t.Parallel()
	h := flashAndPro(t)
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(SetStep(sprint.SetReq{Attempts: "8", Who: h.st.Actor}))
	h.startMachine()
	h.machine()
	h.boundBy("s1-1", noResultLine)
	require.Empty(t, h.reworkOf("run on the pro tier", ""), "the first bound's rework is the next attempt")
	h.boundBy("s1-1", noResultLine)
	open := h.openOf(sprint.NBound)
	require.Len(t, open, 1)
	what := open[0].Note.What
	require.Contains(t, what, "a second bound on tier flash: not reworked on it again")
	require.Contains(t, what, "rework it with a fix and --tier pro or heavy", "the remedy names the exact tier above")
	require.Contains(t, what, "nova-sprint drop s1-1 --reason <why>", "the remedy names the drop command whole")
	h.clean("the bound names its remedy")
}
