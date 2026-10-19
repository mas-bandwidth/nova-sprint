package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The friend deal counts reads against her width (the-friend-deal-counts-reads-against-
// her-width.w1, v1.2.24). The take gate counts every live lane, work and read, and refuses
// a work take at "12 of 8"; the deal stops at the same number (friendLiveLanes,
// friendFreeLanes). No test here uses real time or a socket: the tick's now is t0.

// seedWork puts n work cards working on the friend's row, by primaries that are not on the
// work table (they are never offered to the deal), as a store that already holds them
// would.
func seedWork(w *world, friend string, n int) {
	row := FriendRow(friend)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	for i := 0; i < n; i++ {
		id := "seedw-" + friend + "-" + itoa(i+1)
		w.s.Fleet.Put(&Card{ID: id, Row: row, Col: Working, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "work", "gen": "1", "primary": id, "stream": "s1", FieldStarted: "1"}})
	}
}

// seedReads places n read cards working on the friend's row, each of a primary in review at
// its attempt, as the ask places hers: the index keeps them (a read of a primary in review
// is not taken back).
func seedReads(w *world, friend string, n int) {
	row := FriendRow(friend)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	if !w.s.Work.HasRow("reads") {
		w.s.Work.SetRows(append(w.s.Work.Rows(), "reads"))
	}
	for i := 0; i < n; i++ {
		id := "read-" + itoa(i)
		w.s.Work.Put(&Card{ID: id, Row: "reads", Col: Review, Score: float64(i + 1), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "reads", "brief": friendBrief("friend"), "head": "h"}})
		w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, friend), Row: row, Col: Working, Score: float64(i + 1), Rev: 1,
			Fields: map[string]string{"kind": "read", "primary": id, "stream": "reads", "reader": friend, "attempt": "1", FieldReadCard: "1"}})
	}
}

// dealsN adds n hard-pinned primaries for the friend and runs one deal, answering how many
// of them landed on her row and her live lanes after it.
func dealsN(w *world, friend string, n int) (dealt, live int) {
	w.t.Helper()
	var cards []CardAdd
	base := len(w.s.Work.Cards())
	for i := 0; i < n; i++ {
		cards = append(cards, CardAdd{ID: "off" + itoa(base) + "-" + itoa(i+1), Brief: friendBrief("only friend " + friend)})
	}
	if n > 0 {
		w.must(Add(w.s, AddReq{Stream: "off", Cards: cards}))
	}
	row := FriendRow(friend)
	before := w.s.Fleet.Count(row, Ready) + w.s.Fleet.Count(row, Working)
	dealWith(w, workerSeatFixture(FriendSeat{Name: friend, Width: 8, Status: Up, Class: "flash,pro"}))
	after := w.s.Fleet.Count(row, Ready) + w.s.Fleet.Count(row, Working)
	return after - before, after
}

// (4) the property: the number the take gate refuses at (live lanes at the width) and the
// number the deal stops at (free lanes zero) are the same, for widths 1 to 8 and 0 to 12
// reads. The pure count, no store.
func TestTheDealStopsAtTheNumberTheGateRefusesAt(t *testing.T) {
	t.Parallel()
	for width := 1; width <= 8; width++ {
		for reads := 0; reads <= 12; reads++ {
			for work := 0; work <= 12; work++ {
				free := friendFreeLanes(width, work, reads)
				// the gate refuses a work take when her live lanes are at her width;
				// the deal stops at exactly the same count
				assert.Equal(t, work+reads >= width, free == 0,
					"width=%d work=%d reads=%d: free=%d", width, work, reads, free)
				assert.Equal(t, max(width-(work+reads), 0), free)
			}
		}
	}
}

// The live count is one place: friendLiveLanes is her work cards plus her reads, and her
// free lanes are her width less it, never below zero. The read ask fills her free lanes
// first, but a friend at width still keeps half a slot for her ready queue past her width
// (the owner, 2026-10-18: width bounds what she works at once, never what is queued in her
// ready queue; her queue is bounded by her room, DealAhead times her width).
func TestTheFriendLiveLanesCountsReads(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	seedWork(w, "amy", 4)
	seedReads(w, "amy", 8)
	assert.Equal(t, 12, friendLiveLanes(w.s, "amy"), "4 work + 8 reads = 12 live lanes")
	assert.Zero(t, friendFreeLanes(8, 4, 8), "12 of 8: no free lane")

	seat := workerSeatFixture(FriendSeat{Name: "amy", Width: 8, Status: Up, Class: "flash,pro", Roles: []string{RoleReader}})
	units := readUnitsOf(w.s, workerSeatFixtures([]FriendSeat{seat}))
	require.Len(t, units, 1)
	assert.LessOrEqual(t, units[0].idle, 0, "no free lane at width")
	assert.Positive(t, units[0].half, "room remains in her ready queue at width, below her room: 12 of 16")
}

// (1) a friend at width 8 with 4 work cards and 8 reads is dealt no ninth read and no work
// card; (2) the same friend with 4 work and 3 reads (7 of 8) is dealt exactly one more;
// (3) a friend with 8 lanes live where reads finish is dealt into the freed lanes, no
// earlier.
func TestTheFriendDealCountsReadsAgainstHerWidth(t *testing.T) {
	t.Parallel()
	const width = 8

	t.Run("at width she is dealt into her queue", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		seedWork(w, "amy", 4)
		seedReads(w, "amy", 8)
		require.Equal(t, 12, friendLiveLanes(w.s, "amy"), "4 work + 8 reads live")

		dealt, live2 := dealsN(w, "amy", 3)
		assert.Equal(t, 3, dealt, "a friend at live %d of width %d is dealt into her ready queue (the owner's rule: width bounds work, never queued)", live2, width)
		assert.Equal(t, 12, w.s.Fleet.Count(FriendRow("amy"), Working), "her working stays at width (4 work, 8 reads)")
	})

	t.Run("below width she is dealt into her queue", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		seedWork(w, "amy", 4)
		seedReads(w, "amy", 3)
		require.Equal(t, 7, friendLiveLanes(w.s, "amy"), "4 work + 3 reads (7 of 8)")

		dealt, live2 := dealsN(w, "amy", 5)
		assert.Equal(t, 5, dealt, "7 of 8: every card offered is dealt into her queue (width bounds work, never queued)")
		assert.Equal(t, 7, w.s.Fleet.Count(FriendRow("amy"), Working), "her working stays at 7 (4 work, 3 reads)")
		assert.Equal(t, width+4, live2, "ready+working = 12")
	})

	t.Run("reads hold lanes and their finish frees room", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		seedWork(w, "amy", 0)
		seedReads(w, "amy", 8)
		dealt, _ := dealsN(w, "amy", 2)
		assert.Equal(t, 2, dealt, "8 reads of width 8: she still deals into her queue (width bounds work, never queued)")

		// three reads finish this tick: they leave her row, freeing room
		for i := 0; i < 3; i++ {
			w.place(w.s.Fleet, ReadCardID("read-"+itoa(i), 1, "amy"), "", DoneOK)
		}
		require.Equal(t, 7, friendLiveLanes(w.s, "amy"), "three reads finished: 2 queued work + 5 reads")
		dealt2, _ := dealsN(w, "amy", 4)
		assert.Equal(t, 4, dealt2, "the freed room takes more queued cards")
	})
}
