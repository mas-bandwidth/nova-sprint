package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/config"
	"github.com/stretchr/testify/require"
)

// TestAFrontierReadGoesToAFrontierReaderWhenFrontierFriendsAreFull pins the
// ask of 2026-10-05: three readers up, every frontier friend at her room, and
// the tick raised "fewer than two readers up" every tick. A reader row that
// declares the frontier tier is asked the read; when no one who reads frontier
// has room, one judgment names who is full, the same one the next tick; and
// "fewer than two readers up" is raised only when fewer are up.
func TestAFrontierReadGoesToAFrontierReaderWhenFrontierFriendsAreFull(t *testing.T) {
	t.Parallel()
	readers := []string{"reader-rowan-space", "reader-johnny", "reader-rowan-coord"}
	frontierCards := func(w *world, ids ...string) {
		for i, id := range ids {
			putReview(w, id, id+": work (s1) tier: frontier\n", 1, float64(i+1), "head-"+id)
		}
	}
	allUp := func(w *world) {
		w.s.ReaderStates = map[string]string{}
		for _, rd := range w.s.Readers.Rows() {
			w.s.ReaderStates[rd] = ReaderUp
		}
	}
	oneShot := func(name string) FriendSeat {
		seat := frontierSeat(name, 1, Up, "")
		seat.Mode = config.FriendModeOneShot // her room is one read
		return seat
	}

	t.Run("a frontier reader takes it", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, readers...)
		allUp(w)
		w.s.Readers.Texts = map[string]map[string]string{"reader-johnny": {ReaderTiers: "flash,pro,heavy,frontier"}}
		frontierCards(w, "s1-1", "s1-2")
		askReaders(t, w, []FriendSeat{oneShot("amy")})

		require.NotNil(t, w.s.Fleet.Card(ReadCardID("s1-1", 1, "amy")))
		rc := w.s.Readers.Card(ReadCardID("s1-2", 1, "reader-johnny"))
		require.NotNil(t, rc, "amy is full: the reader who reads frontier is asked")
		require.Equal(t, "reader-johnny", rc.Row)
		require.Equal(t, Asked, rc.Col)
		require.Equal(t, cardhdr.RouteFrontier, rc.F(FieldTier), "its runner maps frontier to a frontier model")
		require.Equal(t, "head-s1-2", rc.F("head"))
		for _, rd := range []string{"reader-rowan-space", "reader-rowan-coord"} {
			require.Nil(t, w.s.Readers.Card(ReadCardID("s1-2", 1, rd)), "a reader that does not declare frontier is not asked")
		}
		require.Empty(t, w.notesOf(NFewReaders))
		require.Empty(t, w.notesOf(NNoFrontierRoom))

		// the next tick asks it of no one more, and the level leaves it with the
		// reader who reads frontier
		askReaders(t, w, []FriendSeat{oneShot("amy")})
		w.part(TickLevelReads, TickReq{})
		require.Equal(t, "reader-johnny", w.s.Readers.Card(ReadCardID("s1-2", 1, "reader-johnny")).Row)
		require.True(t, w.s.Readers.Card(ReadCardID("s1-2", 1, "reader-johnny")).Placed())
		for _, rd := range []string{"reader-rowan-space", "reader-rowan-coord"} {
			require.Nil(t, w.s.Readers.Card(ReadCardID("s1-2", 1, rd)))
		}
		require.Nil(t, w.s.Fleet.Card(ReadCardID("s1-2", 1, "amy")))
	})

	t.Run("nobody has room", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, readers...)
		allUp(w)
		w.s.Readers.Texts = map[string]map[string]string{"reader-johnny": {ReaderTiers: "frontier"}}
		w.s.Fleet.SetRows([]string{"johnny"})
		w.s.Fleet.Put(&Card{ID: CtlID("johnny"), Row: "johnny", Col: Ctl, Rev: 1, Fields: map[string]string{"status": Up, FieldWidth: "1"}})
		frontierCards(w, "s1-1", "s1-2", "s1-3")
		askReaders(t, w, []FriendSeat{oneShot("amy")})

		require.NotNil(t, w.s.Fleet.Card(ReadCardID("s1-1", 1, "amy")))
		require.NotNil(t, w.s.Readers.Card(ReadCardID("s1-2", 1, "reader-johnny")))
		require.Nil(t, w.s.Fleet.Card(ReadCardID("s1-3", 1, "amy")))
		require.Nil(t, w.s.Readers.Card(ReadCardID("s1-3", 1, "reader-johnny")))
		ns := w.notesOf(NNoFrontierRoom)
		require.Len(t, ns, 1)
		require.Equal(t, "no frontier reader has room: amy full, reader-johnny full", ns[0].What)
		require.Equal(t, Judgment, ns[0].Kind)
		require.Equal(t, []string{"s1-3"}, ns[0].Primaries)
		require.Empty(t, w.notesOf(NFewReaders), "three readers are up")
		require.Empty(t, w.notesOf(NWaitingForReader), "the judgment is the card's note")

		// the next tick, and the one after: the same judgment stands, not a new one
		for range 2 {
			w.tick(0)
			askReaders(t, w, []FriendSeat{oneShot("amy")})
		}
		require.Len(t, w.notesOf(NNoFrontierRoom), 1)
		require.Empty(t, w.notesOf(NFewReaders))
		var open []Open
		for _, o := range w.s.Open {
			if o.Note.Type == NNoFrontierRoom {
				open = append(open, o)
			}
		}
		require.Len(t, open, 1)

		// the no-stall rule finds the waiting card held by that judgment
		hd := Holder(HeldState{Snap: w.s, Running: true}, w.s.Now, "s1-3")
		require.Equal(t, HeldByJudgment, hd.By, "s1-3: %s", hd.Why)
		require.Contains(t, hd.Why, NNoFrontierRoom)

		// room frees: the read is asked and the judgment closes
		w.s.Fleet.Put(&Card{ID: CtlID("johnny"), Row: "johnny", Col: Ctl, Rev: 2, Fields: map[string]string{"status": Up, FieldWidth: "2"}})
		askReaders(t, w, []FriendSeat{oneShot("amy")})
		require.NotNil(t, w.s.Readers.Card(ReadCardID("s1-3", 1, "reader-johnny")))
		for _, o := range w.s.Open {
			require.NotEqual(t, NNoFrontierRoom, o.Note.Type)
		}
	})

	t.Run("three readers up and no one reads frontier", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, readers...)
		allUp(w)
		frontierCards(w, "s1-1")
		putReview(w, "s1-2", "s1-2: other (s1) tier: pro\n", 1, 2, "pro-head")
		askReaders(t, w, []FriendSeat{frontierSeat("amy", 1, Down, "")})

		require.Empty(t, w.notesOf(NFewReaders), "three readers are up")
		ns := w.notesOf(NNoFrontierRoom)
		require.Len(t, ns, 1)
		require.Equal(t, "no frontier reader has room: no friend or reader up reads frontier", ns[0].What)
		require.Len(t, readsAt(w.s, w.s.Work.Card("s1-2"), 1), 1, "the pro card is asked")
	})

	t.Run("one reader up", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, readers...)
		w.s.ReaderStates = map[string]string{"reader-rowan-space": ReaderUp, "reader-johnny": ReaderDown, "reader-rowan-coord": ReaderAway}
		putReview(w, "s1-2", "s1-2: other (s1) tier: pro\n", 1, 2, "pro-head")
		askReaders(t, w, nil)
		require.Len(t, w.notesOf(NFewReaders), 1, "fewer than two readers are up: the judgment is true")
	})
}
