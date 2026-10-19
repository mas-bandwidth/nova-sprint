package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deal throttles new work while landing backs up (deal_throttle.go; tla/DealThrottle.tla).
// Every test is on the in-memory world (no store, no socket), its clock set to 1000.0 s and
// moved only by argument.

// throttleWorld is priorityWorld (the friend amy up at width 2, room 4) with the clock at
// 1000 s, one flash read she may take in review (s1-1), and the ready cards of stream s2:
// s2-1 and s2-2 new work at attempt 0, s2-3 a fix card; merging holds merging primaries of
// stream s4.
func throttleWorld(t *testing.T, working, merging int) (*world, FriendSeat) {
	t.Helper()
	w, amy := priorityWorld(t, working, 1, 0, 0)
	w.s.Now = time.Unix(1000, 0).UTC()
	w.must(Add(w.s, AddReq{Stream: "s2", Cards: []CardAdd{
		{ID: "s2-1", Brief: friendBrief("only friend amy")},
		{ID: "s2-2", Brief: friendBrief("only friend amy")},
		{ID: "s2-3", Brief: "c: a fix\nREPO: mas-bandwidth/nova-tools\nWHO: only friend amy\nPRIORITY: fix\n\nThe task."},
	}}))
	w.s.Work.SetRows(append(w.s.Work.Rows(), "s4"))
	for i := 1; i <= merging; i++ {
		id := "s4-" + itoa(i)
		w.s.Work.Put(&Card{ID: id, Row: "s4", Col: Merging, Score: float64(i), Rev: 1, Fields: map[string]string{
			"kind": "primary", "attempt": "1", "stream": "s4", "brief": id + ": merging"}})
	}
	return w, amy
}

// dealTick runs the tick's deal once with amy's seat and applies its plan; the throttle's own
// refusal is the only refusal it may carry.
func dealTick(t *testing.T, w *world, amy FriendSeat) Plan {
	t.Helper()
	p, _ := tickDealFixture(w.s, TickReq{Friends: workerSeatFixtures([]FriendSeat{amy})})
	for _, r := range p.Refused {
		require.Equal(t, "throttle", r.Key, "refused: %v", p.Refused)
	}
	return w.do(p)
}

// dealtTo says the primary has a work card at its attempt on amy's row.
func dealtTo(w *world, id, name string) bool {
	c := w.s.Work.Card(id)
	if c == nil || c.Int("attempt") == 0 {
		return false
	}
	wc := w.s.Fleet.Card(WorkCardID(id, c.Int("attempt")))
	return wc != nil && wc.Row == FriendRow(name)
}

// TestTheDealStopsNewWorkWhileLandingBacksUp: merging three above its land bound of two,
// the tick's deal leaves the attempt-1 work card ready, while the fix card and the read in
// the same tick are dealt; the throttle names its counts on the deal's refusal and on the
// where line, and the ready-over part explains the held card without raising a judgment.
func TestTheDealStopsNewWorkWhileLandingBacksUp(t *testing.T) {
	t.Parallel()
	w, amy := throttleWorld(t, 0, 3)
	w.s.Work.SetProp(PropLandBacklogBound, "2")
	on, why := w.s.WorkThrottled()
	require.True(t, on, "merging 3 is above the land bound 2")
	assert.Contains(t, why, "3 primaries merging, above the land backlog bound of 2")

	p := dealTick(t, w, amy)
	assert.Equal(t, Ready, w.s.StateOf("s2-1"), "a new work attempt is not started")
	assert.Equal(t, Ready, w.s.StateOf("s2-2"), "nor the second")
	assert.False(t, dealtTo(w, "s2-1", "amy"))
	assert.True(t, dealtTo(w, "s2-3", "amy"), "the fix card is dealt in the same tick")
	assert.Equal(t, []string{"s1-1"}, friendReads(w, "amy"), "the read is placed in the same tick")
	var refusal string
	for _, r := range p.Refused {
		if r.Key == "throttle" {
			refusal = r.Why
		}
	}
	assert.Contains(t, refusal, "2 new work cards held in ready", "one refusal names the held cards")
	assert.Contains(t, refusal, "above the land backlog bound of 2")
	assert.Contains(t, ThrottleLine(w.s.Work.Props(), 1, 3), "3 primaries merging, above the land backlog bound of 2", "where shows the throttle as a line")

	// the ready-over part: the held card is explained by the throttle and raises nothing
	w.s.Work.Card("s2-1").Fields[FieldReadyAt] = stamp(w.s.Now.Add(-10 * time.Minute))
	_, rw := w.s.readyWhy(w.s.Work.Card("s2-1"))
	assert.Contains(t, rw, "the deal starts no new work while landing backs up")
	assert.Empty(t, readyOverConds(w.s, TickReq{}), "a throttled card is no ready-over alarm")
}

