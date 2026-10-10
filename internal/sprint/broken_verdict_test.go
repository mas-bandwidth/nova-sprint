package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// BrokenVerdictAtSameHead: a card at its brief bound with a coordinator broken
// verdict and a fleet ok at the same head is not accepted and returns to work
// with the finding. A broken verdict at the same head outweighs any number of oks.
func TestBrokenVerdictAtSameHead(t *testing.T) {
	t.Parallel()
	// Use a 2-reader setup to have one ok and one broken read at the same head
	w := setup(t, 2)
	// Create a primary in review with work done
	finished(w, "s1-1", false)

	// Ask for reads
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))

	// Get the read cards for s1-1
	pr := w.s.Work.Card("s1-1")
	rcs := readsAt(w.s, pr, 1)
	require.Len(t, rcs, 2, "need two reads for a pro card")

	// Report one read as ok
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rcs[0].Row, Verdict: "ok", Sel: Sel{IDs: []string{rcs[0].ID}}}))

	// Report another read as broken at the same head
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rcs[1].Row, Verdict: "broken", Finding: "f:1", Sel: Sel{IDs: []string{rcs[1].ID}}}))

	// Now check judgment - should have NReadBroken open
	got := openTypes(w, "s1-1")
	require.Equal(t, NReadBroken, got, "broken verdict should be open: %q", got)

	// Tick should NOT accept when there's a broken verdict
	require.False(t, tickTakes(w, "s1-1"), "tick should not accept when broken verdict exists")

	w.clean("brokenVerdict")
}
