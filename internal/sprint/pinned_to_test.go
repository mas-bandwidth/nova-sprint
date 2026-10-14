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
	free := &Card{ID: "p1-2", Fields: map[string]string{}}
	cases := []struct {
		name string
		card *Card
		who  string
		want bool
	}{
		{"a hard pin to amy is amy's pin", pin, "amy", true},
		{"a hard pin to amy dealt to bea is not bea's pin: her start bound and level may move it", pin, "bea", false},
		{"a card with no pin is nobody's pin", free, "amy", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, pinnedTo(tc.card, tc.who))
		})
	}
}
