package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The level evens every card a friend holds but a hard pin (WHO: only friend): friends
// first gives friends cards with no WHO line and cards preferring a named friend, so those
// move from a friend's ready backlog to a friend up with an idle lane like WHO: friend.
func TestTheLevelMovesUnpinnedCardsToAnIdleFriend(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, "c: no WHO\n\nThe task.", friendBrief("friend amy"), friendBrief("only friend amy"))
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}})
	dealStarted(w, amy, workerSeatFixture(FriendSeat{Name: "bob", Width: 1, Status: Held, Tiers: []string{cardhdr.RouteFlash}}))
	require.Equal(t, 1, w.s.Fleet.Count(FriendRow("amy"), Working))
	require.Equal(t, 1, w.s.Fleet.Count(FriendRow("amy"), Ready), "room 2: one working, one ready behind")
	ready := w.s.Fleet.Cell(FriendRow("amy"), Ready)[0]
	require.False(t, OnlyFriend(w.s.Primary(ready.F("primary"))), "the ready card behind her is not the hard pin")

	// bob comes up idle: the tick's level moves amy's unpinned ready card to him
	dealStarted(w, amy, workerSeatFixture(FriendSeat{Name: "bob", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}))
	moved := w.s.Fleet.Card(ready.ID)
	assert.Equal(t, FriendRow("bob"), moved.Row, "an unpinned card is levelled to the idle friend")
	assert.Equal(t, Working, moved.Col)
	assert.Equal(t, "amy", moved.F(FieldFriendsLeft))
	assert.Empty(t, Check(w.s, nil))
}

// A friend is dealt a card only when her tiers hold its tier, whatever its WHO line: a
// frontier card goes to the friend with frontier, never to a friend it names without it
// or to a friend whose row names no tier; a hard pin to a friend without the tier waits.
func TestAFrontierCardGoesOnlyToAFriendWithFrontier(t *testing.T) {
	t.Parallel()
	brief := func(who string) string { return "c: work tier: frontier\nWHO: " + who + "\n\nThe task." }
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 4, Status: Up, Tiers: []string{cardhdr.RouteFlash, cardhdr.RoutePro}})
	dan := workerSeatFixture(FriendSeat{Name: "dan", Width: 8, Status: Up})
	cat := workerSeatFixture(FriendSeat{Name: "cat", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFrontier}})

	w := friendWorld(t, brief("friend amy"), brief("friend"))
	dealStarted(w, amy, dan, cat)
	assert.Equal(t, FriendRow("cat"), w.s.Fleet.Card("s1-1.w1").Row, "named amy, but only cat has frontier")
	assert.Equal(t, FriendRow("cat"), w.s.Fleet.Card("s1-2.w1").Row, "any friend: cat, not dan whose row names no tier")

	only := friendWorld(t, brief("only friend amy"))
	dealStarted(only, amy, dan, cat)
	assert.Nil(t, only.s.Fleet.Card("s1-1.w1"), "a hard pin to amy without frontier waits for her")
	assert.Equal(t, Ready, only.s.StateOf("s1-1"))
}
