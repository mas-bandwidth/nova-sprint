package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// An empty run is a lane that wrote no report. On 2026-10-09/10 a friend's opencode lanes
// ended so, the classifier took the report's Cost line for its first error ("cost line"), and the failed
// rule reworked the card on its tier straight back to the lane that ran it empty.

// emptyRunReport is the failed report of an empty run as the sprint log quoted it.
func emptyRunReport(friend string) string {
	return "friend " + friend + " FAIL: Verdict: FAIL nova-friend of " + friend + ": the lane ended with exit 0 after 154s and wrote no report (harness fault, not a finding); first error: Cost: $0.00 (intro rate, route flash-mercury) (invoice effective: $0.00) (opencode: $0.00) tokens input=0 cache_read=0 cache_write=0 output=0 reasoning=0 model=inception/mercury-2.5 harness=opencode price_route=flash-mercury"
}

func TestAnEmptyRunIsItsOwnClassBeforeTheCostLine(t *testing.T) {
	t.Parallel()
	for name, report := range map[string]string{
		"the log's report":         emptyRunReport("amy"),
		"the lane's fault words":   "friend amy FAIL: harness-fault: no report; first error: the harness printed no error line",
		"no report, a cost beside": "friend amy FAIL: the lane wrote no report; Cost: $0.02 tokens input=10 cache_read=0 cache_write=0 output=0",
	} {
		assert.Equal(t, ClassEmptyRun, HarnessFault(report), name)
	}
	// reversed witnesses: a run that spent tokens is no empty run, its Cost line is the cost line's
	zhi := "friend zhi HOLD: Cost: $0.09 (list price, route flash-deepseek41-direct) (opencode: -) tokens input=84549 cache_read=3719936 cache_write=0 output=34474 reasoning=0 model=deepseek/deepseek-v4.1-flash harness=dsh price_route=flash-deepseek41-direct"
	assert.Equal(t, "cost line", HarnessFault(zhi), "tokens spent: the cost line")
	assert.Equal(t, "lane died", HarnessFault(harnessFaults["lane died"]), "a runner's kill stays lane died")
	// a zero tokens line with a report is no empty run: the daemon can read a lane's tokens
	// from the wrong session store, so zero tokens alone is no evidence of an empty run
	zero := "friend amy HOLD: Cost: $0.00 (opencode: $0.00) tokens input=0 cache_read=0 cache_write=0 output=0 reasoning=0 model=m harness=opencode"
	assert.NotEqual(t, ClassEmptyRun, HarnessFault(zero), "zero tokens alone")
}

// emptyRunOnAmy is a world with s1-1 a card for any friend (WHO: friend) dealt to amy alone,
// started, and failed with the report; amy and bob's seats.
func emptyRunOnAmy(t *testing.T, who, report string) (*world, FriendSeat, FriendSeat) {
	t.Helper()
	w := friendWorld(t, friendBrief(who))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	bob := FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	startLanes(w, amy)
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, FriendRow("amy"), wc.Row, "dealt to amy")
	require.Equal(t, Working, wc.Col, "started on her row")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Report: report, Failed: true}))
	require.Equal(t, Review, w.state("s1-1"))
	return w, amy, bob
}

func TestAnEmptyRunIsNeverReworkedOntoTheFriendWhoseLaneRanItEmpty(t *testing.T) {
	t.Parallel()
	t.Run("any friend's card: the rework goes to another friend", func(t *testing.T) {
		t.Parallel()
		w, amy, bob := emptyRunOnAmy(t, "friend", emptyRunReport("amy"))
		a := answerOn(t, w, on(amy, bob), NWorkFailed, "s1-1")
		rules(w, on(amy, bob))
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, 1, pr.Int("reworks"), "reworked by rule: %s %s", a.Act, a.Why)
		assert.Equal(t, "amy", pr.F(FieldFriendsLeft), "the primary has left amy")
		assert.Contains(t, pr.F(FieldNote), "never again on friend amy")
		dealWith(w, amy, bob)
		wc := w.s.Fleet.Card(WorkCardID("s1-1", 2))
		require.NotNil(t, wc, "the next attempt is dealt")
		assert.Equal(t, FriendRow("bob"), wc.Row, "never back to amy")
		// the new work card carries her as left, so the rebalance, a lane's start, the level
		// and the coordinator's pass (each reading friendsLeft of the work card) keep it off her
		assert.Contains(t, friendsLeft(wc), "amy", "the work card has left amy too")
	})
	t.Run("amy alone up: it waits, never back to her", func(t *testing.T) {
		t.Parallel()
		w, amy, _ := emptyRunOnAmy(t, "friend", emptyRunReport("amy"))
		rules(w, on(amy))
		dealWith(w, amy)
		assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-1", 2)), "no attempt 2 is dealt: never back to amy")
		assert.Equal(t, Ready, w.state("s1-1"), "it waits ready")
	})
	t.Run("reversed: a cost-line fault leaves no friend", func(t *testing.T) {
		t.Parallel()
		w, amy, bob := emptyRunOnAmy(t, "friend", harnessFaults["cost line"])
		rules(w, on(amy, bob))
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, 1, pr.Int("reworks"))
		assert.Empty(t, pr.F(FieldFriendsLeft), "only an empty run leaves the friend")
	})
	t.Run("reversed: a card that names its friend stays hers", func(t *testing.T) {
		t.Parallel()
		w, amy, bob := emptyRunOnAmy(t, "friend amy", emptyRunReport("amy"))
		rules(w, on(amy, bob))
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, 1, pr.Int("reworks"))
		assert.Empty(t, pr.F(FieldFriendsLeft), "a named friend's rework is hers alone (ReworkPinned): leaving her would strand it")
		dealWith(w, amy, bob)
		wc := w.s.Fleet.Card(WorkCardID("s1-1", 2))
		require.NotNil(t, wc)
		assert.Equal(t, FriendRow("amy"), wc.Row)
	})
}

