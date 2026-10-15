package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The start-bound level leaves a one-shot friend's row to her member, as the
// friend start, the dealt return and the friends' level do (friendStartUnits,
// friendUnstartedWorking, friendDealtReturn, friendLevel: each skips a seat
// whose Session is FriendOneShot): a card dealt to a one-shot and not started
// past the start bound is not levelled off her row, and no stale card is
// levelled onto her row. Her member takes by count; no friend start bound
// moves her cards.
func TestTheUnstartedLevelLeavesAOneShotRowToHerMember(t *testing.T) {
	t.Parallel()
	notStarted := func(p Plan) []string {
		var lines []string
		for _, l := range movedLines(p) {
			if strings.Contains(l, "not started") {
				lines = append(lines, l)
			}
		}
		return lines
	}

	t.Run("a stale card on a one-shot's row stays", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"))
		seats := []FriendSeat{
			{Name: "amy", Width: 1, Status: Up, Class: "flash", Session: FriendOneShot},
			{Name: "bob", Width: 1, Status: Up, Class: "flash"},
		}
		w.must(func() Plan { p, _ := TickDeal(w.s, TickReq{Friends: seats}); return p }())
		require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "dealt to the one-shot she names")
		w.s.Now = t0.Add(FriendStartMaxDefault + time.Minute)
		p, _ := TickDeal(w.s, TickReq{Friends: seats})
		w.must(p)
		assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "her member takes it: the start-bound level does not move it")
		assert.Empty(t, notStarted(p), "no not-started line moves a one-shot's card")
	})

	t.Run("no stale card is levelled onto a one-shot's row", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"))
		seats := []FriendSeat{
			{Name: "amy", Width: 1, Status: Up, Class: "flash"},
			{Name: "bob", Width: 1, Status: Up, Class: "flash", Session: FriendOneShot},
		}
		w.must(func() Plan { p, _ := TickDeal(w.s, TickReq{Friends: seats}); return p }())
		require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row)
		w.s.Now = t0.Add(FriendStartMaxDefault + time.Minute)
		p, _ := TickDeal(w.s, TickReq{Friends: seats})
		w.must(p)
		assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "the only idle lane is a one-shot's member lane: it stays")
		assert.Empty(t, notStarted(p))
	})
}
