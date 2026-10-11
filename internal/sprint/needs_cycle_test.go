package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A needs rewrite never closes a cycle of needs (docs/SPEC-SPRINT.md section 9,
// rule 11: add refuses needs that would make a cycle, and so does every path
// that rewrites them; tla/SprintTables.tla NoNeedCycle, witness "cycle"). Two
// ready cards: the first edit makes a wait for b, and the second, b waiting for
// a, is refused naming the loop; the first edit's graph is kept.
func TestNeedsSetRefusesAnEditThatClosesACycle(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1 and s1-2, both ready
	require.Equal(t, Ready, w.state("s1-1"))
	require.Equal(t, Ready, w.state("s1-2"))

	w.must(NeedsSet(w.s, NeedsSetReq{ID: "s1-1", Needs: []string{"s1-2"}, Who: "coordinator"}))
	require.Equal(t, Waiting, w.state("s1-1"), "a waits for b, not landed")

	p := NeedsSet(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"s1-1"}, Who: "coordinator"})
	require.Empty(t, p.Units, "the edit that closes the cycle writes nothing: %+v", p)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "the needs would make a cycle")
	require.Contains(t, p.Refused[0].Why, "s1-1")
	require.Contains(t, p.Refused[0].Why, "s1-2")

	require.Equal(t, "s1-2", w.s.Work.Card("s1-1").F("needs"), "the first edit's graph is kept")
	require.Equal(t, "", w.s.Work.Card("s1-2").F("needs"))
	require.Equal(t, Ready, w.state("s1-2"))
	require.Empty(t, Cycles(w.s), "no cycle on the table")
}
