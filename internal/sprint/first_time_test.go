package sprint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func markFirstTimeLanded(w *world, id, route string, d time.Duration) {
	w.t.Helper()
	stallCard(w, id, Landed)
	c := w.s.Work.Card(id)
	c.Fields["attempt"] = "1"
	c.Fields["landed"] = stamp(w.s.Now.Add(d))
	if route != "" {
		c.Fields[FieldRoute] = route
	}
}

func markReworkedLanded(w *world, id, failReport string, d time.Duration) {
	w.t.Helper()
	stallCard(w, id, Landed)
	c := w.s.Work.Card(id)
	c.Fields["attempt"] = "2"
	c.Fields["reworks"] = "1"
	c.Fields["fix"] = "fix after failure"
	c.Fields[FieldRoute] = "pro-a"
	c.Fields[FieldFailure] = FailureClass(failReport)
	c.Fields["landed"] = stamp(w.s.Now.Add(d))
}

// TestFirstTimeLandRateCountsOnlyAttemptOneWithNoRework pins item (a): a primary counts
// once it is landed or dropped (an open card or sentinel is not counted), and it landed
// first time only when it landed on attempt 1 with no rework, no brief replacement, and
// no carry. Per-stream and per-route (attempt 1's route) rates are computed over the
// sprint and over the stream's last N counted primaries (default 20).
func TestFirstTimeLandRateCountsOnlyAttemptOneWithNoRework(t *testing.T) {
	t.Parallel()
	w := setup(t, 6)
	w.must(Add(w.s, AddReq{Stream: "s1", Sentinel: true, IDs: []string{"gate1"}}))

	// s1-1: lands on attempt 1 with no rework, no brief replacement, and no carry.
	markFirstTimeLanded(w, "s1-1", "pro-a", 1*time.Minute)
	assert.True(t, FirstTimeCounted(w.s.Work.Card("s1-1")))
	assert.True(t, FirstTimeLanded(w.s.Work.Card("s1-1")))

	// s1-2: lands on attempt 2 after rework -> counted, not first-time.
	markReworkedLanded(w, "s1-2", "verdict not-done; build failed", 2*time.Minute)
	assert.True(t, FirstTimeCounted(w.s.Work.Card("s1-2")))
	assert.False(t, FirstTimeLanded(w.s.Work.Card("s1-2")))

	// s1-3: lands on attempt 1, but its brief was replaced -> counted, not first-time.
	markFirstTimeLanded(w, "s1-3", "pro-a", 3*time.Minute)
	w.s.Work.Card("s1-3").Fields[FieldBriefAttempt] = "1"
	assert.True(t, FirstTimeCounted(w.s.Work.Card("s1-3")))
	assert.False(t, FirstTimeLanded(w.s.Work.Card("s1-3")), "a card whose brief was replaced is not first-time")

	// s1-4: lands on attempt 1, but carries spend/work from a replaced card -> counted, not first-time.
	markFirstTimeLanded(w, "s1-4", "pro-b", 4*time.Minute)
	w.s.Work.Card("s1-4").Fields[FieldCostCarried] = "1.25"
	assert.True(t, FirstTimeCounted(w.s.Work.Card("s1-4")))
	assert.False(t, FirstTimeLanded(w.s.Work.Card("s1-4")), "a card with carry is not first-time")

	// s1-5: dropped -> counted, not first-time.
	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-5"}}, Reason: "obsolete"}))
	assert.True(t, FirstTimeCounted(w.s.Work.Card("s1-5")))
	assert.False(t, FirstTimeLanded(w.s.Work.Card("s1-5")))

	// s1-6: still open (ready), and gate1 is a sentinel -> neither is counted.
	assert.False(t, FirstTimeCounted(w.s.Work.Card("s1-6")), "an open card has not been tried to its end")
	assert.False(t, FirstTimeCounted(w.s.Work.Card("gate1")), "a sentinel is not a primary")

	ft := FirstTimeOf(w.s)
	st := ft.Streams["s1"]
	assert.Equal(t, 5, st.Overall.Counted)
	assert.Equal(t, 1, st.Overall.FirstTime)
	assert.Equal(t, "20% 1/5", st.Overall.String())
	assert.Equal(t, 90, ft.Target)
	assert.Equal(t, 20, ft.Window)
	assert.Equal(t, 1, ft.Routes["pro-a"].FirstTime)
	assert.Equal(t, 3, ft.Routes["pro-a"].Counted)
}

