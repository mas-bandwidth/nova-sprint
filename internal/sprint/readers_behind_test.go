package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestReadersBehindWhatIsStable tests that when the readers behind condition
// is unchanged, the judgment text is identical. This ensures that the judgment
// is not pushed again on every tick when the condition hasn't changed materially.
func TestReadersBehindWhatIsStable(t *testing.T) {
	t.Parallel()
	s := &Snapshot{
		Now:        time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		Work:       NewTable(Work),
		Readers:    NewTable(Readers),
		Fleet:      NewTable(Fleet),
	}

	// Set up readers
	s.Readers.SetRows([]string{"r1", "r2"})
	
	// Add asked reads (not begun)
	askedTime := s.Now.Add(-(ReadersWindow + time.Minute))
	s.Readers.Put(&Card{ID: "r1-1", Row: "r1", Col: Asked, Fields: map[string]string{"asked": askedTime.Format(time.RFC3339), "stream": "test"}})
	s.Readers.Put(&Card{ID: "r2-1", Row: "r2", Col: Asked, Fields: map[string]string{"asked": askedTime.Format(time.RFC3339), "stream": "test"}})

	// Set up fleet members with width
	s.Fleet.Put(&Card{ID: "m1", Row: "m1", Col: Up, Fields: map[string]string{"width": "10"}})

	// First check
	b1, ok1 := ReadersBehind(s)
	require.True(t, ok1, "expected readers to be behind")
	w1 := b1.What()

	// Second check with same state - should produce same text
	b2, ok2 := ReadersBehind(s)
	require.True(t, ok2, "expected readers to still be behind")
	w2 := b2.What()

	require.Equal(t, w1, w2, "What() should return identical text for identical input")
}

// TestReadersBehindWhatChangesOnMaterialChange tests that the judgment text
// changes when the underlying facts change (e.g., when a reader catches up).
func TestReadersBehindWhatChangesOnMaterialChange(t *testing.T) {
	t.Parallel()
	s := &Snapshot{
		Now:        time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		Work:       NewTable(Work),
		Readers:    NewTable(Readers),
		Fleet:      NewTable(Fleet),
	}

	s.Readers.SetRows([]string{"r1"})
	askedTime := s.Now.Add(-(ReadersWindow + time.Minute))
	s.Readers.Put(&Card{ID: "r1-1", Row: "r1", Col: Asked, Fields: map[string]string{"asked": askedTime.Format(time.RFC3339), "stream": "test"}})
	s.Fleet.Put(&Card{ID: "m1", Row: "m1", Col: Up, Fields: map[string]string{"width": "10"}})

	// First check - reader is behind
	_, ok1 := ReadersBehind(s)
	require.True(t, ok1)

	// Reader begins reading (material change)
	s.Readers = NewTable(Readers)
	s.Readers.SetRows([]string{"r1"})
	s.Readers.Put(&Card{ID: "r1-1", Row: "r1", Col: Reading, Fields: map[string]string{"stream": "test"}})

	// Second check - reader has caught up (late count is now 0)
	_, ok2 := ReadersBehind(s)
	require.False(t, ok2, "reader is now reading, not behind")
}
