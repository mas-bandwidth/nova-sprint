package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's take by count never moves an unstarted card (docs/SPEC-SPRINT.md
// section 1, a friend's card is working once she starts it; tla/FriendReadyTake.tla
// Take, WorkingIsStarted): on her row a take by count moves only a card she has
// started by the tick's own read (friendStarted), stamped as her start
// (friendStartSet); every other ready card stays ready, named once on a NOTE
// line (Plan.Said). Fleet rows, a lane's take and a take by id are unchanged.
func TestAFriendsTakeByCountNeverMovesAnUnstartedCard(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"), friendBrief("friend amy"))
	row := FriendRow("amy")
	seat := FriendSeat{Name: "amy", Width: 4, Status: Up, Class: "flash", Running: []string{"s1-2.w1"}}
	dealWith(w, seat)
	w.s.Friends = []FriendSeat{seat}
	require.Equal(t, 3, w.s.Fleet.Count(row, Ready), "three work cards ready on her row")
	require.Equal(t, t0, w.s.Now, "the snapshot's clock is fixed")

	// (1) a take by count --max 3 moves exactly the card her beat names running,
	// stamped started at its generation; the other two stay ready, one NOTE each.
	p := w.must(Take(w.s, TakeReq{As: row, Sel: Sel{Limit: 3}, Who: row}))
	require.Len(t, p.Units, 1, "only the started card moves: %+v", p.Units)
	assert.Equal(t, "s1-2.w1", p.Units[0].Key)
	started := w.s.Fleet.Card("s1-2.w1")
	require.Equal(t, Working, started.Col)
	assert.Equal(t, "1", started.F(FieldStarted), "stamped started at its generation")
	assert.Equal(t, Ready, w.s.Fleet.Card("s1-1.w1").Col, "unstarted: stays ready")
	assert.Equal(t, Ready, w.s.Fleet.Card("s1-3.w1").Col, "unstarted: stays ready")
	assert.Equal(t, []string{
		"s1-1.w1: stays ready on friend.amy's row until her start (a take by count is no start)",
		"s1-3.w1: stays ready on friend.amy's row until her start (a take by count is no start)",
	}, p.Said, "each unstarted card named once on a NOTE line")

	// (2) the tick's unstarted-working check finds nothing to put back.
	units, _ := friendUnstartedWorking(w.s, w.s.Friends)
	assert.Empty(t, units, "the take wrote no state the tick is bound to undo")

	// (4) a take by id of an unstarted card on her row still moves it and stamps
	// her start (LaneByID unchanged).
	p = w.must(Take(w.s, TakeReq{As: row, LaneByID: true, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: map[string]int{"s1-1.w1": 1}, Who: row}))
	require.Len(t, p.Units, 1)
	byID := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Working, byID.Col)
	assert.Equal(t, "1", byID.F(FieldStarted), "a take by id is her start")

	// (3) the same take by count on a fleet member's row still moves all three.
	f := newWorld(t)
	f.must(FleetStep(f.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	f.must(Add(f.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	f.must(Deal(f.s, DealReq{Sel: Sel{Limit: 3}}))
	require.Equal(t, 3, f.s.Fleet.Count("m1", Ready))
	f.must(Take(f.s, TakeReq{As: "m1", Sel: Sel{Limit: 3}}))
	assert.Equal(t, 3, f.s.Fleet.Count("m1", Working), "no regression on a fleet row")
	assert.Zero(t, f.s.Fleet.Count("m1", Ready))
}
