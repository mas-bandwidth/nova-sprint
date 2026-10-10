package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// openFrontierRoom is the NNoFrontierRoom judgments open in the world.
func openFrontierRoom(w *world) []Open {
	var out []Open
	for _, o := range w.s.Open {
		if o.Note.Type == NNoFrontierRoom {
			out = append(out, o)
		}
	}
	return out
}

// TestAFrontierReadNoOneMayTakeIsJudgedOnceByItsRealCause pins frontier-read-judged-by-real-cause:
// a frontier read the ask leaves waiting raises ONE judgment (NNoFrontierRoom) naming each such
// read and who is full, written once, rewritten in place (Plan.Updates) when its facts change and
// never raised anew, closed when the read is asked; the no-stall rule's hold carries its cause.
// This is the friend ask's path (read cards off).
func TestAFrontierReadNoOneMayTakeIsJudgedOnceByItsRealCause(t *testing.T) {
	t.Parallel()
	const primHead = "primary-head"
	body := func(id string) string { return id + ": work (s1) tier: frontier\n" }

	t.Run("a friend at her room is named, once, and rewritten in place", func(t *testing.T) {
		t.Parallel()
		// no paid reader: amy (width 1, room 2) takes s1-1 and s1-2, and s1-3 has no one
		w := newWorld(t)
		seats := []FriendSeat{frontierSeat("amy", 1, Up, t.TempDir())}
		for i, id := range []string{"s1-1", "s1-2", "s1-3"} {
			putReview(w, id, body(id), 1, float64(i+1), primHead)
		}
		askReaders(t, w, seats)
		require.Nil(t, w.s.Fleet.Card(ReadCardID("s1-3", 1, "amy")), "amy is at her room")
		ns := w.notesOf(NNoFrontierRoom)
		require.Len(t, ns, 1, "one judgment")
		require.Equal(t, Judgment, ns[0].Kind)
		require.Equal(t, []string{"s1-3"}, ns[0].Primaries)
		require.Contains(t, ns[0].What, "s1-3 waits: amy full", "the real cause: who is full")
		require.NotContains(t, ns[0].What, "s1-1", "an asked read is no subject")

		// the same facts the next ticks: nothing is written
		for range 2 {
			w.tick(time.Minute)
			askReaders(t, w, seats)
		}
		require.Len(t, w.notesOf(NNoFrontierRoom), 1, "never raised anew")
		require.Empty(t, w.updates, "the same facts rewrite nothing")

		// another frontier read waits: the same judgment is rewritten in place
		putReview(w, "s1-4", body("s1-4"), 1, 4, primHead)
		w.tick(time.Minute)
		askReaders(t, w, seats)
		require.Len(t, w.notesOf(NNoFrontierRoom), 1, "still the one judgment, never raised anew")
		require.Len(t, w.updates, 1, "its facts changed: rewritten in place")
		open := openFrontierRoom(w)
		require.Len(t, open, 1)
		require.Equal(t, ns[0].ID, open[0].Note.ID, "the same judgment")
		require.Contains(t, open[0].Note.What, "s1-3 waits: amy full")
		require.Contains(t, open[0].Note.What, "s1-4 waits: amy full")
		require.Equal(t, []string{"reader add", "wait"}, open[0].Note.Decisions)

		// she has room for both: they are asked, and the judgment closes
		w.tick(time.Minute)
		askReaders(t, w, []FriendSeat{frontierSeat("amy", 4, Up, t.TempDir())})
		require.NotNil(t, w.s.Fleet.Card(ReadCardID("s1-3", 1, "amy")))
		require.NotNil(t, w.s.Fleet.Card(ReadCardID("s1-4", 1, "amy")))
		require.Empty(t, openFrontierRoom(w), "asked: the judgment closes")
	})

	t.Run("a paid reader with room takes it and no judgment is raised", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a", "reader-b")
		for i, id := range []string{"s1-1", "s1-2", "s1-3"} {
			putReview(w, id, body(id), 1, float64(i+1), primHead)
		}
		askReaders(t, w, []FriendSeat{frontierSeat("amy", 1, Up, t.TempDir())})
		require.NotNil(t, placedReaderRead(w, "s1-3"))
		require.Empty(t, w.notesOf(NNoFrontierRoom))
	})

	t.Run("a read below frontier that waits is no frontier judgment", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		seats := []FriendSeat{frontierSeat("amy", 1, Up, t.TempDir())}
		for i, id := range []string{"s1-1", "s1-2", "s1-3"} {
			putReview(w, id, id+": work (s1) tier: pro\n", 1, float64(i+1), primHead)
		}
		askReaders(t, w, seats)
		require.Empty(t, w.notesOf(NNoFrontierRoom))
	})

	t.Run("the hold of a waiting read says the judgment's cause", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		seats := []FriendSeat{frontierSeat("amy", 1, Up, t.TempDir())}
		for i, id := range []string{"s1-1", "s1-2", "s1-3"} {
			putReview(w, id, body(id), 1, float64(i+1), primHead)
		}
		askReaders(t, w, seats)
		require.Len(t, openFrontierRoom(w), 1)
		hd := Holder(running(w), w.s.Now, "s1-3")
		require.False(t, hd.Stalled(), "judged, not stalled: %s", hd)
		require.Contains(t, hd.Why, NNoFrontierRoom, "the hold names the judgment: %s", hd)
		require.Contains(t, hd.Why, "s1-3 waits: amy full", "and its cause: %s", hd)
	})

	t.Run("no friend or reader up reads frontier is said when that is the cause", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		putReview(w, "s1-1", body("s1-1"), 1, 1, primHead)
		require.Equal(t, "no friend or reader up reads frontier", frontierWhy(w.s, nil, w.s.Work.Card("s1-1")))
	})
}

