package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A friend's take by count moves only the cards she has started (docs/SPEC-SPRINT.md
// section 1, a friend's card is working once she starts it): a batch-mode daemon took
// every ready card by count, the take moved them working with no start, and the tick's
// friendUnstartedWorking put them back ready seconds later, every BatchTakeEvery, so her
// working cell flapped between full and her started count (2026-10-14, 618 returns on one
// row in 20 minutes). tla/FriendReadyTake.tla, WorkingIsStarted.
func TestAFriendsTakeByCountNeverMovesAnUnstartedCard(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"), fleetBrief("flash"))
	amy := FriendSeat{Name: "amy", Width: 4, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-3.w1"} {
		c := w.s.Fleet.Card(id)
		require.NotNil(t, c, "%s is dealt to amy", id)
		require.Equal(t, Ready, c.Col, "%s waits ready on her row", id)
	}

	// her beat names s1-1 running; s1-2 and s1-3 she has not begun
	running := FriendSeat{Name: "amy", Width: 4, Status: Up, Class: "flash,pro", Running: []string{"s1-1.w1"}}
	w.s.Friends = []FriendSeat{running}

	p := Take(w.s, TakeReq{Sel: Sel{Limit: 3}, As: FriendRow("amy")})
	require.Len(t, p.Units, 1, "a take by count moves only the card she started")
	require.Equal(t, "s1-1.w1", p.Units[0].Key)
	said := strings.Join(p.Said, "\n")
	require.Contains(t, said, "s1-2.w1: stays ready", "each unstarted card is named once")
	require.Contains(t, said, "s1-3.w1: stays ready", "each unstarted card is named once")
	w.must(p)

	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "the started card is working")
	require.Equal(t, "1", w.s.Fleet.Card("s1-1.w1").F(FieldStarted), "stamped started at its generation")
	require.Equal(t, Ready, w.s.Fleet.Card("s1-2.w1").Col, "an unstarted card stays ready")
	require.Equal(t, Ready, w.s.Fleet.Card("s1-3.w1").Col, "an unstarted card stays ready")

	// the tick has nothing to undo: no working card on her row lacks her start
	units, _ := friendUnstartedWorking(w.s, []FriendSeat{running})
	require.Empty(t, units, "the take wrote no state the tick puts back")

	// a take by id of an unstarted card is unchanged: it moves and stamps her start
	q := Take(w.s, TakeReq{Sel: Sel{IDs: []string{"s1-2.w1"}}, As: FriendRow("amy"), Gens: gensOf(w.s, "s1-2.w1"), LaneByID: true})
	require.Len(t, q.Units, 1, "a take by id still moves the named card")
	w.must(q)
	require.Equal(t, Working, w.s.Fleet.Card("s1-2.w1").Col)
	require.Equal(t, "1", w.s.Fleet.Card("s1-2.w1").F(FieldStarted), "a take by id is her start")
}

// A fleet member's take by count is unchanged: it moves every ready card up to its width.
func TestAMembersTakeByCountStillMovesEveryReadyCard(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"), fleetBrief("flash"))
	dealWith(w)
	m := "m1"
	ready := len(w.s.Fleet.Cell(m, Ready))
	require.Positive(t, ready, "the deal placed ready cards on %s", m)
	p := Take(w.s, TakeReq{Sel: Sel{Limit: -1}, As: m})
	require.Empty(t, p.Said, "no card stays ready on a member's take")
	require.Len(t, p.Units, min(ready, w.s.Width(m)), "a member takes every ready card to its width")
}
