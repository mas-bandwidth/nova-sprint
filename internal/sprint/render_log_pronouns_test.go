package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderingALegacyFriendNoteLeavesItsStoredTypeUntouched(t *testing.T) {
	t.Parallel()
	note := Note{Kind: Judgment, Type: "a card pinned to one friend waits while she is not up", What: "card p1-1 waits for friend pat"}
	line := renderNote(Line{Note: &note})
	assert.Contains(t, line, "a card pinned to one friend waits while the friend is not up")
	assert.Equal(t, "a card pinned to one friend waits while she is not up", note.Type)
}
