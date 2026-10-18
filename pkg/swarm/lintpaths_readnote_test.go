package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-18: a START entry or a path in THE TASK was read-only only when it carried the
// bare token `(read)`. The marking people write says why: `(read: the shape to copy)` or
// `(read; copied by hand)`, and a file THE TASK names three times was marked once and
// counted unmarked at the other two, so paths-cover-named asked PATHS to cover a file the
// card only reads. A marking with a note is a marking, and one marking of a file in THE
// TASK covers every mention of it.
func TestAReadMarkingMayCarryANoteAndOneMarkingPerFileIsEnough(t *testing.T) {
	t.Parallel()
	for _, v := range []string{
		"internal/a.go (read)",
		"internal/a.go (read: the shape to copy)",
		"internal/a.go (read; copied by hand)",
		"internal/a.go (Read: the shape to copy)",
	} {
		es := startEntries(v)
		require.Len(t, es, 1, v)
		assert.True(t, es[0].read, "START %q marks the file read-only", v)
	}
	// a note that only begins with the letters is no marking
	es := startEntries("internal/a.go (reader: the loop)")
	require.Len(t, es, 1)
	assert.False(t, es[0].read, "(reader: ...) is not (read)")

	for _, task := range []string{
		"Copy the shape of internal/a.go (read: the shape to copy).",
		"Copy the shape of internal/a.go (read; copied by hand).",
		"internal/a.go (read) is the model. Then internal/a.go is named again here, and again in internal/a.go's own words.",
		"Later mentions come first: internal/a.go is named here before it is marked, then internal/a.go (read: why).",
	} {
		assert.Empty(t, taskPaths(task), "no path of %q is a file the card changes", task)
	}
	assert.Equal(t, []string{"internal/b.go"}, taskPaths("Change internal/b.go; internal/a.go (read: why) is the model, and internal/a.go again."),
		"an unmarked file is still named")
}
