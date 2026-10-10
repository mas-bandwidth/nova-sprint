package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's lanes are thin clients of the server (lane_hold.go; nova-tools tla/FriendLane.tla).

var amyLanes = FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}

// laneWorld is three cards dealt ready on amy's row (width 2), none started.
func laneWorld(t *testing.T) *world {
	w := friendWorld(t, friendBrief("only friend amy"), friendBrief("only friend amy"), friendBrief("only friend amy"))
	dealWith(w, amyLanes)
	w.s.Friends = []FriendSeat{amyLanes}
	require.Len(t, w.s.Fleet.Cell(FriendRow("amy"), Ready), 3, "dealt ready, unstarted")
	return w
}

func laneTake(w *world, lane int) Plan {
	return Take(w.s, TakeReq{As: FriendRow("amy"), Lane: lane, Who: FriendRow("amy")})
}

// The fault of 2026-10-10 21:42Z on a one-shot friend host: her daemon logged "took" and the server still
// showed the card ready. A take that stamps no start of hers is put back ready by the very next
// tick (friendUnstartedWorking), so every progress after it was refused "not working (it is
// friend.freddy:ready)". A lane's take is her start: the tick keeps it working, on the lane.
func TestALanesTakeIsHerStartAndTheTickKeepsItWorking(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	p := w.must(laneTake(w, 1))
	require.Len(t, p.Units, 1)
	held := LaneHolds(w.s, FriendRow("amy"), 1)
	require.NotNil(t, held)
	assert.Equal(t, Working, held.Col)
	assert.Equal(t, "1", held.F(FieldLane))
	assert.True(t, startedNow(held), "the lane's take is her start")
	dealWith(w, amyLanes) // the tick
	held = w.s.Fleet.Card(held.ID)
	assert.Equal(t, Working, held.Col, "still working after the tick: never put back ready as unstarted")
	assert.Equal(t, "1", held.F(FieldLane))

	t.Run("the take before lanes: a bare take by count is put back ready by the tick", func(t *testing.T) {
		t.Parallel()
		w := laneWorld(t)
		w.must(Take(w.s, TakeReq{As: FriendRow("amy"), Sel: Sel{Limit: 1}, Who: FriendRow("amy")}))
		require.Len(t, w.s.Fleet.Cell(FriendRow("amy"), Working), 1)
		dealWith(w, amyLanes)
		assert.Empty(t, w.s.Fleet.Cell(FriendRow("amy"), Working), "the fault: taken, then ready again a tick later")
	})
}

// A lane asks the server what it holds: the same take answers the card it holds and moves
// nothing; another lane is given another card; past her width a lane is given none.
func TestALanesTakeAnswersWhatItHolds(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	w.must(laneTake(w, 1))
	first := LaneHolds(w.s, FriendRow("amy"), 1)
	require.NotNil(t, first)
	w.s.Now = w.s.Now.Add(time.Minute)
	p := w.must(laneTake(w, 1))
	require.Len(t, p.Units, 1, "the lane holds its card: nothing moves, the ask is the lane heard from")
	assert.Equal(t, "lane 1 of friend.amy heard: it holds "+first.ID, p.Units[0].Moved)
	assert.Empty(t, p.Refused)
	assert.Equal(t, first.ID, LaneHolds(w.s, FriendRow("amy"), 1).ID)
	assert.Equal(t, Working, w.s.Fleet.Card(first.ID).Col)
	assert.Equal(t, stamp(w.s.Now), w.s.Fleet.Card(first.ID).F(FieldLaneAt), "heard from at the ask")

	w.must(laneTake(w, 2))
	second := LaneHolds(w.s, FriendRow("amy"), 2)
	require.NotNil(t, second)
	assert.NotEqual(t, first.ID, second.ID)

	p = w.must(laneTake(w, 3))
	assert.Empty(t, p.Units, "her width is 2: lane 3 is given nothing")
	assert.Nil(t, LaneHolds(w.s, FriendRow("amy"), 3))
	assert.Empty(t, Check(w.s, nil))
}

