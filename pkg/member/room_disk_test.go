package member

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAMemberUnderTheDiskFloorStartsNoCard is the disk floor's room
// (DiskRoom): a member under floorPct ( free starts no card, with a why naming
// the percent free and the floor; an unreadable disk is reported but does not
// stop the member; floor 0 turns the floor off; and composed with a Room that
// refuses, the result refuses with both whys.
func TestAMemberUnderTheDiskFloorStartsNoCard(t *testing.T) {
	t.Parallel()

	// 1. 10 GiB free of 100 at floor 15: false, why names 10% and 15%.
	room := DiskRoom("/bench", 15, func(string) (uint64, uint64, error) {
		return 10 << 30, 100 << 30, nil
	})
	ok, why := room()
	require.False(t, ok)
	require.Contains(t, why, "10%")
	require.Contains(t, why, "15%")

	// 2. 20 of 100 at floor 15: true.
	room = DiskRoom("/bench", 15, func(string) (uint64, uint64, error) {
		return 20 << 30, 100 << 30, nil
	})
	ok, _ = room()
	require.True(t, ok)

	// 3. floor 0 is off: true at 1 of 100.
	room = DiskRoom("/bench", 0, func(string) (uint64, uint64, error) {
		return 1 << 30, 100 << 30, nil
	})
	ok, _ = room()
	require.True(t, ok)

	// 4. a free func that fails: true, and the why names the error.
	boom := errors.New("disk gone")
	room = DiskRoom("/bench", 15, func(string) (uint64, uint64, error) {
		return 0, 0, boom
	})
	ok, why = room()
	require.True(t, ok)
	require.Contains(t, why, "disk gone")

	// 5. composed with a Room that refuses: false, both whys present.
	refusing := func() (bool, string) { return false, "the usage source stopped answering" }
	composed := ComposeRooms(refusing, DiskRoom("/bench", 15, func(string) (uint64, uint64, error) {
		return 10 << 30, 100 << 30, nil
	}))
	ok, why = composed()
	require.False(t, ok)
	require.Contains(t, why, "the usage source stopped answering")
	require.Contains(t, why, "10%")
}
