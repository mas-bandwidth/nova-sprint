package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 2026-10-17, nova-sprint v1.5.4: check failed with about 27 rule 12 findings "past its
// due time, and no overdue mark holds it" on judgments the tick quiets (a need is
// blocked, and a card reached its bound on a blocker with dependents): tickOverdue skips
// them by JudgmentOverdue (quietedByBlocker), while overdueUnmarked judged them by the
// clock alone. The checker asks what the tick asks.
func TestAJudgmentTheTickQuietsIsNoOverdueStall(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.s.Now = time.Unix(1000, 0).UTC() // the test clock starts at 1000 s, never 0
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"blocker"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dependent"}, Needs: []string{"blocker"}}))
	w.note(judgment(NBound, "s1", w.s.Now, 0, "blocker"))
	w.s.Now = w.s.Now.Add(3 * time.Hour)
	p, _ := TickNeedBlocked(w.s, TickReq{})
	w.note(p.Notes...)
	w.s.Now = w.s.Now.Add(3 * time.Hour) // both judgments past DeadlineJudgment
	po, _ := TickOverdue(w.s, TickReq{})
	w.must(po)
	for _, n := range po.Notes {
		require.NotEqual(t, NOverdue, n.Type, "the tick quiets both judgments: %+v", n)
	}
	for _, f := range Unheld(HeldState{Snap: w.s, Running: true}, w.s.Now) {
		require.False(t, strings.Contains(f.Why, "no overdue mark holds it"), "a quieted judgment is no stall: %s", f)
	}
}
