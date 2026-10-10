package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bounce-back (bounce.go, tla/DealFill.tla). The owner, 2026-10-10: "Cards need to
// bounce back. We can't just have cards disappearing." "If they fail to do the thing they
// are supposed to do, you MUST be notified via push."

// bounceTick is the tick's bounce and its deal, as TickTables runs them.
func bounceTick(w *world, seats ...FriendSeat) {
	w.t.Helper()
	w.part(TickBounce, TickReq{Friends: seats})
	w.part(TickDeal, TickReq{Friends: seats})
}

// bouncedNotes is every bounce note the world holds.
func bouncedNotes(w *world) []Note {
	var out []Note
	for _, n := range w.notes {
		if n.Type == NBounced {
			out = append(out, n)
		}
	}
	return out
}

// A friend who never takes (2026-10-10: 47 cards dealt to two friends whose daemons could
// not take them, the fleet idle): her cards return to the pool past the take bound and the
// deal of the same tick gives them to the live machines, which take them. Each return is a
// note to the coordinator (pushed: inbox --push wakes the seat for a note with a To) naming
// the card, her row, how long and what was done; the card is taken from her and never dealt
// to her again.
func TestCardsAFriendNeverTakesBounceToTheLiveFleet(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"), friendBrief("friend"))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	bounceTick(w, amy)
	row := FriendRow("amy")
	require.Equal(t, 2, w.s.Fleet.Count(row, Ready), "friends first: both dealt to her")

	w.tick(w.s.TakeBound() - time.Minute)
	bounceTick(w, amy)
	require.Equal(t, 2, w.s.Fleet.Count(row, Ready), "within the take bound: hers")
	require.Empty(t, bouncedNotes(w))

	w.tick(2 * time.Minute)
	bounceTick(w, amy)
	assert.Zero(t, w.s.Fleet.Count(row, Ready), "past the take bound, never taken: returned")
	for _, id := range []string{"s1-1", "s1-2"} {
		wc := w.s.Fleet.Card(w.s.Primary(id).F("work"))
		require.NotNil(t, wc)
		assert.Contains(t, []string{"m1", "m2"}, wc.Row, "%s dealt again to a live machine", id)
		assert.Equal(t, row, wc.F(FieldBouncedFrom))
	}
	for _, m := range []string{"m1", "m2"} {
		w.must(Take(w.s, TakeReq{As: m, Sel: Sel{Limit: -1}, Who: m}))
	}
	for _, id := range []string{"s1-1", "s1-2"} {
		assert.Equal(t, Working, w.s.Fleet.Card(w.s.Primary(id).F("work")).Col, "%s taken by the fleet", id)
	}
	notes := bouncedNotes(w)
	require.Len(t, notes, 2, "one pushed note a return")
	for _, n := range notes {
		assert.Equal(t, Happened, n.Kind)
		assert.Equal(t, w.s.Coordinator, n.To, "addressed: inbox --push wakes the seat for it")
		assert.Contains(t, n.What, row)
		assert.Contains(t, n.What, "6m0s ago")
		assert.Contains(t, n.What, "returned to the pool")
	}
	assert.True(t, strings.Contains(notes[0].What, "s1-1") || strings.Contains(notes[1].What, "s1-1"))
}

// B3: a card returned from a friend is never dealt to her again, when no machine is up to
// take it: it waits for another worker, never for the friend that did not take it.
func TestACardBouncedOffAFriendIsNotDealtBackToHer(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	bounceTick(w, amy)
	require.Equal(t, 1, w.s.Fleet.Count(FriendRow("amy"), Ready))
	w.tick(w.s.TakeBound() + time.Second)
	bounceTick(w, amy)
	assert.Zero(t, w.s.Fleet.Count(FriendRow("amy"), Ready), "not dealt back to her")
	require.Len(t, bouncedNotes(w), 1)
	w.tick(time.Minute)
	bounceTick(w, amy)
	assert.Zero(t, w.s.Fleet.Count(FriendRow("amy"), Ready), "nor the tick after")
	assert.Len(t, bouncedNotes(w), 1, "one return, one push")
}

