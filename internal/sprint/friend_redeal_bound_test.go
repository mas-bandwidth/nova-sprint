package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's card past its deadline is a redeal, never a late judgment and never a failure
// (docs/SPEC-SPRINT.md section 1; TickFriendRedeal, verdicts.go): its work card goes to her
// redealt cell and its primary waits ready, and the friend the next deal gives its next
// attempt runs her own whole deadline from her own deal, not from the first friend's deal or
// the redeal. This replaces the old friend-take bound coverage (the take-back measured from
// the next friend's deal, WordNeverTaken, FieldTakenFrom, RuleFriendTake): a friend's late
// card is a redeal, not a take-back, so rule friend-take has nothing to answer against her.

// TestAFriendsRedealtCardGetsTheNextFriendsOwnBound is the redeal rule's bound: the deadline
// the redeal measures is the friend's own, and the next friend dealt its next attempt gets
// her own whole deadline, never the first friend's spent one.
func TestAFriendsRedealtCardGetsTheNextFriendsOwnBound(t *testing.T) {
	t.Parallel()

	t.Run("past her deadline the card is redealt, never taken back", func(t *testing.T) {
		t.Parallel()
		w, _ := friendDealt(t)
		wc := w.s.Fleet.Card("s1-1.w1")
		assert.Equal(t, Ready, wc.Col, "dealt to amy, ready past her bound")
		assert.Empty(t, openOf(w, NWorkLate, "s1-1"), "no late judgment on a friend's card")
		w.part(TickFriendRedeal, TickReq{})
		wc = w.s.Fleet.Card("s1-1.w1")
		assert.Equal(t, Redealt, wc.Col, "redealt on her row, not taken back")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"), "its primary waits ready for the next deal")
		assert.Empty(t, openOf(w, NWorkLate, "s1-1"), "never a late judgment")
		assert.Empty(t, logged(w, RuleFriendTake), "never a friend-take: the redeal is no take-back")
		w.clean("redealt past its deadline")
	})

	t.Run("the next friend's deadline runs from her own deal, not the first friend's", func(t *testing.T) {
		t.Parallel()
		w, bob := friendDealt(t)
		w.part(TickFriendRedeal, TickReq{})
		require.Equal(t, Redealt, w.s.Fleet.Card("s1-1.w1").Col, "redealt on amy's row")

		// the next deal gives bob the next attempt; her own deal starts her own deadline, so
		// amy's eight hours spent on the card are not bob's
		dealStarted(w, bob)
		wc := w.s.Fleet.Card("s1-1.w2")
		require.NotNil(t, wc, "the next deal deals the next attempt")
		assert.Equal(t, FriendRow("bob"), wc.Row, "dealt to the next friend")
		assert.Equal(t, Working, wc.Col, "bob started it: working on her row")
		w.part(TickFriendRedeal, TickReq{})
		assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w2").Col, "inside her own deadline: still hers, not redealt")

		// past her own deadline it is redealt too
		w.tick(DeadlineUnfinished * 4)
		w.part(TickFriendRedeal, TickReq{})
		assert.Equal(t, Redealt, w.s.Fleet.Card("s1-1.w2").Col, "past her own deadline: redealt on her row")
		w.clean("redealt after her own deadline")
	})
}
