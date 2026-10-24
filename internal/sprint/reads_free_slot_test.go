package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Reads never wait behind a full row (roadmap reads-never-wait-behind-a-full-row; the
// coordinator, 2026-10-24: four reads were dealt to a friend whose row was at 8 of 8 and sat in
// her ready column for an hour while 18 cards waited in review, and friend give refused their
// ids). A read is dealt to a friend only while her row has a free slot; a ready read left
// untaken on a friend's row past the start bound is levelled to another reader with a free
// lane; friend give moves a read card to the friend named, never to its own author.
// tla/FriendReadSlot.tla. Unit tests: the clock is the snapshot's (t0 and w.tick), no socket.

// slotWorld is read cards on with no fleet reader, and three friends who read flash, each of
// width 1: alex, pat, and zoe, who worked s1-1, a flash card in review that needs one read.
func slotWorld(t *testing.T) (*world, []FriendSeat) {
	t.Helper()
	w := readCardsWorld(t, 4)
	seats := workerSeatFixtures([]FriendSeat{
		readerSeat("alex", 1, []string{"flash"}, []string{"reader"}),
		readerSeat("pat", 1, []string{"flash"}, []string{"reader"}),
		readerSeat("zoe", 1, []string{"flash"}, []string{"builder", "reader"}),
	})
	rememberWorkerSeats(w.s, seats)
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: flash\n", "zoe", 1)
	return w, seats
}

// busy puts a work card working on the friend's row: her one lane is held.
func busy(w *world, friend string) {
	row := FriendRow(friend)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	w.s.Fleet.Put(&Card{ID: "busy-" + friend + ".w1", Row: row, Col: Working, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "work", "gen": "1", "primary": "busy-" + friend, "stream": "s1"}})
}

// readyRead puts the friend's read card of s1-1 at attempt 1 ready on her row, asked at.
func readyRead(w *world, friend string, asked time.Time) *Card {
	row := FriendRow(friend)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	c := &Card{ID: ReadCardID("s1-1", 1, friend), Row: row, Col: Ready, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": "s1-1", "stream": "s1", "reader": friend, "attempt": "1",
			"head": "head-s1-1", "asked": stamp(asked), "gen": "1", FieldReadCard: "1", FieldTier: "flash"}}
	w.s.Fleet.Put(c)
	return c
}

// placedReadRows is the rows of s1-1's placed read cards.
func placedReadRows(w *world) []string { return eReadRows(w, "s1-1") }

func TestAReadIsDealtToAFriendOnlyWhileHerRowHasAFreeSlot(t *testing.T) {
	t.Parallel()
	t.Run("a friend at her width is dealt no read", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		busy(w, "alex")
		busy(w, "pat")
		dealReads(t, w, seats)
		require.Empty(t, placedReadRows(w), "every reader is at her width: the read waits in review, on no row")
	})
	t.Run("a friend with a free slot is dealt it working", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		busy(w, "alex")
		dealReads(t, w, seats)
		require.Equal(t, []string{FriendRow("pat")}, placedReadRows(w), "the friend with the free slot, never alex at her width, never zoe who worked it")
		require.Equal(t, Working, w.s.Fleet.Card(ReadCardID("s1-1", 1, "pat")).Col)
	})
	t.Run("no read is ever left ready on a friend's row", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		for i := 2; i <= 5; i++ {
			id := "s1-" + itoa(i)
			putReviewBy(w, id, id+": work (s1) tier: flash\n", "zoe", float64(i))
		}
		dealReads(t, w, seats)
		for _, f := range []string{"alex", "pat", "zoe"} {
			for _, c := range w.s.Fleet.Cell(FriendRow(f), Ready) {
				require.False(t, isRead(c), "read %s left ready on %s's row", c.ID, f)
			}
			require.LessOrEqual(t, friendLiveLanes(w.s, f), 1, "%s past her width", f)
		}
	})
}

