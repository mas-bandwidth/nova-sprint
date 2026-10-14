package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Unit tests for the pure inbox helpers of internal/sprint/inbox.go: Stalled, StaleStream
// and NoteCommands. Each covers the function's main path and one refusal; the inputs are
// plain time values and the outputs are plain values.

// TestInboxCoverStalled covers StreamClock.Stalled (inbox.go:33): a stream that
// has not landed and whose last progress is older than stale is stalled; a
// landed stream, a zero stale, a zero progress, or fresh progress is refused
// the staleness.
func TestInboxCoverStalled(t *testing.T) {
	t.Parallel()
	progress := t0.Add(-2 * time.Hour)
	for _, tt := range []struct {
		name  string
		clk   StreamClock
		stale time.Duration
		want  bool
	}{
		{"not landed and past the stale mark", StreamClock{State: StreamWaiting, Progress: progress}, time.Hour, true},
		{"the stream landed: not stalled", StreamClock{State: StreamLanded, Progress: progress}, time.Hour, false},
		{"a zero stale: not stalled", StreamClock{State: StreamWaiting, Progress: progress}, 0, false},
		{"no progress yet: not stalled", StreamClock{State: StreamWaiting}, time.Hour, false},
		{"progress is fresh: not stalled", StreamClock{State: StreamWaiting, Progress: t0.Add(-time.Minute)}, time.Hour, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.clk.Stalled(t0, tt.stale))
		})
	}
}

// TestInboxCoverStaleStream covers StaleStream (inbox.go:142): a stale:<card id>
// group id yields the card id it names; anything else is refused.
func TestInboxCoverStaleStream(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, id, wantID string
		wantOk           bool
	}{
		{"a stale group id yields its card id", StaleGroupID("s1", 7), "s1", true},
		{"a regular note id is refused", "n1", "", false},
		{"the stale prefix alone is refused", "stale:", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := StaleStream(tt.id)
			assert.Equal(t, tt.wantID, got)
			assert.Equal(t, tt.wantOk, ok)
		})
	}
}

// TestInboxCoverNoteCommands covers NoteCommands (inbox.go:350): an open
// judgment's decisions render as the commands that make them, one command each;
// a note with no decisions renders none.
func TestInboxCoverNoteCommands(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		note Note
		want []Command
	}{
		{"a blocked judgment renders its ack command",
			Note{ID: "n1", Kind: Judgment, Type: NBlocked, Stream: "s1", Decisions: []string{"ack"}},
			[]Command{{Decision: "ack", Lines: []string{"nova-sprint ack n1 --reason " + noneText}}}},
		{"a note with no decisions renders no commands", Note{ID: "n2", Kind: Judgment, Type: NBlocked}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NoteCommands(tt.note, []string{"p1"}))
		})
	}
}