// The cold reads of nova-sprint #45: an attempt a machine is dealt carries the friends its
// primary has left, and every reader of a work card's friends left reads the primary's too,
// so a rebalance off a full machine never gives it back to the friend whose lane ran it empty.
func TestAMachinesAttemptNeverGoesBackToTheFriendItLeft(t *testing.T) {
	t.Parallel()
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}
	setup := func(fields ...string) *world {
		w := rbWorld(t, "m1")
		rbPlace(w, "s1-1", cardhdr.RouteFlash, "m1", Working)
		rbPlace(w, "s1-2", cardhdr.RouteFlash, "m1", Ready, fields...)
		return w
	}
	t.Run("the primary has left amy: the rebalance keeps it off her", func(t *testing.T) {
		t.Parallel()
		w := setup("pr."+FieldFriendsLeft, "amy")
		assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy}, "machine")), "never back to amy")
		assert.False(t, friendCouldTake(w.s, amy, w.s.Work.Card("s1-2"), w.s.Fleet.Card("s1-2.w1")), "the coordinator's pass never offers it to her")
		assert.False(t, friendCouldTake(w.s, amy, w.s.Work.Card("s1-2"), nil), "nor with no work card")
	})
	t.Run("reversed: a primary that left no one moves to her idle lane", func(t *testing.T) {
		t.Parallel()
		w := setup()
		require.Len(t, rebalanced(w.must(Rebalance(w.s, []FriendSeat{amy}, "machine"))), 1, "the rebalance gives a machine's queued card to an idle friend")
		assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row)
		assert.True(t, friendCouldTake(w.s, amy, w.s.Work.Card("s1-2"), nil))
	})
	t.Run("the machines' deal copies the primary's friends left onto the attempt", func(t *testing.T) {
		t.Parallel()
		w := rbWorld(t, "m1")
		rbPlace(w, "s1-2", cardhdr.RouteFlash, "m1", Working, "pr."+FieldFriendsLeft, "amy")
		u, why := deal(w.s, w.s.Work.Card("s1-2"), "", "m1", map[string]int{}, routeIndexesOf(w.s), nil, nil)
		require.Empty(t, why)
		w.must(Plan{Units: []Unit{u}})
		wc := w.s.Fleet.Card(WorkCardID("s1-2", 2))
		require.NotNil(t, wc)
		assert.Equal(t, "amy", wc.F(FieldFriendsLeft))
	})
}

// The attempt cap's default answer deals a card at its brief's bound to a frontier or heavy
// friend (AttemptCapDeal, friendWithFree): never one the card has left for good, or its
// brief gains WHO: friend <her> and every later rework is pinned to the lane that ran it
// empty (the cold read of nova-sprint #45 at f41a84d).
func TestTheAttemptCapsFriendIsNeverOneTheCardLeft(t *testing.T) {
	t.Parallel()
	seats := []FriendSeat{
		{Name: "amy", Width: 3, Status: Up, Class: cardhdr.RouteFrontier},
		{Name: "bob", Width: 1, Status: Up, Class: cardhdr.RouteHeavy},
	}
	free := map[string]int{"amy": 3, "bob": 1}
	classes := []string{cardhdr.RouteFrontier, cardhdr.RouteHeavy}
	c := &Card{ID: "s1-1", Row: "s1", Col: Ready, Fields: map[string]string{"kind": "primary", FieldFriendsLeft: "amy"}}
	assert.Equal(t, "bob", friendWithFree(seats, free, c, classes...), "amy has the most room, but the card left her")
	c.Fields[FieldFriendsLeft] = "amy,bob"
	assert.Empty(t, friendWithFree(seats, free, c, classes...), "every friend left: none, the deal's judgment")
	// reversed: a card that left no one goes to the friend with the most room
	delete(c.Fields, FieldFriendsLeft)
	assert.Equal(t, "amy", friendWithFree(seats, free, c, classes...))
}