// TestTheThrottleDealsFixesAndReadsAndKeepsWhatIsInFlight: while the throttle stands, a
// card working on amy's row stays working (nothing is cut or recalled), and a rework, a
// primary whose next deal cuts attempt 2, is dealt.
func TestTheThrottleDealsFixesAndReadsAndKeepsWhatIsInFlight(t *testing.T) {
	t.Parallel()
	w, amy := throttleWorld(t, 1, 3)
	w.s.Work.SetProp(PropLandBacklogBound, "2")
	// s2-2 reworked: its attempt 1 ran, its next deal is attempt 2, at normal priority
	w.s.Work.Card("s2-2").Fields["attempt"] = "1"
	require.True(t, NewWorkAttempt(w.s.Work.Card("s2-1")))
	require.False(t, NewWorkAttempt(w.s.Work.Card("s2-2")), "a next attempt is not new work")

	dealTick(t, w, amy)
	assert.Equal(t, Working, w.s.StateOf("s3-1"), "the working card stays working")
	wc := w.s.Fleet.Card(WorkCardID("s3-1", 1))
	require.NotNil(t, wc)
	assert.Equal(t, Working, wc.Col, "its work card is not recalled")
	assert.True(t, dealtTo(w, "s2-2", "amy"), "the rework attempt is dealt")
	assert.Equal(t, 2, w.s.Work.Card("s2-2").Int("attempt"), "as attempt 2")
	assert.Equal(t, Ready, w.s.StateOf("s2-1"), "the new work attempt waits")
}

// TestTheThrottleLiftsWhenLandingDrains: the same cards, held while merging is three; the
// lander lands one, merging falls to the bound of two, and the next tick deals the work card.
func TestTheThrottleLiftsWhenLandingDrains(t *testing.T) {
	t.Parallel()
	w, amy := throttleWorld(t, 0, 3)
	w.s.Work.SetProp(PropLandBacklogBound, "2")
	dealTick(t, w, amy)
	require.Equal(t, Ready, w.s.StateOf("s2-1"), "held while merging is 3")

	w.place(w.s.Work, "s4-1", "s4", Landed)
	w.tick(30 * time.Second)
	on, _ := w.s.WorkThrottled()
	require.False(t, on, "merging 2 is at the bound: the throttle lifts")
	assert.Empty(t, ThrottleLine(w.s.Work.Props(), 1, 2), "where shows no throttle line")
	dealTick(t, w, amy)
	assert.True(t, dealtTo(w, "s2-1", "amy"), "the work card is dealt on the first tick after the drain")
}

// TestTheThrottleBoundsDefaultOff: with no bound set nothing is throttled, whatever the
// counts, and the settings read off; a review bound alone, review above it, throttles.
func TestTheThrottleBoundsDefaultOff(t *testing.T) {
	t.Parallel()
	w, amy := throttleWorld(t, 0, 10)
	for i := 1; i <= 10; i++ {
		id := "s4-r" + itoa(i)
		w.s.Work.Put(&Card{ID: id, Row: "s4", Col: Review, Score: float64(20 + i), Rev: 1, Fields: map[string]string{
			"kind": "primary", "attempt": "1", "stream": "s4", "brief": id + ": in review tier: frontier"}}) // above amy's tiers: no read of hers fills her room
	}
	on, why := w.s.WorkThrottled()
	assert.False(t, on, "no bound set: never throttled (%s)", why)
	for _, v := range SettingsOf(w.s) {
		if v.Flag == "land-backlog-bound" || v.Flag == "review-backlog-bound" {
			assert.Equal(t, BacklogBoundOff, v.Value, "%s reads off by default", v.Flag)
			assert.False(t, v.Set, "%s is not set", v.Flag)
		}
	}
	for _, v := range []string{"0", BacklogBoundOff, ReadTierDefault} {
		w.s.Work.SetProp(PropLandBacklogBound, v)
		on, _ := w.s.WorkThrottled()
		assert.False(t, on, "a land bound of %q never throttles", v)
	}
	w.s.Work.SetProp(PropLandBacklogBound, BacklogBoundOff)

	// review alone, 11 above its bound of 5 (merging's bound off), throttles
	w.s.Work.SetProp(PropReviewBacklogBound, "5")
	on, why = w.s.WorkThrottled()
	require.True(t, on)
	assert.Contains(t, why, "11 primaries in review, above the review backlog bound of 5")
	assert.NotContains(t, why, "merging")
	dealTick(t, w, amy)
	assert.Equal(t, Ready, w.s.StateOf("s2-1"), "review over its bound holds new work")
	assert.True(t, dealtTo(w, "s2-3", "amy"), "the fix still deals")

	// the setting itself: set takes a count, off and default, and refuses a word it does not take
	ok := Set(w.s, SetReq{LandBacklogBound: "30", ReviewBacklogBound: "60", Who: "coordinator"})
	assert.Empty(t, ok.Refused)
	bad := Set(w.s, SetReq{LandBacklogBound: "lots", Who: "coordinator"})
	require.Len(t, bad.Refused, 1)
	assert.Contains(t, bad.Refused[0].Why, "--land-backlog-bound wants a whole number")
}
