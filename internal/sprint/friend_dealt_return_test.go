package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A card dealt to a friend and not taken within the dealt bound returns to the pool and is
// dealt to the next capable member, and a friend that has not taken a dealt card is dealt no
// more until it takes one (docs/SPEC-SPRINT.md section 1, a friend's card; tla/DealFill.tla
// Return and DealtNoMoreHolds). At the start the deal put every card on two friends whose
// daemons could not take them and the idle fleet machines got nothing; after the bound every
// card is on a machine.
func TestAFriendWhoNeverTakesReturnsHerCardsToTheFleet(t *testing.T) {
	t.Parallel()
	briefs := make([]string, 8)
	for i := range briefs {
		briefs[i] = fleetBrief("flash")
	}
	w := friendWorld(t, briefs...)
	w.s.Work.SetProp(PropDealtMax, "90s")
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	bob := FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash,pro"}

	// friends first: the two friends who never take hold all eight cards, the machines none
	dealWith(w, amy, bob)
	for i := 1; i <= 8; i++ {
		wc := w.s.Fleet.Card("s1-" + itoa(i) + ".w1")
		require.NotNil(t, wc, "s1-%d is dealt", i)
		require.True(t, IsFriendRow(wc.Row), "s1-%d sits untaken on a friend's row", i)
	}

	// past the dealt bound the tick returns them to the pool and marks the friends dealt no
	// more; the next tick's machines' deal takes every one
	w.tick(2 * time.Minute)
	dealWith(w, amy, bob) // the return
	dealWith(w, amy, bob) // the machines' deal
	for i := 1; i <= 8; i++ {
		wc := w.s.Fleet.Card("s1-" + itoa(i) + ".w1")
		require.NotNil(t, wc, "s1-%d is still dealt", i)
		require.False(t, IsFriendRow(wc.Row), "s1-%d is on a machine after the bound", i)
	}
	w.clean("returned and dealt to the machines")
}