// TestAFrontierReadWaitingForRoomIsJudgedWithReadCards is the same rule on the read-card ask's
// path (read cards on): a frontier read that waits because every unit that may read it is at its
// room raises the one judgment, naming the units, and it closes when the read is dealt.
func TestAFrontierReadWaitingForRoomIsJudgedWithReadCards(t *testing.T) {
	t.Parallel()
	frontierReader := func(name string, width int) FriendSeat {
		return readerSeat(name, width, []string{"frontier"}, []string{"builder", "reader"})
	}
	ask := func(w *world, seats []FriendSeat) {
		w.s.Now = w.s.Now.Add(time.Minute)
		w.part(func(s *Snapshot, r TickReq) (Plan, int) { return readCardsAskPart(s, r, seats) }, TickReq{Friends: seats})
	}
	newWorldWaiting := func(t *testing.T) *world {
		w := readCardsWorld(t, 4)
		putReviewBy(w, "s1-1", "s1-1: work (s1) tier: frontier\n", "bob", 1)
		return w
	}
	bob := frontierReader("bob", 8)

	t.Run("every unit that may read it is at its room", func(t *testing.T) {
		t.Parallel()
		w := newWorldWaiting(t)
		full := []FriendSeat{bob, frontierReader("amy", 0)}
		ask(w, full)
		ns := w.notesOf(NNoFrontierRoom)
		require.Len(t, ns, 1, "notes: %+v", w.notes)
		require.Equal(t, []string{"s1-1"}, ns[0].Primaries)
		require.Contains(t, ns[0].What, "s1-1 waits: amy full")

		ask(w, full)
		ask(w, full)
		require.Len(t, w.notesOf(NNoFrontierRoom), 1, "never raised anew")
		require.Empty(t, w.updates, "the same facts rewrite nothing")

		// cal comes up with room: both reads are dealt and the judgment closes
		ask(w, []FriendSeat{bob, frontierReader("amy", 8), frontierReader("cal", 8)})
		require.Empty(t, openFrontierRoom(w), "dealt: the judgment closes")
	})

	t.Run("a read no unit may read is cannot ask's, not this judgment", func(t *testing.T) {
		t.Parallel()
		w := newWorldWaiting(t)
		ask(w, []FriendSeat{bob})
		require.Empty(t, w.notesOf(NNoFrontierRoom))
		require.Len(t, w.notesOf(NCannotAsk), 1)
	})
}