// A reader that is down holding asked reads, and one that never begins them (2026-10-10: 43
// reads asked of a friend's reader row whose lanes all failed, 51 cards in review for over
// 75 minutes): past the take bound each read is retired spending no one, asked again of the
// next capable reader in the same tick, and begun there; each return is pushed.
func TestReadsAskedOfAReaderThatIsDownBounceToALiveReader(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2", "m3")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	putReviewBy(w, "s1-2", "s1-2: work (s1)\n", "m1", 2)
	bounceTick(w)
	first := map[string]string{}
	for _, id := range []string{"s1-1", "s1-2"} {
		rc := readCardsOf(w, id)
		require.Len(t, rc, 1)
		first[id] = rc[0].Row
	}
	// the reader s1-1's went to goes down; a read on another reader is begun there
	down := first["s1-1"]
	for _, id := range []string{"s1-1", "s1-2"} {
		if rc := readCardsOf(w, id)[0]; rc.Row != down {
			w.must(Take(w.s, TakeReq{As: rc.Row, Sel: Sel{Limit: -1}, Who: rc.Row}))
		}
	}
	w.s.MemberCtl(down).Fields["status"] = Down
	w.tick(w.s.TakeBound() + time.Second)
	bounceTick(w)
	held := 0
	for _, id := range []string{"s1-1", "s1-2"} {
		rc := readCardsOf(w, id)
		require.Len(t, rc, 1, "%s asked again at once", id)
		assert.NotEqual(t, down, rc[0].Row, "never the reader down")
		assert.NotEqual(t, "m1", rc[0].Row, "never its worker")
		if first[id] == down {
			held++
			w.must(Take(w.s, TakeReq{As: rc[0].Row, Sel: Sel{Limit: -1}, Who: rc[0].Row}))
		}
		assert.Equal(t, Working, w.s.Fleet.Card(rc[0].ID).Col, "begun on the live reader")
	}
	notes := bouncedNotes(w)
	require.Len(t, notes, held, "each read the down reader held returned, each with its push; no other")
	for _, n := range notes {
		assert.Equal(t, w.s.Coordinator, n.To)
		assert.Contains(t, n.What, "not begun within the take bound")
		assert.Contains(t, n.What, down)
	}
}

// The reads keep being asked until the card has them (the owner: "This should continue
// until the reads are done, retrying, and notifying you of failures"): a reader that does
// not begin, then a second, then a third that reads it ok. Two pushes, and the read is not
// asked again once it is read.
func TestAReadIsAskedAgainUntilAReaderReadsIt(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2", "m3", "m4")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	var tried []string
	for range 2 {
		bounceTick(w)
		rc := readCardsOf(w, "s1-1")
		require.Len(t, rc, 1)
		tried = append(tried, rc[0].Row)
		w.tick(w.s.TakeBound() + time.Second)
	}
	bounceTick(w)
	rc := readCardsOf(w, "s1-1")
	require.Len(t, rc, 1, "asked a third time")
	third := rc[0]
	require.NotEqual(t, "m1", third.Row)
	w.must(Take(w.s, TakeReq{As: third.Row, Sel: Sel{Limit: -1}, Who: third.Row}))
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: third.Row, Verdict: "ok", Sel: Sel{IDs: []string{third.ID}}}))
	assert.Equal(t, "ok", w.s.Fleet.Card(third.ID).F("verdict"))
	assert.Len(t, bouncedNotes(w), 2, "each failure pushed")
	w.tick(w.s.TakeBound() + time.Second)
	bounceTick(w)
	assert.Empty(t, readCardsOf(w, "s1-1"), "read: not asked again")
	assert.Len(t, bouncedNotes(w), 2)
}

// A member whose every lane works holds its ready queue (DealAhead): a card waiting behind
// it is the member's queue, never a member that does not take. No bounce, no push.
func TestACardWaitingBehindAWorkingMemberDoesNotBounce(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 1}))
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	bounceTick(w)
	require.Equal(t, 2, w.s.Fleet.Count("m1", Ready))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 1}, Who: "m1"}))
	for range 6 {
		w.tick(15 * time.Minute)
		bounceTick(w)
	}
	assert.Equal(t, 1, w.s.Fleet.Count("m1", Ready), "still its queue")
	assert.Empty(t, bouncedNotes(w))
}

// Reader A's probe on PR 69: a friend's beat may name her card running by its job name
// (<id>~<epoch>, at its generation); that card is begun and never bounced off her.
func TestACardHerBeatNamesByJobIsNotBounced(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("only friend amy"))
	w.s.Epoch = 7
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	bounceTick(w, amy)
	c := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, c)
	job := StoredID(c.ID, w.s.Epoch)
	if g := c.Int("gen"); g > 1 {
		job += ".g" + itoa(g)
	}
	amy.Running = []string{job}
	w.tick(w.s.TakeBound() + time.Minute)
	w.part(TickBounce, TickReq{Friends: []FriendSeat{amy}})
	assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card(c.ID).Row, "her lane runs it: hers")
	assert.Empty(t, bouncedNotes(w))
}

// Reader B's probe on PR 69: a machine STOPPED for the whole span moves nothing. The
// bounce-back measures running time, and it is the one path: the deal's take-back retires
// no unstarted read on the wall clock, silently.
func TestAStoppedMachineRetiresNoReadSilently(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2", "m3")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	req := TickReq{Stopped: func(from, to time.Time) time.Duration { return to.Sub(from) }}
	w.part(TickBounce, req)
	w.part(TickDeal, req)
	rc := readCardsOf(w, "s1-1")
	require.Len(t, rc, 1)
	w.tick(10 * time.Minute)
	w.part(TickBounce, req)
	w.part(TickDeal, req)
	c := w.s.Fleet.Card(rc[0].ID)
	assert.True(t, c.Placed(), "stopped the whole time: still its reader's")
	assert.Empty(t, c.F("retired_by"))
	assert.Empty(t, bouncedNotes(w))
}
