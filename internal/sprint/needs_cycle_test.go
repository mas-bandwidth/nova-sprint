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

// A dealt card that gains a need keeps its edge while it works (the tick moves it
// back to waiting later): a rewrite that closes a loop through it is a cycle. The
// graph takes every open card's stored needs, whatever its state (needsCycleWith).
func TestNeedsSetSeesAWorkingCardsNeeds(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1 and s1-2, both ready
	w.must(Deal(w.s, DealReq{Sel: Sel{Only: []string{"s1-1"}}}))
	require.Equal(t, Working, w.state("s1-1"))

	w.must(NeedsSet(w.s, NeedsSetReq{ID: "s1-1", Needs: []string{"s1-2"}, Who: "coordinator"}))
	require.Equal(t, "s1-2", w.s.Work.Card("s1-1").F("needs"), "the working card holds its new need")
	require.Equal(t, Working, w.state("s1-1"))

	p := NeedsSet(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"s1-1"}, Who: "coordinator"})
	require.Empty(t, p.Units, "the rewrite closes a loop through the working card: %+v", p)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "the needs would make a cycle")
	require.Equal(t, "", w.s.Work.Card("s1-2").F("needs"), "the first edit's graph is kept")
}

// A brief batch is judged as a whole: two rewrites that each pass alone (s1-1
// needs s1-2, s1-2 needs s1-1) close a cycle together, and both are refused
// against the state with the whole batch applied (needsCycleWith).
func TestABriefBatchCannotCloseACycleBetweenItsCards(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1 and s1-2, both ready
	p := Brief(w.s, BriefReq{Cards: []BriefCard{
		{ID: "s1-1", Brief: "one, reworded\nDEPENDS-ON: s1-2", Needs: []string{"s1-2"}},
		{ID: "s1-2", Brief: "two, reworded\nDEPENDS-ON: s1-1", Needs: []string{"s1-1"}},
	}, Who: "coordinator"})
	require.Empty(t, p.Units, "the batch closes a cycle: %+v", p)
	require.Len(t, p.Refused, 2, "%+v", p.Refused)
	for _, r := range p.Refused {
		require.Contains(t, r.Why, "the needs would make a cycle", r.Key)
	}
	w.do(p)
	require.Empty(t, Cycles(w.s), "no cycle on the table")
}

// Every writer of needs sees a working card's stored needs (cycle.go,
// needGraph.needs reads every open card's named needs whatever its state):
// sentinel set. A dealt card W takes a need on a waiting sentinel S; S then may
// not need W, or the tick that moves W back to waiting leaves the two waiting on
// each other forever.
func TestSentinelSetSeesAWorkingCardsNeeds(t *testing.T) {
	t.Parallel()
	w := setup(t, 1) // s1-1 ready
	w.must(Add(w.s, AddReq{Stream: "g", IDs: []string{"S"}, Sentinel: true}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Only: []string{"s1-1"}}}))
	require.Equal(t, Working, w.state("s1-1"))
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "s1-1", Needs: []string{"S"}, Who: "coordinator"}))
	require.Equal(t, "S", w.s.Work.Card("s1-1").F("needs"))

	p := SentinelSet(w.s, SentinelSetReq{ID: "S", Needs: []string{"s1-1"}, Who: "coordinator"})
	require.Empty(t, p.Units, "sentinel set closes a loop through the working card: %+v", p)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "the needs would make a cycle")
}

// The same for a twin's relink: the waiting card that needed the old id would
// need the twin, a working card that needs it in turn.
func TestRelinkSeesAWorkingCardsNeeds(t *testing.T) {
	t.Parallel()
	w := setup(t, 3) // s1-1, s1-2, s1-3 ready
	w.must(Deal(w.s, DealReq{Sel: Sel{Only: []string{"s1-1"}}}))
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"s1-3"}, Who: "coordinator"}))
	require.Equal(t, Waiting, w.state("s1-2"))
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "s1-1", Needs: []string{"s1-2"}, Who: "coordinator"}))
	require.Equal(t, Working, w.state("s1-1"))

	p := Relink(w.s, RelinkReq{Old: []string{"s1-3"}, New: "s1-1", Who: "coordinator"})
	require.Empty(t, p.Units, "the relink closes a loop through the working card: %+v", p)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "the needs would make a cycle")
	require.Equal(t, "s1-3", w.s.Work.Card("s1-2").F("needs"), "nothing is written")
}
