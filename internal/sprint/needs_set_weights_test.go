package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every needs change writes the weights it changes (weight.go, weighUnits): a
// needs set re-points a primary's needs, so every primary whose weight moves must
// have its `behind` written by the same plan. Without that the next drop's plan
// refreshes each stale weight (steps_review.go, weighUnits), and a refused
// all-or-none drop then names every refreshed card as refused: the one-card drop
// that printed 18 REFUSED lines.
func TestNeedsSetWritesTheWeightsItChanges(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	for _, id := range []string{"A", "B", "C"} {
		w.must(Add(w.s, AddReq{Stream: "s", IDs: []string{id}, Brief: id, Who: "coordinator"}))
	}
	require.Equal(t, Ready, w.state("A"))
	require.Equal(t, Ready, w.state("B"))
	require.Equal(t, Ready, w.state("C"))

	// Case 1: B needs A. A's weight is now 1, and the edit writes it.
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "B", Needs: []string{"A"}, Who: "coordinator"}))
	assert.Equal(t, 1, w.s.Work.Card("A").Int(FieldBehind), "A carries B behind it")
	assert.Equal(t, Weights(w.s)["A"], w.s.Work.Card("A").Int(FieldBehind), "A's behind is its weight")
	for id, n := range Weights(w.s) {
		assert.Equal(t, n, w.s.Work.Card(id).Int(FieldBehind), "%s behind", id)
	}

	// Case 2: from the state the edit left, nothing is stale, so a drop plans no
	// weight units (weightOnly): the next drop cannot refresh a card and name it
	// refused.
	p := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"C"}}, Reason: "not wanted", Who: "coordinator"})
	var weights []Unit
	for _, u := range p.Units {
		if weightOnly(u) {
			weights = append(weights, u)
		}
	}
	assert.Empty(t, weights, "nothing stale: %+v", weights)
	w.must(p)

	// Case 3: clearing B's needs takes A's weight back to zero, and the edit
	// unsets it.
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "B", Clear: true, Who: "coordinator"}))
	assert.Equal(t, "", w.s.Work.Card("A").F(FieldBehind), "A is behind nothing")
	assert.Equal(t, 0, Weights(w.s)["A"], "A weighs nothing")
}
