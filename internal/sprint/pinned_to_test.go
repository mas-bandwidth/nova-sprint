package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A hard pin released past its bound and dealt to another friend is that friend's ordinary card:
// it is pinned to the friend it names, and to no other.
func TestAHardPinIsPinnedToTheFriendItNamesOnly(t *testing.T) {
	t.Parallel()
	pin := &Card{ID: "p1-1", Fields: map[string]string{FieldWho: "only.friend.amy"}}
	assert.True(t, pinnedTo(pin, "amy"), "a hard pin to amy is amy's pin")
	assert.False(t, pinnedTo(pin, "bea"), "a hard pin to amy dealt to bea is not bea's pin: her start bound and level may move it")
	free := &Card{ID: "p1-2", Fields: map[string]string{}}
	assert.False(t, pinnedTo(free, "amy"), "a card with no pin is nobody's pin")
}
