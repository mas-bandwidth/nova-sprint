package sprint_test

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verified working (card sn-verified-working-b-ns-b.w1; docs/SPEC-SPRINT.md section 1,
// "Verified working"; the owner: "trust but VERIFY", "Are they actually doing the work
// that is shown in the friend table? Really?"): the friends table's dealt column beside
// working counts every card of hers in working on her row, and working counts only the
// verified of them, a card with a push on its branch or named in her beat --running
// (the started read friend take makes of her, sprint.FriendTakeReq.Started), so dealt
// and working differ exactly when she holds cards no push proves and her beat does not
// name. A friend holding working cards and finishing none raises one judgment per friend
// after the friend-finish window's default, fifteen minutes, its decisions return (take
// back the cards she has not started) and wait.
func TestWorkingCountsOnlyVerifiedCardsAndAnIdleFriendRaisesOneJudgment(t *testing.T) {
	t.Parallel()

	// two friends holding cards: amy her two lanes, bob his one
	r := &passRig{holdRig: newHoldRig(t, 1, 3),
		pongs:    map[string]time.Time{"amy": holdT0.Add(-passPongBefore), "bob": holdT0.Add(-passPongBefore)},
		answered: map[string]time.Time{}}
	rows, err := r.st.FriendRows(r.ctx, r.clock())
	require.NoError(t, err)
	for _, row := range rows {
		if row.Health != nil {
			r.answered[row.Name] = row.Health.Seen
		}
	}
	r.tick(0)
	s := r.snap()
	amy, bob := sprint.FriendRow("amy"), sprint.FriendRow("bob")
	hers := s.Fleet.Cell(amy, sprint.Working)
	his := s.Fleet.Cell(bob, sprint.Working)
	require.Len(t, hers, 2, "the deal gives amy her two lanes")
	require.Len(t, his, 1, "and bob his one")

	// the counting: dealt is every card of hers in working, working only the verified
	w := sprint.FriendWorkingOf(hers, nil)
	assert.Equal(t, sprint.FriendWorking{Dealt: 2, Working: 0}, w, "nothing is pushed or running: dealt and working differ")
	w = sprint.FriendWorkingOf(hers, map[string]string{hers[0].ID: "a push on its branch sprint/" + hers[0].ID + " at 089c6ef400160c74db35fa5c69134fb78c6d471b"})
	assert.Equal(t, sprint.FriendWorking{Dealt: 2, Working: 1}, w, "a push on its branch verifies one")
	w = sprint.FriendWorkingOf(hers, map[string]string{hers[1].ID: "her beat names it running"})
	assert.Equal(t, sprint.FriendWorking{Dealt: 2, Working: 1}, w, "so does her beat naming it running")
	w = sprint.FriendWorkingOf(hers, map[string]string{hers[0].ID: "a push on its branch", hers[1].ID: "her beat names it running"})
	assert.Equal(t, sprint.FriendWorking{Dealt: 2, Working: 2}, w, "a card both ways counts once")
	w = sprint.FriendWorkingOf(hers, map[string]string{his[0].ID: "a push on its branch"})
	assert.Equal(t, sprint.FriendWorking{Dealt: 2, Working: 0}, w, "another friend's card verifies nothing of hers")

	// the judgment: one per idle friend after fifteen minutes, its decisions return and wait
	fresh := func() {
		r.pongs["amy"], r.pongs["bob"] = r.clock(), r.clock()
	}
	fresh()
	r.tick(10 * time.Minute)
	assert.Nil(t, r.open(sprint.NFriendIdle, amy), "her cards are 10 minutes old: not idle")
	assert.Nil(t, r.open(sprint.NFriendIdle, bob), "his card is 10 minutes old: not idle")

	fresh()
	r.tick(5*time.Minute + 30*time.Second)
	idle := r.open(sprint.NFriendIdle, amy)
	require.NotNil(t, idle, "a friend holding working cards with no finish in 15 minutes is idle")
	assert.Contains(t, idle.What, "friend amy")
	assert.Equal(t, []string{"return", "wait"}, idle.Decisions, "return her unstarted cards, or wait")
	hisIdle := r.open(sprint.NFriendIdle, bob)
	require.NotNil(t, hisIdle, "one judgment per idle friend: bob holds a card too")
	assert.Equal(t, []string{"return", "wait"}, hisIdle.Decisions)
	assert.Equal(t, 2, r.count(sprint.Judgment, sprint.NFriendIdle, ""), "one judgment each, two friends holding cards")

	// still one each while it holds: raised again in place, never a second judgment
	fresh()
	r.tick(10*time.Minute + 30*time.Second)
	assert.Equal(t, 2, r.count(sprint.Judgment, sprint.NFriendIdle, ""), "raised again in place, never a second judgment")
	assert.Equal(t, 2, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NFriendIdle), "the push names each friend once")
}