// A card working on her row that no lane holds (her finish's next, a read dealt into working,
// a card from before lanes asked) is the asking lane's before any ready card is taken.
func TestALaneIsGivenAWorkingCardNoLaneHolds(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	w.must(Take(w.s, TakeReq{As: FriendRow("amy"), Sel: Sel{Limit: 1}, Who: FriendRow("amy")}))
	orphan := w.s.Fleet.Cell(FriendRow("amy"), Working)[0]
	require.Empty(t, orphan.F(FieldLane))
	p := w.must(laneTake(w, 2))
	require.Len(t, p.Units, 1)
	assert.Contains(t, p.Units[0].Moved, "no lane held it")
	held := LaneHolds(w.s, FriendRow("amy"), 2)
	require.NotNil(t, held)
	assert.Equal(t, orphan.ID, held.ID)
	assert.True(t, startedNow(held))
	assert.Len(t, w.s.Fleet.Cell(FriendRow("amy"), Ready), 2, "no ready card was taken")
}

// A lane's take names its member and its lane and nothing else; a friend not up is refused.
func TestALanesTakeRefusals(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	for _, r := range []TakeReq{
		{As: FriendRow("amy"), Lane: -1},
		{As: FriendRow("amy"), Lane: 1, Sel: Sel{IDs: []string{"s1-1.w1"}}},
		{As: FriendRow("amy") + ",m1", Lane: 1},
	} {
		p := Take(w.s, r)
		require.Len(t, p.Refused, 1, "%+v", r)
		assert.Empty(t, p.Units)
	}
	w.s.Friends = []FriendSeat{{Name: "amy", Width: 2, Status: Down, Why: "her beat says down"}}
	p := laneTake(w, 1)
	assert.Empty(t, p.Units)
	assert.Nil(t, LaneHolds(w.s, FriendRow("amy"), 1))
}

// Progress and finish name the lane: a card another lane holds is refused, and the lane drops
// it and asks again; the lane's own stamp is the lane heard from.
func TestProgressAndFinishNameTheLane(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	w.must(laneTake(w, 1))
	c := LaneHolds(w.s, FriendRow("amy"), 1)
	require.NotNil(t, c)
	gens := map[string]int{c.ID: max(c.Int("gen"), 1)}

	p := Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{c.ID}}, As: FriendRow("amy"), Gens: gens, Lane: 2})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "held by lane 1 of friend.amy, not lane 2")

	w.s.Now = w.s.Now.Add(5 * time.Minute)
	w.must(Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{c.ID}}, As: FriendRow("amy"), Gens: gens, Lane: 1}))
	assert.Equal(t, stamp(w.s.Now), w.s.Fleet.Card(c.ID).F(FieldLaneAt), "the lane heard from")

	f := Finish(w.s, FinishReq{Sel: Sel{IDs: []string{c.ID}}, As: FriendRow("amy"), Gens: gens, Lane: 2, Head: "9f3c2e1"})
	require.Len(t, f.Refused, 1)
	assert.Contains(t, f.Refused[0].Why, "held by lane 1 of friend.amy, not lane 2")
}

// A lane not heard from within LaneGoneAfter is gone: the tick bounces its card back ready,
// its lane cleared, and the lane that asks again is told it holds nothing; a lane heard from
// (its progress) is kept.
func TestALaneGoneUnheardBouncesItsCardBack(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	w.must(laneTake(w, 1))
	w.must(laneTake(w, 2))
	gone := LaneHolds(w.s, FriendRow("amy"), 1)
	kept := LaneHolds(w.s, FriendRow("amy"), 2)
	require.NotNil(t, gone)
	require.NotNil(t, kept)
	genBefore := max(gone.Int("gen"), 1)

	w.s.Now = w.s.Now.Add(LaneGoneAfter - time.Minute)
	w.must(Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{kept.ID}}, As: FriendRow("amy"), Gens: map[string]int{kept.ID: max(kept.Int("gen"), 1)}, Lane: 2}))
	dealWith(w, amyLanes)
	require.Equal(t, Working, w.s.Fleet.Card(gone.ID).Col, "inside the bound: kept")

	w.s.Now = w.s.Now.Add(2 * time.Minute)
	p := dealWith(w, amyLanes)
	g := w.s.Fleet.Card(gone.ID)
	assert.NotEqual(t, Working, g.Col, "lane 1 gone: its card bounced back")
	assert.Empty(t, g.F(FieldLane))
	assert.Nil(t, LaneHolds(w.s, FriendRow("amy"), 1), "lane 1 asks again and holds nothing")
	assert.Equal(t, genBefore+1, g.Int("gen"), "a new generation: the gone lane's late report is stale")
	assert.Equal(t, Working, w.s.Fleet.Card(kept.ID).Col, "lane 2 was heard from: kept")
	said := false
	for _, u := range p.Units {
		said = said || (u.Key == gone.ID && strings.Contains(u.Moved, "lane 1 gone"))
	}
	assert.True(t, said, "the bounce is said")
}

