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

	stellaSeat := FriendSeat{Name: "stella", Width: 2, Status: Up, Class: "flash,pro"}
	stellaRow := FriendRow("stella")

	t.Run("a take by count on two unstarted ready cards moves neither", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("only friend stella"), friendBrief("only friend stella"))
		dealWith(w, stellaSeat)
		w.s.Friends = []FriendSeat{stellaSeat}
		require.Len(t, w.s.Fleet.Cell(stellaRow, Ready), 2)

		p := w.must(Take(w.s, TakeReq{As: stellaRow, Sel: Sel{Limit: 2}, Who: stellaRow}))
		assert.Empty(t, p.Units, "unstarted ready cards never move on a take by count")
		assert.Equal(t, []string{
			"s1-1.w1: stays ready on friend.stella's row until her start (a take by count is no start)",
			"s1-2.w1: stays ready on friend.stella's row until her start (a take by count is no start)",
		}, p.Said)
		assert.Empty(t, w.s.Fleet.Cell(stellaRow, Working))
		assert.Len(t, w.s.Fleet.Cell(stellaRow, Ready), 2)

		dealWith(w, stellaSeat)
		assert.Empty(t, w.s.Fleet.Cell(stellaRow, Working))
		assert.Len(t, w.s.Fleet.Cell(stellaRow, Ready), 2, "ready row stays intact across the next tick")
	})

	t.Run("a take by count when one ready card is in seat.Running and one is unstarted moves only the started card", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("only friend stella"), friendBrief("only friend stella"))
		dealWith(w, stellaSeat)
		runningSeat := stellaSeat
		runningSeat.Running = []string{"s1-2.w1"}
		w.s.Friends = []FriendSeat{runningSeat}
		require.Len(t, w.s.Fleet.Cell(stellaRow, Ready), 2)

		p := w.must(Take(w.s, TakeReq{As: stellaRow, Sel: Sel{Limit: 1}, Who: stellaRow}))
		require.Len(t, p.Units, 1, "the unstarted card does not consume the count limit; the started card moves")
		assert.Equal(t, []string{
			"s1-1.w1: stays ready on friend.stella's row until her start (a take by count is no start)",
		}, p.Said)

		working := w.s.Fleet.Cell(stellaRow, Working)
		require.Len(t, working, 1)
		assert.Equal(t, "s1-2.w1", working[0].ID)
		assert.True(t, startedNow(working[0]), "FieldStarted is stamped at its generation")

		ready := w.s.Fleet.Cell(stellaRow, Ready)
		require.Len(t, ready, 1)
		assert.Equal(t, "s1-1.w1", ready[0].ID)

		// Even if her next beat omits Running, FieldStarted keeps the started card in Working across the tick.
		dealWith(w, stellaSeat)
		working = w.s.Fleet.Cell(stellaRow, Working)
		require.Len(t, working, 1, "friendUnstartedWorking leaves the started card in Working")
		assert.Equal(t, "s1-2.w1", working[0].ID)
		assert.Len(t, w.s.Fleet.Cell(stellaRow, Ready), 1)
	})

	t.Run("a take with Lane or LaneByID still moves an unstarted card and stamps FieldStarted and FieldLane", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("only friend stella"), friendBrief("only friend stella"))
		dealWith(w, stellaSeat)
		w.s.Friends = []FriendSeat{stellaSeat}

		p := w.must(Take(w.s, TakeReq{As: stellaRow, Lane: 1, Who: stellaRow}))
		require.Len(t, p.Units, 1)
		c1 := w.s.Fleet.Card("s1-1.w1")
		require.NotNil(t, c1)
		assert.Equal(t, Working, c1.Col)
		assert.True(t, startedNow(c1))
		assert.Equal(t, "1", c1.F(FieldLane))

		p = w.must(Take(w.s, TakeReq{
			As:       stellaRow,
			Lane:     2,
			LaneByID: true,
			Sel:      Sel{IDs: []string{"s1-2.w1"}},
			Gens:     map[string]int{"s1-2.w1": 1},
			Who:      stellaRow,
		}))
		require.Len(t, p.Units, 1)
		c2 := w.s.Fleet.Card("s1-2.w1")
		require.NotNil(t, c2)
		assert.Equal(t, Working, c2.Col)
		assert.True(t, startedNow(c2))
		assert.Equal(t, "2", c2.F(FieldLane))
	})

	t.Run("a machine take by count still moves unstarted ready cards", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
		w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 2}))
		pDeal, _ := TickDeal(w.s, TickReq{})
		w.must(pDeal)
		require.Len(t, w.s.Fleet.Cell("m1", Ready), 2)

		p := w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 2}, Who: "m1"}))
		assert.Len(t, p.Units, 2)
		assert.Empty(t, p.Said)
		assert.Len(t, w.s.Fleet.Cell("m1", Working), 2)
		assert.Empty(t, w.s.Fleet.Cell("m1", Ready))
	})
}
