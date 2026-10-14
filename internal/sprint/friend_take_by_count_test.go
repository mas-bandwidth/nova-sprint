package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's take by count moves only cards she has started; the rest stay ready
// (steps_work.go takeOne; tla/FriendReadyTake.tla; docs/SPEC-SPRINT.md section 1).
func TestAFriendsTakeByCountNeverMovesAnUnstartedCard(t *testing.T) {
	t.Parallel()

	stellaSeat := FriendSeat{Name: "stella", Width: 4, Status: Up, Class: "flash,pro"}
	stellaRow := FriendRow("stella")

	// Build a snapshot in memory with one friend up, width 4, three work cards ready on her row,
	// and her seat's Running naming one of them.
	w := friendWorld(t, friendBrief("only friend stella"), friendBrief("only friend stella"), friendBrief("only friend stella"))
	dealWith(w, stellaSeat)
	runningSeat := stellaSeat
	runningSeat.Running = []string{"s1-2.w1"}
	w.s.Friends = []FriendSeat{runningSeat}
	require.Len(t, w.s.Fleet.Cell(stellaRow, Ready), 3)

	// (1) Take by count --max 3 as her row moves exactly the named card to working with
	// FieldStarted at its generation, and the other two stay ready with a NOTE line each.
	p := w.must(Take(w.s, TakeReq{As: stellaRow, Sel: Sel{Limit: 3}, Who: stellaRow}))
	require.Len(t, p.Units, 1, "only the started card moves on a take by count")
	assert.Equal(t, []string{
		"s1-1.w1: stays ready on friend.stella's row until her start (a take by count is no start)",
		"s1-3.w1: stays ready on friend.stella's row until her start (a take by count is no start)",
	}, p.Said)

	working := w.s.Fleet.Cell(stellaRow, Working)
	require.Len(t, working, 1)
	assert.Equal(t, "s1-2.w1", working[0].ID)
	assert.True(t, startedNow(working[0]), "FieldStarted is stamped at its generation")
	assert.Equal(t, "1", working[0].F(FieldStarted))

	ready := w.s.Fleet.Cell(stellaRow, Ready)
	require.Len(t, ready, 2)
	assert.Equal(t, "s1-1.w1", ready[0].ID)
	assert.Equal(t, "s1-3.w1", ready[1].ID)

	// (2) The tick's friendUnstartedWorking on the resulting snapshot returns no unit
	// (even if the next beat does not name Running, because FieldStarted is stamped).
	units, _ := friendUnstartedWorking(w.s, w.s.Friends)
	assert.Empty(t, units, "friendUnstartedWorking on the resulting snapshot returns no unit")
	unitsNoBeat, _ := friendUnstartedWorking(w.s, []FriendSeat{stellaSeat})
	assert.Empty(t, unitsNoBeat, "FieldStarted keeps the card working even without Running on the beat")

	// (4) A take by id of an unstarted card on her row still moves it and stamps her start (LaneByID unchanged).
	pByID := w.must(Take(w.s, TakeReq{
		As:       stellaRow,
		Lane:     1,
		LaneByID: true,
		Sel:      Sel{IDs: []string{"s1-1.w1"}},
		Gens:     map[string]int{"s1-1.w1": 1},
		Who:      stellaRow,
	}))
	require.Len(t, pByID.Units, 1)
	c1 := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, c1)
	assert.Equal(t, Working, c1.Col)
	assert.True(t, startedNow(c1), "LaneByID stamps her start")
	assert.Equal(t, "1", c1.F(FieldLane))

	// (3) The same take by count --max 3 on a fleet member's row still moves all three (no regression).
	wm := newWorld(t, "reader-a")
	wm.setWidth("m1", 4)
	wm.must(FleetStep(wm.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	wm.must(Add(wm.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	pDeal, _ := TickDeal(wm.s, TickReq{})
	wm.must(pDeal)
	require.Len(t, wm.s.Fleet.Cell("m1", Ready), 3)

	pm := wm.must(Take(wm.s, TakeReq{As: "m1", Sel: Sel{Limit: 3}, Who: "m1"}))
	assert.Len(t, pm.Units, 3, "fleet member take by count moves all three ready cards")
	assert.Empty(t, pm.Said)
	assert.Len(t, wm.s.Fleet.Cell("m1", Working), 3)
	assert.Empty(t, wm.s.Fleet.Cell("m1", Ready))
}