// A reader's reads go through the same lane loop: a reader's lane take begins its oldest
// asked read (asked -> reading) on the lane, asked again it answers the same read, and the
// verdict names the lane: another lane's is refused.
func TestAReadersLaneBeginsItsReadAndTheVerdictNamesTheLane(t *testing.T) {
	t.Parallel()
	w, reads := routedReads(t)
	a := reads[0]
	_, isReader := ReaderMachine(a.Row)
	require.True(t, isReader, "a reader's row: %s", a.Row)
	p := w.must(Take(w.s, TakeReq{As: a.Row, Lane: 1, Who: a.Row}))
	require.Len(t, p.Units, 1)
	assert.Contains(t, p.Units[0].Moved, "asked -> reading lane=1")
	held := LaneHolds(w.s, a.Row, 1)
	require.NotNil(t, held)
	assert.Equal(t, a.ID, held.ID)
	assert.Equal(t, Reading, held.Col)
	assert.NotEmpty(t, held.F("begun"))

	p = w.must(Take(w.s, TakeReq{As: a.Row, Lane: 1, Who: a.Row}))
	require.Len(t, p.Units, 1, "the lane holds its read: nothing moves, the ask is the lane heard from")
	assert.Contains(t, p.Units[0].Moved, "heard: it holds "+a.ID)
	assert.Equal(t, Reading, w.s.Readers.Card(a.ID).Col)
	p = w.must(Take(w.s, TakeReq{As: a.Row, Lane: 2, Who: a.Row}))
	assert.Empty(t, p.Units, "nothing else asked of this reader: lane 2 holds nothing")
	assert.Nil(t, LaneHolds(w.s, a.Row, 2))

	usage := "input=500000 output=50000"
	r := Read(w.s, ReadReq{As: a.Row, Verdict: "ok", Usage: usage, Lane: 2, Sel: Sel{IDs: []string{a.ID}}})
	require.Len(t, r.Refused, 1)
	assert.Contains(t, r.Refused[0].Why, "held by lane 1 of "+a.Row+", not lane 2")
	w.must(Read(w.s, ReadReq{As: a.Row, Verdict: "ok", Usage: usage, Lane: 1, Sel: Sel{IDs: []string{a.ID}}}))
	assert.Equal(t, OK, w.s.Readers.Card(a.ID).Col)
	assert.Nil(t, LaneHolds(w.s, a.Row, 1), "the verdict ends the lane's hold")
}

// A card that leaves working by any other path is no lane's: its lane goes with the move, so
// a card that comes back to working by another path (her next, her start) is never held by a
// lane that did not take it.
func TestAMoveByAnotherPathClearsTheLane(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	w.must(laneTake(w, 1))
	c := LaneHolds(w.s, FriendRow("amy"), 1)
	require.NotNil(t, c)
	w.must(Plan{Units: []Unit{{Key: c.ID, Changes: []Change{change(Fleet, moveEntry(c, c.Row, Ready, nil))}}}})
	back := w.s.Fleet.Card(c.ID)
	assert.Empty(t, back.F(FieldLane))
	assert.Empty(t, back.F(FieldLaneAt))
	w.must(Plan{Units: []Unit{{Key: c.ID, Changes: []Change{change(Fleet, moveEntry(back, back.Row, Working, nil))}}}})
	assert.Nil(t, LaneHolds(w.s, FriendRow("amy"), 1), "working again by another path: no lane holds it")
}

// A stop-return names the lane: another lane's card is refused.
func TestAStopReturnNamesTheLane(t *testing.T) {
	t.Parallel()
	w := laneWorld(t)
	w.must(laneTake(w, 1))
	c := LaneHolds(w.s, FriendRow("amy"), 1)
	require.NotNil(t, c)
	p := StopReturn(w.s, StopReturnReq{As: FriendRow("amy"), IDs: []string{c.ID}, Gens: map[string]int{c.ID: max(c.Int("gen"), 1)}, Reason: "stopped", Lane: 2})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "held by lane 1 of friend.amy, not lane 2")
	p = StopReturn(w.s, StopReturnReq{As: FriendRow("amy"), IDs: []string{c.ID}, Gens: map[string]int{c.ID: max(c.Int("gen"), 1)}, Reason: "stopped", Lane: 1})
	require.Len(t, p.Units, 1, "%+v", p.Refused)
}
