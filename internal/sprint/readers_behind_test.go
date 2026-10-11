package sprint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The readers are behind is a standing judgment (docs/SPEC-SPRINT.md, the table's "updated
// in place every tick while it holds"): its subject is the readers, not the review queue
// behind them, so a review count that moves while the readers read and wait as before is
// no material change and the judgment's text holds (notify keys it by type and subject and
// rewrites it only when the text moves). Ten ticks of a moving review queue raise one
// judgment whose text is the same every tick.
func TestReadersBehindWhatHoldsWhenOnlyReviewMoves(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	s := &Snapshot{Now: now, Work: NewTable(Work), Readers: NewTable(Readers)}
	s.Readers.SetRows([]string{"r1"})
	asked := now.Add(-(ReadersWindow + time.Minute)).Format(time.RFC3339)
	s.Readers.Put(&Card{ID: "r1-1", Row: "r1", Col: Asked, Fields: map[string]string{"asked": asked}})
	s.Work.SetRows([]string{"s1"})
	for i := 1; i <= 6; i++ {
		s.Work.Put(&Card{ID: fmt.Sprintf("s1-%d", i), Row: "s1", Col: Review})
	}
	b, ok := ReadersBehind(s)
	require.True(t, ok, "a read waited the window on a reader up with room: the readers are behind")
	first := b.What()
	// Ten ticks with review growing and the readers as they were: one judgment, its text held.
	for i := 7; i <= 16; i++ {
		s.Work.Put(&Card{ID: fmt.Sprintf("s1-%d", i), Row: "s1", Col: Review})
		b, ok = ReadersBehind(s)
		require.True(t, ok)
		require.Equal(t, first, b.What(), "tick %d: review moved and the readers did not, so the judgment's text holds", i-5)
	}
}

// A material change is a change in the readers themselves: a read begins and the waiting
// count falls, so the judgment's text moves and the tick rewrites it in place.
func TestReadersBehindWhatMovesOnMaterialChange(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	s := &Snapshot{Now: now, Work: NewTable(Work), Readers: NewTable(Readers)}
	s.Readers.SetRows([]string{"r1"})
	asked := now.Add(-(ReadersWindow + time.Minute)).Format(time.RFC3339)
	s.Readers.Put(&Card{ID: "r1-1", Row: "r1", Col: Asked, Fields: map[string]string{"asked": asked}})
	s.Readers.Put(&Card{ID: "r1-2", Row: "r1", Col: Asked, Fields: map[string]string{"asked": asked}})
	s.Work.SetRows([]string{"s1"})
	s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: Review})
	b, ok := ReadersBehind(s)
	require.True(t, ok)
	before := b.What()
	// the reader begins one of the two: the waiting count falls from one to none, a material change
	s.Readers.Put(&Card{ID: "r1-1", Row: "r1", Col: Reading, Fields: map[string]string{"stream": "s1"}})
	b, ok = ReadersBehind(s)
	require.True(t, ok, "one read still waits past the window")
	after := b.What()
	require.NotEqual(t, before, after, "a read began and another still waits: the judgment's numbers moved, so its text moves")
}
