package config

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// withOldFriendSlots is the reversed witness: the friend kind as it was before
// 2026-10-13, a required slots field beside width. The checks below that the
// design passes are refused by it, so they would see a friend slots field if
// one came back.
func withOldFriendSlots(friend *Kind) *Kind {
	old := *friend
	old.Fields = append([]Field{{Name: "slots", Type: TypeInt, Required: true, Help: "the friend's desired slots, under the ceiling of the machine the beat reports"}}, slices.Clone(friend.Fields)...)
	return &old
}

// friendIsWidthOnly is what the test holds of a friend kind: no slots field,
// and a row of width 16 and tiers only is a whole friend row.
func friendIsWidthOnly(k *Kind) error {
	if _, found := k.Field("slots"); found {
		return &RefusedError{Detail: "the friend kind has a slots field"}
	}
	_, err := k.NewRow("stella", map[string]string{"width": "16", "tiers": "frontier"})
	return err
}

// TestAFriendRowIsWidthOnlyAndNoCeilingRefusesIt: a friend's capacity is her
// width and nothing else (the owner, 2026-10-13: "there is only width in
// nova-config. I say, stella is width 16. she is. done."). nova-sprint's copy
// of the friend kind has no slots field, Check takes width 16 with no slots,
// --slots is refused as no friend field, and a friend of width 16 is stored
// beside a machine of one slot with no refusal. The reversed witness (the old
// required slots field put back) is refused by the same checks. The Lua side
// (no CEILING or NOCEILING for a friend) is pkg/nsprint/fn's
// TestAFriendIsNeverRefusedForCapacityAtAnyWidth; the model is nova-tools'
// tla/ConfigApply.tla, FriendWidthNeverRefused.
func TestAFriendRowIsWidthOnlyAndNoCeilingRefusesIt(t *testing.T) {
	t.Parallel()

	friend, ok := Lookup(KindFriend)
	require.True(t, ok)
	require.NoError(t, friendIsWidthOnly(friend), "the friend kind is not width only")
	for _, f := range friend.Fields {
		require.NotEqual(t, "slots", f.Name, "the friend kind has a slots field; a friend row is width, tiers, roles and the rest")
	}

	row := Row{Name: "stella", Fields: map[string]string{"width": "16", "tiers": "frontier"}}
	require.NoError(t, checkFriend(row), "checkFriend refused width 16 with no slots")
	_, err := friend.NewRow("stella", map[string]string{"width": "16", "tiers": "frontier", "slots": "8"})
	require.ErrorContains(t, err, "--slots is not a friend field", "a friend row still takes --slots")
	for _, w := range []int{1, 16, 64, 1000} {
		_, err := friend.NewRow("stella", map[string]string{"width": strconv.Itoa(w), "tiers": "frontier"})
		require.NoError(t, err, "friend set --width %d was refused", w)
	}
	_, err = friend.NewRow("stella", map[string]string{"width": "0", "tiers": "frontier"})
	require.Error(t, err, "width 0 was taken: a friend's width is at least 1")

	t.Run("a friend of width 16 is stored beside a one-slot coordinator machine", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		st := NewMem()
		machine, _ := Lookup(KindMachine)
		m, err := machine.NewRow("m", map[string]string{"user": "u", "seat": "m", "slots": "1"})
		require.NoError(t, err)
		_, err = st.Insert(ctx, KindMachine, m, "rowan")
		require.NoError(t, err)
		_, _, err = st.Update(ctx, KindFleet, KindFleet, map[string]string{"coordinator": "m"}, "rowan")
		require.NoError(t, err)
		x, err := friend.NewRow("x", map[string]string{"width": "16", "tiers": "frontier,pro", "roles": "builder"})
		require.NoError(t, err)
		_, err = st.Insert(ctx, KindFriend, x, "rowan")
		require.NoError(t, err, "a friend of width 16 was refused beside a machine of one slot")
		got, found, err := st.Get(ctx, KindFriend, "x")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "16", got.Fields["width"])
		require.Equal(t, 16, FriendWidth(got))
		_, has := got.Fields["slots"]
		require.False(t, has, "the stored friend row carries slots: %v", got.Fields)
	})

	t.Run("reversed witness: the old friend slots field is refused by the same checks", func(t *testing.T) {
		t.Parallel()
		old := withOldFriendSlots(friend)
		require.Error(t, friendIsWidthOnly(old), "the old kind with a required slots field passed as width only: the witness is not red")
		_, err := old.NewRow("stella", map[string]string{"width": "16", "tiers": "frontier"})
		require.ErrorContains(t, err, "--slots is required", "the old kind took a friend row with no slots")
	})
}
