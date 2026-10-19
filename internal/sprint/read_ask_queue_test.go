package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The friend ready queue holds reads past her width: her queue is bounded by her room
// (DealAhead times her width), never by her width, so a friend at width still takes reads
// into her ready column while her room has space (the owner, 2026-10-18: "ready cards, go
// to friend ready queues, even if friends are full. That's what the ready queues are FOR
// per-friend and per-fleet machine."). No test here uses real time or a socket: the tick's
// now is t0.

// seedStandingRead places a flash primary in review with its read card working on the
// friend's row, as the ask places hers: the read stands (the primary in review at its
// attempt is not taken back) and wants no more reads.
func seedStandingRead(w *world, friend, id string) {
	row := FriendRow(friend)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	if !w.s.Work.HasRow("reads") {
		w.s.Work.SetRows(append(w.s.Work.Rows(), "reads"))
	}
	w.s.Work.Put(&Card{ID: id, Row: "reads", Col: Review, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "reads", "brief": fleetBrief("flash"), FieldTierNow: "flash", "head": "h-" + id}})
	w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, friend), Row: row, Col: Working, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": id, "stream": "reads", "reader": friend, "attempt": "1", FieldReadCard: "1"}})
}

// seedPoolPrimaries puts n flash primaries in review wanting one read each, their work
// finished on a member row, the reads waiting in the pool.
func seedPoolPrimaries(w *world, n int) {
	if !w.s.Work.HasRow("s1") {
		w.s.Work.SetRows(append(w.s.Work.Rows(), "s1"))
	}
	if !w.s.Fleet.HasRow("worker") {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), "worker"))
	}
	for i := 0; i < n; i++ {
		id := "pool-" + itoa(i+1)
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: float64(i + 1), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief("flash"), FieldTierNow: "flash", "head": "h-" + id}})
		w.s.Fleet.Put(&Card{ID: WorkCardID(id, 1), Row: "worker", Col: Finished, Score: float64(i + 1), Rev: 1,
			Fields: map[string]string{"kind": "work", "primary": id, "attempt": "1", "member": "worker"}})
	}
}

// readyQueueFriend is a reader friend up at width 4 with 4 cards working (2 work, 2 reads):
// at width, no lane free.
func readyQueueFriend(w *world) FriendSeat {
	seat := workerSeatFixture(FriendSeat{Name: "amy", Width: 4, Status: Up, Class: "flash,pro", Roles: []string{RoleReader}})
	seedWork(w, "amy", 2)
	seedStandingRead(w, "amy", "stand-1")
	seedStandingRead(w, "amy", "stand-2")
	return seat
}

// TestTheReadAskQueuesPastWidth pins the owner's rule: a friend reader at width with room in
// her ready queue is asked the reads waiting in the pool, placed on her ready column, and her
// working stays put. At BASE neither is placed: the friend at width had half=0.
func TestTheReadAskQueuesPastWidth(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	seat := readyQueueFriend(w)
	seedPoolPrimaries(w, 2)
	row := FriendRow("amy")
	require.Equal(t, 4, w.s.Fleet.Count(row, Working), "4 working: 2 work + 2 reads")

	p, waits := readCardsAsk(w.s, workerSeatFixtures([]FriendSeat{seat}), nil)
	w.must(p)

	require.Empty(t, waits, "both waiting reads are dealt")
	assert.Equal(t, 4, w.s.Fleet.Count(row, Working), "her working stays 4")
	assert.Equal(t, 2, w.s.Fleet.Count(row, Ready), "both reads queue on her ready column")
}

// TestTheReadQueueIsBounded pins the friend ready queue's bound: offered 8 waiting reads,
// a friend at width 4 (room DealAhead*4 = 8) with 4 working is given exactly 4, so her
// ready+working equals her room and never passes it.
func TestTheReadQueueIsBounded(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	seat := readyQueueFriend(w)
	seedPoolPrimaries(w, 8)
	row := FriendRow("amy")

	p, _ := readCardsAsk(w.s, workerSeatFixtures([]FriendSeat{seat}), nil)
	w.must(p)

	assert.Equal(t, 4, w.s.Fleet.Count(row, Working), "her working stays 4")
	assert.Equal(t, 4, w.s.Fleet.Count(row, Ready), "given exactly 4 reads")
	assert.Equal(t, DealAhead*4, w.s.Fleet.Count(row, Ready)+w.s.Fleet.Count(row, Working), "ready+working = DealAhead*width")
}
