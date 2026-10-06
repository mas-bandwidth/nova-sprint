package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAFinishAsksAFreeReaderInTheSameTick verifies that when a card finishes
// ok and moves to review, if a free reader with room is available, the read
// is asked in the same tick. This implements the timing requirement in
// docs/SPEC-SPRINT.md section 6 (cycle-time-breakdown): a finished card should
// start reading in the same tick when readers have room.
func TestAFinishAsksAFreeReaderInTheSameTick(t *testing.T) {
	t.Parallel()

	t.Run("finish with free reader: asked in same tick", func(t *testing.T) {
		t.Parallel()

		// World with 2 readers each with width 2, and 1 primary already in review
		w := widthWorld(t, 2, 2, 1)

		// Run the tick ask - should ask a read immediately
		_, due := tickAsk(t, w, nil)

		// Should have asked a read - no due items
		assert.Zero(t, due, "asked in same tick, nothing due")

		// The read should be placed
		pr := w.s.Work.Card("p1")
		live := liveReadsAt(w.s, pr, 1)
		require.Len(t, live, 1, "read asked in same tick")

		// No waiting note
		assert.Empty(t, w.notesOf(NWaitingForReader))
	})

	t.Run("finish with no free reader: wait until one frees", func(t *testing.T) {
		t.Parallel()

		// World with 2 readers each with width 1 - only 2 reads can be asked
		w := widthWorld(t, 1, 1, 3)

		// First ask: asks 2, 3rd waits
		_, due := tickAsk(t, w, nil)
		assert.Equal(t, 1, due, "the third read waits for room")

		// Should have a waiting note
		ns := w.notesOf(NWaitingForReader)
		require.Len(t, ns, 1)

		// Now free a reader by completing its current read
		s := w.s
		readCard := s.Readers.Column(Asked)[0]
		readCard.Col = OK
		s.Readers.cells = nil

		// Next tick should ask the waiting card
		_, due = tickAsk(t, w, nil)
		assert.Zero(t, due, "asked when reader freed")

		// The p3 card should now have a read
		pr := s.Work.Card("p3")
		live := liveReadsAt(s, pr, 1)
		require.Len(t, live, 1)
	})
}
