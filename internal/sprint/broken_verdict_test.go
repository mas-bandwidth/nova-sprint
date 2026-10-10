package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// BrokenVerdictAtSameHead: a card at its brief bound with a coordinator broken
// verdict and ok reads at the same head is not accepted and returns to work
// with the finding. A broken verdict at the same head outweighs any number of oks.
func TestBrokenVerdictAtSameHead(t *testing.T) {
	t.Parallel()
	// Use a 3-reader setup with a pro card (ReadsNeeded=2). Two readers report
	// ok and a third reader reports broken at the same head. The broken should
	// block acceptance even though there are 2 oks (sufficient for a pro card).
	w := setup(t, 3)
	// Create a primary in review with work done
	finished(w, "s1-1", false)

	// Ask for reads (pro card needs 2, asks 2 readers)
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))

	// Get the read cards for s1-1
	pr := w.s.Work.Card("s1-1")
	rcs := readsAt(w.s, pr, 1)
	require.Len(t, rcs, 2, "need two reads for a pro card")

	// Report both reads as ok
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rcs[0].Row, Verdict: "ok", Sel: Sel{IDs: []string{rcs[0].ID}}}))
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rcs[1].Row, Verdict: "ok", Sel: Sel{IDs: []string{rcs[1].ID}}}))

	// Ask another reader (third reader)
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true}))

	// Get the new read cards (should be 3 now)
	rcs = readsAt(w.s, pr, 1)
	require.Len(t, rcs, 3, "need three reads after asking another reader")

	// Report the third read as broken at the same head
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rcs[2].Row, Verdict: "broken", Finding: "f:1", Sel: Sel{IDs: []string{rcs[2].ID}}}))

	// Now check judgment - should have NReadBroken open
	got := openTypes(w, "s1-1")
	require.Equal(t, NReadBroken, got, "broken verdict should be open: %q", got)

	// Acceptable should be false when there's a broken read at the head
	// even with 2 oks (sufficient for a pro card). The broken outweighs any number of oks.
	require.False(t, acceptable(w.s, pr), "card should not be acceptable with broken read at head even with 2 oks")

	// Tick should NOT accept when there's a broken read at the head
	require.False(t, tickTakes(w, "s1-1"), "tick should not accept when broken read at head exists even with enough oks")

	w.clean("brokenVerdict")
}
