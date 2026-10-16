package sprint

// The one test that seeds the retired stall ladder's record by its raw key: it is the
// history the regression is about, so this file is listed in the retired-name guard's
// history (internal/ci/retired_presence_test.go, retiredHistory) and is the only place
// the key may appear.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// An up, unheld friend with an empty row is dealt by the tick whatever stale mark the
// retired stall ladder left on the live store (the fleet prop "friend_stall_down.<friend>"):
// the mark was released only by her activity, and a friend dealt nothing has no card to be
// active on, so she was never dealt again (2026-10-15 live, v1.3.8: aurora and stella up
// with empty rows of width 8, marked at rung 5 at 00:39Z and 00:40Z, while cards waited
// ready and the coordinator pass raised "an up friend has an empty row while cards wait").
// The sprint's friend states are up, down and held alone (the owner, 2026-10-14); the ladder
// is gone (v1.3.6) and its leftover record must gate nothing.
func TestAnUpFriendTheStallLadderMarkedIsDealtByTheTick(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"), friendBrief("friend"), friendBrief("friend"))
	w.s.Fleet.SetProp("friend_stall_down.aurora", stamp(w.s.Now.Add(-time.Hour)))
	seats := workerSeatFixtures([]FriendSeat{{Name: "aurora", Width: 8, Status: Up, Class: "flash,pro"}})

	dealWith(w, seats...)

	row := FriendRow("aurora")
	assert.Equal(t, 3, w.s.Fleet.Count(row, Ready)+w.s.Fleet.Count(row, Working), "an up friend with an empty row is dealt the cards that wait, stale stall mark or no mark")
}