// TestFirstTimeLandRateJudgedUnderTargetOnce pins item (d): no judgment is raised before
// a stream has at least 10 counted primaries in its window; once at least 10 are counted
// and the windowed rate is under target, TickDeadlines raises NFirstTimeUnderTarget once
// naming the stream, its rate, the window and its top failure class, re-raises no more
// often than the stall bound, and clears with a happened note when the windowed rate is
// back at or over the target.
func TestFirstTimeLandRateJudgedUnderTargetOnce(t *testing.T) {
	t.Parallel()
	w := setup(t, 22)

	// Land 7 first-time and 2 reworked (9 counted total, 7/9 = 78% < 90%, but < 10 counted).
	for i := 1; i <= 7; i++ {
		markFirstTimeLanded(w, fmt.Sprintf("s1-%d", i), "pro-a", time.Duration(i)*time.Second)
	}
	markReworkedLanded(w, "s1-8", "verdict not-done; gate red", 8*time.Second)
	markReworkedLanded(w, "s1-9", "verdict not-done; gate red", 9*time.Second)

	p, _ := TickDeadlines(w.s, TickReq{})
	w.must(p)
	assert.Empty(t, w.notesOf(NFirstTimeUnderTarget), "fewer than 10 counted primaries raises no judgment")

	// 10th counted primary lands first-time -> 8/10 = 80% < 90% target, 10 counted.
	markFirstTimeLanded(w, "s1-10", "pro-a", 10*time.Second)

	p, _ = TickDeadlines(w.s, TickReq{})
	w.must(p)
	got := w.notesOf(NFirstTimeUnderTarget)
	require.Len(t, got, 1, "one judgment raised once 10 primaries are counted under target")
	assert.Equal(t, Judgment, got[0].Kind)
	assert.Equal(t, "s1", got[0].Stream)
	assert.Contains(t, got[0].What, "s1")
	assert.Contains(t, got[0].What, "80% 8/10")
	assert.Contains(t, got[0].What, "window 20")
	assert.Contains(t, got[0].What, "verdict not-done; gate red")

	// Within the 1-minute stall bound, TickDeadlines does not re-raise.
	w.tick(LandingStallBound / 2)
	p, _ = TickDeadlines(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NFirstTimeUnderTarget), 1, "re-raises no more often than the stall bound")
	assert.Empty(t, w.updates, "no in-place rewrite before the stall bound")

	// At the 1-minute stall bound, TickDeadlines re-raises in place.
	w.tick(LandingStallBound)
	p, _ = TickDeadlines(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NFirstTimeUnderTarget), 1, "re-raised in place, one open judgment")
	require.NotEmpty(t, w.updates, "the standing judgment is rewritten with fresh facts")

	// Land s1-11..s1-22 first-time so the last-20 window holds s1-3..s1-22: 18/20 = 90% (at target).
	for i := 11; i <= 22; i++ {
		markFirstTimeLanded(w, fmt.Sprintf("s1-%d", i), "pro-a", time.Duration(i)*time.Second)
	}

	w.tick(time.Minute)
	p, _ = TickDeadlines(w.s, TickReq{})
	assert.Len(t, p.Closes, 1, "clearing the episode closes the standing judgment")
	w.must(p)
	recovered := w.notesOf(NFirstTimeRecovered)
	require.Len(t, recovered, 1, "recovery to target emits a happened note")
	assert.Equal(t, Happened, recovered[0].Kind)
	assert.Contains(t, recovered[0].What, "s1")
	assert.Contains(t, recovered[0].What, "90% 18/20")
	_, open := openJudgmentOf(w.s, NFirstTimeUnderTarget)
	assert.False(t, open, "no stale first-time judgment remains open")
}
