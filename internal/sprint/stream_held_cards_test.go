package sprint

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHeldBackByStreamCountsEachStreamsHeldCards pins the per-stream held
// count the dashboard needs (a-waiting-stream-is-never-shown-held): a stream's
// held cards are its waiting primaries no tick moves on its own (behind a
// sentinel not released, admitted held, or waiting on one of those through a
// need), counted per stream. The stream's stored state stays its own fact: a
// waiting stream with held cards is waiting, never held. No real time,
// sockets, network or services.
func TestHeldBackByStreamCountsEachStreamsHeldCards(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	// gates holds the sentinel the waiters wait on.
	w.must(Add(w.s, AddReq{Stream: "gates", IDs: []string{"gate"}, Sentinel: true}))
	// waiters: every card waits on the gates stream's sentinel.
	w.must(Add(w.s, AddReq{Stream: "waiters", IDs: []string{"w1", "w2"}, Needs: []string{"gate"}}))
	// heldonly: a stream whose only card was admitted held (a card hold).
	w.must(Add(w.s, AddReq{Stream: "heldonly", IDs: []string{"h1"}, Held: true}))
	// plain: a card waiting on a ready card of another stream is not held.
	w.must(Add(w.s, AddReq{Stream: "readys", IDs: []string{"r1"}}))
	w.must(Add(w.s, AddReq{Stream: "plain", IDs: []string{"p1"}, Needs: []string{"r1"}}))

	by := HeldBackByStream(w.s)
	assert.Equal(t, 2, by["waiters"], "both waiters wait behind the sentinel")
	assert.Equal(t, 1, by["heldonly"], "the admitted-held card counts on its own stream")
	assert.Equal(t, 1, by["gates"], "the sentinel itself is held back on its stream")
	assert.Zero(t, by["plain"], "waiting on a ready card is not a held card")
	assert.Zero(t, by["readys"], "a ready card is not held")
	sum := 0
	for _, n := range by {
		sum += n
	}
	assert.Equal(t, HeldBack(w.s), sum, "the per-stream counts sum to the held total")

	// The clock carries the count beside the stored state, and the JSON
	// names it held_cards only when there are held cards to name.
	clock := StreamClock{Stream: "waiters", State: StreamWaiting, HeldCards: by["waiters"]}
	raw, err := json.Marshal(clock)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"held_cards":2`)
	assert.Contains(t, string(raw), `"State":"waiting"`)
	bare, err := json.Marshal(StreamClock{Stream: "plain", State: StreamWaiting})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "held_cards", "no held cards, no held_cards: %s", bare)
}
