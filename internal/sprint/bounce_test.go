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

// bounceTick is the tick's bounce and its deal, as TickTables runs them: the bounce-back,
// then the deal over the view with every member set aside held out.
func bounceTick(w *world, seats ...FriendSeat) {
	w.t.Helper()
	w.part(TickBounce, TickReq{Friends: seats})
	w.part(passAside(TickDeal), TickReq{Friends: seats})
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
// deal of the same tick gives them to the live machines, which take them. Each return is
// a note to the coordinator (pushed: inbox --push wakes the seat for a note with a To)
// naming the card, her row, how long and what was done; she is set aside, one judgment,
// and dealt nothing more until she takes again.
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
	var onFleet []string
	for _, id := range []string{"s1-1", "s1-2"} {
		wc := w.s.Fleet.Card(w.s.Primary(id).F("work"))
		require.NotNil(t, wc)
		assert.Contains(t, []string{"m1", "m2"}, wc.Row, "%s dealt again to a live machine", id)
		assert.Equal(t, row, wc.F(FieldBouncedFrom))
		onFleet = append(onFleet, wc.Row)
	}
	for _, m := range []string{"m1", "m2"} {
		w.must(Take(w.s, TakeReq{As: m, Sel: Sel{Limit: -1}, Who: m}))
	}
	for _, id := range []string{"s1-1", "s1-2"} {
		assert.Equal(t, Working, w.s.Fleet.Card(w.s.Primary(id).F("work")).Col, "%s taken by the fleet", id)
	}

	// B2: every return pushed, saying who, what, how long and what was done
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

	// B3: set aside, one judgment, and the next deal passes her by
	require.Contains(t, AsideRows(w.s), row)
	require.Len(t, w.openOn(StreamSubject(MemberSubject(row))), 1, "one judgment: she is set aside")
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-3", Brief: friendBrief("friend")}}}))
	bounceTick(w, amy)
	assert.Zero(t, w.s.Fleet.Count(row, Ready), "set aside: dealt nothing")
	bounceTick(w, amy)
	require.Len(t, w.openOn(StreamSubject(MemberSubject(row))), 1, "the judgment stands once")
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
	// the reader s1-1's went to goes down (its beat lapses: presence writes it down); a read
	// on another reader is begun there
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
// not begin, then a second, then a third that reads it ok. Two pushes, one read standing,
// and the primary's ok read is the third reader's.
func TestAReadIsAskedAgainUntilAReaderReadsIt(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2", "m3", "m4")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	var tried []string
	for range 2 {
		bounceTick(w)
		rc := readCardsOf(w, "s1-1")
		require.Len(t, rc, 1)
		require.NotContains(t, tried, rc[0].Row, "asked of a reader not yet tried")
		tried = append(tried, rc[0].Row)
		w.tick(w.s.TakeBound() + time.Second)
	}
	bounceTick(w)
	rc := readCardsOf(w, "s1-1")
	require.Len(t, rc, 1, "asked a third time")
	third := rc[0]
	require.NotContains(t, tried, third.Row)
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
// it is the member's queue, never a member that does not take (store dealt_bound_test.go).
// No bounce, no push, not set aside.
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
	assert.NotContains(t, AsideRows(w.s), "m1")
}

// B3: a member whose last TakeFaults finishes ended on a harness fault (an exit 126 lane) is
// set aside until it takes again; and a member set aside by a bounce is tried again once
// the cooldown has passed (its probe).
func TestAMemberIsSetAsideUntilItTakesAgain(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 2}))
	at := w.s.Now
	for i, id := range []string{"x.w1", "y.w1"} {
		w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: DoneFailed, Score: float64(i), Rev: 1, Fields: map[string]string{
			"kind": "work", "taken": stamp(at.Add(-time.Minute)), "finished": stamp(at.Add(time.Duration(i) * time.Second)),
			"report": "the lane exited with status 126: cannot execute"}})
	}
	w.tick(10 * time.Second)
	a, ok := AsideRows(w.s)["m1"]
	require.True(t, ok, "two harness faults in a row: set aside")
	assert.Contains(t, a.Why, "cannot run")
	w.s.Fleet.Put(&Card{ID: "z.w1", Row: "m1", Col: Working, Score: 3, Rev: 1, Fields: map[string]string{"kind": "work", "taken": stamp(w.s.Now)}})
	assert.NotContains(t, AsideRows(w.s), "m1", "it took again")

	w2 := newWorld(t)
	w2.must(FleetStep(w2.s, FleetReq{Op: "up", Member: "m2", Width: 2}))
	w2.s.Fleet.SetProp(PropBounced("m2"), stamp(w2.s.Now))
	require.Contains(t, AsideRows(w2.s), "m2")
	w2.tick(BounceCooldown(w2.s))
	assert.NotContains(t, AsideRows(w2.s), "m2", "past the cooldown: tried again")
}