func TestAReadyReadUntakenPastTheStartBoundIsLevelled(t *testing.T) {
	t.Parallel()
	t.Run("levelled to a reader with a free lane", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		busy(w, "alex")
		old := readyRead(w, "alex", t0)
		w.tick(FriendStartMaxDefault + time.Minute)
		p := dealReads(t, w, seats)
		gone := w.s.Fleet.Card(old.ID)
		require.False(t, gone.Placed(), "alex's untaken read is taken back")
		require.Equal(t, RetiredByLevel, gone.F("retired_by"), "levelled, spending no one")
		require.Equal(t, []string{FriendRow("pat")}, placedReadRows(w), "dealt to pat, who has a free lane")
		said := false
		for _, u := range p.Units {
			said = said || strings.Contains(u.Moved, "levelled from alex")
		}
		require.True(t, said, "the move says where the read was levelled from")
	})
	t.Run("not before the bound", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		busy(w, "alex")
		readyRead(w, "alex", t0)
		w.tick(FriendStartMaxDefault - time.Minute)
		dealReads(t, w, seats)
		require.Equal(t, []string{FriendRow("alex")}, placedReadRows(w), "inside the bound it stays hers")
	})
	t.Run("not while no other reader has a free lane", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		busy(w, "alex")
		busy(w, "pat")
		readyRead(w, "alex", t0)
		w.tick(FriendStartMaxDefault + time.Minute)
		dealReads(t, w, seats)
		require.Equal(t, []string{FriendRow("alex")}, placedReadRows(w), "with nowhere to go it stays; the deal bound still takes it back")
	})
	t.Run("not one she has begun", func(t *testing.T) {
		t.Parallel()
		w, seats := slotWorld(t)
		busy(w, "alex")
		c := readyRead(w, "alex", t0)
		seats[0].Running = []string{c.ID}
		w.tick(FriendStartMaxDefault + time.Minute)
		dealReads(t, w, seats)
		require.Contains(t, placedReadRows(w), FriendRow("alex"), "her beat names it running: it stays hers")
	})
}

func TestFriendGiveMovesAReadCard(t *testing.T) {
	t.Parallel()
	t.Run("a read moves to the friend named", func(t *testing.T) {
		t.Parallel()
		w, _ := slotWorld(t)
		busy(w, "alex")
		old := readyRead(w, "alex", t0)
		p := w.must(FriendGive(w.s, FriendGiveReq{Friend: "pat", IDs: []string{old.ID}}))
		require.Contains(t, p.Said, old.ID+": handed to pat")
		gone := w.s.Fleet.Card(old.ID)
		require.False(t, gone.Placed())
		require.Equal(t, RetiredByCoordinator, gone.F("retired_by"))
		moved := w.s.Fleet.Card(ReadCardID("s1-1", 1, "pat"))
		require.NotNil(t, moved)
		require.Equal(t, FriendRow("pat"), moved.Row)
		require.Equal(t, Working, moved.Col)
		require.Equal(t, "pat", moved.F("reader"))
		require.Equal(t, "head-s1-1", moved.F("head"))
	})
	refused := func(t *testing.T, w *world, friend, id, words string) {
		t.Helper()
		p := FriendGive(w.s, FriendGiveReq{Friend: friend, IDs: []string{id}})
		require.Len(t, p.Refused, 1)
		require.Contains(t, p.Refused[0].Why, words)
		require.Empty(t, p.Units)
	}
	t.Run("never to its own author", func(t *testing.T) {
		t.Parallel()
		w, _ := slotWorld(t)
		old := readyRead(w, "alex", t0)
		refused(t, w, "zoe", old.ID, "own work")
	})
	t.Run("never to a row at its width", func(t *testing.T) {
		t.Parallel()
		w, _ := slotWorld(t)
		busy(w, "pat")
		old := readyRead(w, "alex", t0)
		refused(t, w, "pat", old.ID, "at its width")
	})
	t.Run("never one begun", func(t *testing.T) {
		t.Parallel()
		w, _ := slotWorld(t)
		old := readyRead(w, "alex", t0)
		old.Fields["taken"] = stamp(t0)
		refused(t, w, "pat", old.ID, "has begun")
	})
	t.Run("never to the friend who holds it", func(t *testing.T) {
		t.Parallel()
		w, _ := slotWorld(t)
		old := readyRead(w, "alex", t0)
		refused(t, w, "alex", old.ID, "already")
	})
}
