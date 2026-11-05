package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A friend who goes down keeps the cards she took and never started for the grace, then the
// tick returns them to the pool, its primary ready for someone up; a card she started (its
// progress stamp) stays hers, and a held friend's row is left to her hold (2026-11-05: emma
// went down holding 16 cards taken and not started, the roots of 60 waiting cards, for 22
// minutes, and friend take --all-unstarted refused them, "a take is recorded on it"). The
// clock is the world's: no real time.
func TestADownFriendReturnsTheCardsSheTookAndNeverStarted(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
	up := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	dealWith(w, up)
	for _, id := range []string{"s1-1.w1", "s1-2.w1"} {
		require.Equal(t, FriendRow("amy"), w.s.Fleet.Card(id).Row, "%s is dealt to amy", id)
	}
	// s1-1 carries a take and no start (her lane gone, its card back ready on her row, as
	// emma's were); s1-2 she started (a progress stamp)
	wc := w.s.Fleet.Card("s1-1.w1")
	wc.Fields["first_taken"] = stamp(w.s.Now)
	w.s.Fleet.Put(wc)
	wc = w.s.Fleet.Card("s1-2.w1")
	wc.Fields[FieldProgress] = stamp(w.s.Now)
	w.s.Fleet.Put(wc)

	down := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Down, Class: "flash,pro", AnsweredAt: w.s.Now})
	w.tick(FriendDownReturnGrace - time.Minute)
	dealWith(w, down)
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "inside the grace she keeps it")
	require.Equal(t, Ready, w.s.Fleet.Card("s1-1.w1").Col)

	// a held friend keeps her row to her hold
	held := down
	held.Held = true
	w.tick(2 * time.Minute)
	dealWith(w, held)
	require.Equal(t, Ready, w.s.Fleet.Card("s1-1.w1").Col, "held: the hold decides, not this rule")

	// past the grace, down: the card she never started goes back to the pool
	dealWith(w, down)
	c := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Withdrawn, c.Col, "past the grace: returned")
	require.Empty(t, c.F("first_taken"))
	require.Empty(t, c.F(FieldTakenFrom), "she is not barred from it")
	require.Equal(t, Ready, w.s.StateOf("s1-1"), "its primary is ready for someone up")
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row, "the card she started stays hers")
	require.True(t, w.s.Fleet.Card("s1-2.w1").Placed(), "not withdrawn")
}
