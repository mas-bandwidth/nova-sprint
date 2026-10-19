package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The friend deal queues every ready card into the ready queue of an eligible friend that
// is up, even when her row is at its width: width bounds what a friend WORKS at once, never
// what is QUEUED in her ready queue (the owner, 2026-10-18: "My dude, ready cards, go to
// friend ready queues, even if friends are full. That's what the ready queues are FOR
// per-friend and per-fleet machine."). Her queue is bounded (DealAhead times width, ready
// and working together, so a row at width queues up to its width again) and the deal spreads
// by queue depth (preferredFriend: the most room free, the shortest queue).
// v17-friend-deal-queues-past-width. At BASE (v1.2.24) a friend holding a read had her free
// room read as her free LANES (friendFreeLanes), so at width her room was zero and nothing
// was queued to her.

// heavyBrief is a card brief whose first model line names the heavy tier.
const heavyBrief = "tier: heavy\nREPO: mas-bandwidth/nova-tools\nWHO: friend\n\nThe task."

// seedPrimary adds a primary ready on the work table, as the deal offers it.
func seedPrimary(w *world, id, brief string) {
	w.must(Add(w.s, AddReq{Stream: "h", Cards: []CardAdd{{ID: id, Brief: brief}}}))
}

// seedReadyWork puts n work cards ready on the friend's row, as the deal would leave them
// queued behind her working lanes.
func seedReadyWork(w *world, friend string, n int) {
	row := FriendRow(friend)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	for i := 0; i < n; i++ {
		id := "seedr-" + friend + "-" + itoa(i+1)
		w.s.Fleet.Put(&Card{ID: id, Row: row, Col: Ready, Score: 1, Rev: 1,
			Fields: map[string]string{"kind": "work", "gen": "1", "primary": id, "stream": "s1"}})
	}
}

// heavySeat is an up friend of width who serves the heavy tier.
func heavySeat(name string, width int) FriendSeat {
	return workerSeatFixture(FriendSeat{Name: name, Width: width, Status: Up, Tiers: []string{"heavy"}})
}

// A friend at her width (2 work and 2 reads working of width 4) with one heavy ready card
// and no machine route serving heavy: one deal places the card on her row's ready column,
// and her working stays at width. At BASE the reads branch reduced her free room to her free
// lanes, zero at width, so the card sat undealt in the pool.
func TestTheFriendDealQueuesPastWidth(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	seedWork(w, "amy", 2)
	seedReads(w, "amy", 2)
	require.Equal(t, 4, friendLiveLanes(w.s, "amy"), "2 work + 2 reads = 4 live of width 4")

	seedPrimary(w, "heavy-1", heavyBrief)
	dealWith(w, heavySeat("amy", 4))

	row := FriendRow("amy")
	assert.Equal(t, 4, w.s.Fleet.Count(row, Working), "her working stays at width")
	assert.Equal(t, 1, w.s.Fleet.Count(row, Ready), "the heavy card is queued on her row")
	assert.NotNil(t, w.s.Fleet.Card("heavy-1.w1"), "its work card is created on her row")
	assert.Equal(t, Working, w.s.StateOf("heavy-1"), "its primary is working on the new attempt")
}

// The queue is bounded: the same friend at width (2 work and 2 reads working) offered 8
// ready cards has exactly 4 placed (ready+working = DealAhead*width = 8); the rest stay in
// the pool.
func TestTheFriendQueueIsBounded(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	seedWork(w, "amy", 2)
	seedReads(w, "amy", 2)
	for i := 1; i <= 8; i++ {
		seedPrimary(w, "heavy-"+itoa(i), heavyBrief)
	}
	dealWith(w, heavySeat("amy", 4))

	row := FriendRow("amy")
	assert.Equal(t, 4, w.s.Fleet.Count(row, Working), "her working stays at width")
	assert.Equal(t, 4, w.s.Fleet.Count(row, Ready), "ready+working = DealAhead*width = 8")
	assert.Equal(t, 4, len(w.s.Work.Column(Ready)), "the other four stay in the pool")
}

// The deal spreads by queue depth: two friends at width 4, one holding 3 queued and one
// holding 1; a card offered goes to the one with fewer queued (the more room), then the
// queues level as more are offered.
func TestTheFriendDealSpreadsByQueueDepth(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	seedWork(w, "amy", 4)
	seedReadyWork(w, "amy", 3)
	seedWork(w, "bob", 4)
	seedReadyWork(w, "bob", 1)

	// the first card goes to bob: he holds 1 queued, amy 3, and both are at width
	seedPrimary(w, "heavy-1", heavyBrief)
	dealWith(w, heavySeat("amy", 4), heavySeat("bob", 4))
	bob := FriendRow("bob")
	assert.Equal(t, 2, w.s.Fleet.Count(bob, Ready), "the first card goes to the friend with the shorter queue")
	require.NotNil(t, w.s.Fleet.Card("heavy-1.w1"), "the first card is dealt")
	assert.Equal(t, bob, w.s.Fleet.Card("heavy-1.w1").Row)

	// three more cards level the queues to 4 each
	for i := 2; i <= 4; i++ {
		seedPrimary(w, "heavy-"+itoa(i), heavyBrief)
	}
	dealWith(w, heavySeat("amy", 4), heavySeat("bob", 4))
	amy := FriendRow("amy")
	assert.Equal(t, 4, w.s.Fleet.Count(amy, Ready), "amy's queue leveled to 4")
	assert.Equal(t, 4, w.s.Fleet.Count(bob, Ready), "bob's queue leveled to 4")
	assert.Equal(t, 4, w.s.Fleet.Count(amy, Working), "amy's working stays at width")
	assert.Equal(t, 4, w.s.Fleet.Count(bob, Working), "bob's working stays at width")
}
