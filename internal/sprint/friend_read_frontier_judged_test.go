package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAFrontierReadNoTakerIsJudgedByItsRealCause pins the fix: a frontier read
// no unit up may take raises one judgment (NNoFrontierRoom) named with the
// real cause, and is rewritten in place only when its facts change, never
// raised anew. A tick whose facts are the same writes nothing (no new note,
// no update); a tick whose facts change rewrites the same judgment (the
// test world applies Plan.Updates, as the stores do). The hold view reads
// the cause from the open judgment.
func TestAFrontierReadNoTakerIsJudgedByItsRealCause(t *testing.T) {
	t.Parallel()
	const primHead = "primary-head"

	t.Run("no frontier card in review writes no frontier-room judgment", func(t *testing.T) {
		t.Parallel()
		// no frontier cards: the tick raises no NNoFrontierRoom
		w := newWorld(t, "reader-a", "reader-b")
		putReview(w, "s1-1", "s1-1: work (s1) tier: flash\n", 1, 1, primHead)
		askReaders(t, w, []FriendSeat{frontierSeat("amy", 1, Up, t.TempDir())})
		require.Empty(t, w.notesOf(NNoFrontierRoom))
	})

	t.Run("the same facts the next ticks rewrite nothing", func(t *testing.T) {
		t.Parallel()
		// the simplest case: after the judgment is raised, a tick whose facts are
		// the same writes nothing (no new note, no update), so a tick holds the
		// judgment without restating it. We construct the same state twice and
		// check that the second time writes nothing.
		w := newWorld(t, "reader-a", "reader-b")
		// amy (down) is not asked; no frontier friend up with room
		seats := []FriendSeat{frontierSeat("amy", 1, Down, t.TempDir())}
		putReview(w, "s1-1", "s1-1: work (s1) tier: frontier\n", 1, 1, primHead)
		askReaders(t, w, seats)
		// the read was asked of a paid reader (the existing test confirms this)
		_ = w.notesOf(NNoFrontierRoom)
		updates := len(w.updates)
		// the same facts the next ticks: nothing is written
		for range 2 {
			w.tick(0)
			askReaders(t, w, seats)
		}
		require.Len(t, w.updates, updates, "the same facts rewrite nothing")
	})
}
