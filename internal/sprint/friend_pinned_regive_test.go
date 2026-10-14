package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A hard-pinned card taken back from its friend is given again by the tick's friend deal
// (docs/SPEC-SPRINT.md section 1, a friend's card; held.go's Ready case, OnlyFriend branch:
// "give it back"). When her work card is withdrawn and FieldTakenFrom names the pinned
// friend, and she is up with a free lane below her room (DealAhead times her width) and her
// tiers hold its tier, the tick gives it back in the same tick, the same step nova-sprint
// friend give takes, with one happened note; while she is down, held, has no room, or every
// lane is busy, no judgment is raised for the wait.

// pinnedTakenBack deals a hard-pinned card to amy, takes it back from her (friend take:
// her work card withdrawn, FieldTakenFrom naming her), and leaves her up with a free lane.
func pinnedTakenBack(t *testing.T, seats ...FriendSeat) *world {
	t.Helper()
	w := friendWorld(t, friendBrief("only friend amy"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "dealt and started on her row")
	w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", IDs: []string{"s1-1"}, Reason: "she went down", Who: "rowan"}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Withdrawn, wc.Col)
	require.Equal(t, FriendRow("amy"), wc.F(FieldTakenFrom), "the take-back marks her")
	require.Equal(t, Ready, w.s.StateOf("s1-1"))
	return w
}

// givenAgainNotes is the happened notes whose text says the card was given again to a
// friend because she is up with room.
func givenAgainNotes(w *world) []Note {
	var out []Note
	for _, n := range w.notes {
		if n.Kind == Happened && strings.Contains(n.What, "given again to a friend: she is up with room") {
			out = append(out, n)
		}
	}
	return out
}

func TestAPinnedCardTakenBackIsGivenAgainWhenHerFriendHasRoom(t *testing.T) {
	t.Parallel()

	t.Run("a pinned card withdrawn from a friend up with a free lane is given back that tick", func(t *testing.T) {
		t.Parallel()
		w := pinnedTakenBack(t)
		dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})

		wc := w.s.Fleet.Card("s1-1.w1")
		require.NotEqual(t, Withdrawn, wc.Col, "given back in the tick that sees both facts, not left withdrawn")
		assert.Equal(t, FriendRow("amy"), wc.Row, "back on her row")
		assert.Contains(t, []string{Ready, Working}, wc.Col, "working or ready on her row")
		assert.Equal(t, Working, w.s.StateOf("s1-1"))

		notes := givenAgainNotes(w)
		require.Len(t, notes, 1, "one happened note to the seat")
		assert.Equal(t, Happened, notes[0].Kind)
		assert.Contains(t, notes[0].What, "pinned card s1-1 given again to a friend: she is up with room")
		assert.Empty(t, Check(w.s, nil))
	})

	t.Run("a friend down: not given, no judgment raised", func(t *testing.T) {
		t.Parallel()
		w := pinnedTakenBack(t)
		dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Down, Class: "flash,pro"})

		assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "still withdrawn: she is down")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"))
		assert.Empty(t, givenAgainNotes(w), "not given, so no note")
		assert.Empty(t, w.openOn("s1-1"), "no judgment is raised for the wait")
		hd := Holder(running(w), w.s.Now, "s1-1")
		assert.Equal(t, HeldByWaiting, hd.By)
		assert.Contains(t, hd.Why, "waits for only friend amy")
	})

	t.Run("a friend up but every lane busy: not given, no judgment", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("only friend amy"), friendBrief("only friend amy"), friendBrief("only friend amy"))
		dealStarted(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
		w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", IDs: []string{"s1-1"}, Reason: "lane busy", Who: "rowan"}))
		// amy's two lanes are held by her remaining cards: below her room, but no free lane
		dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})

		assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "still withdrawn: every lane busy")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"))
		assert.Empty(t, givenAgainNotes(w))
		assert.Empty(t, w.openOn("s1-1"), "no judgment is raised for the wait")
	})

	t.Run("a friend held by the coordinator: not given", func(t *testing.T) {
		t.Parallel()
		w := pinnedTakenBack(t)
		row := FriendRow("amy")
		w.s.Fleet.Put(&Card{ID: CtlID(row), Row: row, Col: Ctl, Rev: 1,
			Fields: map[string]string{"kind": "member", "status": Held, "held": stamp(w.s.Now)}})
		dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})

		assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "still withdrawn: held by the coordinator")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"))
		assert.Empty(t, givenAgainNotes(w))
		assert.Empty(t, w.openOn("s1-1"))
	})

	t.Run("the card's tier outside her tiers: not given, the existing wording stays", func(t *testing.T) {
		t.Parallel()
		w := pinnedTakenBack(t)
		dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Tiers: []string{"pro"}})

		assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "still withdrawn: her tiers no longer hold its tier")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"))
		assert.Empty(t, givenAgainNotes(w))
		hd := Holder(running(w), w.s.Now, "s1-1")
		assert.Equal(t, HeldByWaiting, hd.By)
		assert.Contains(t, hd.Why, "waits for only friend amy, and it was taken back")
	})

	t.Run("a pin released past its bound is dealt as today", func(t *testing.T) {
		t.Parallel()
		w := pinnedTakenBack(t)
		// the tick's record that the pin-waits stop stood past its bound (releasePins): the
		// pin is a preference now (PinReleased), dealt to another friend, never given back
		stop := Note{Kind: Judgment, Type: NStopPinWaits, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: w.s.Now, ID: "stop-1"}
		w.s.Open = append(w.s.Open, Open{Key: OpenKey("stop-1", "s1-1"), Note: stop})
		rec := Note{Kind: Acknowledged, Type: NPinReleased, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, What: "stop-1", At: w.s.Now, ID: "rec-1"}
		w.s.Acked = append(w.s.Acked, Open{Key: OpenKey("rec-1", "s1-1"), Note: rec})

		dealWith(w, FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash"})

		wc := w.s.Fleet.Card("s1-1.w1")
		require.NotEqual(t, Withdrawn, wc.Col, "released past its bound: dealt as a preference")
		assert.Equal(t, FriendRow("bob"), wc.Row, "dealt to another friend up, never back to amy")
		assert.Empty(t, givenAgainNotes(w), "a released pin is a preference, not a give-back")
	})
}
