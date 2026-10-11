package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The idle episode's end (docs/SPEC-SPRINT.md section 14, "The fleet is idle"):
// TickIdle ends an episode for four reasons, and only the fleet working at half
// its width or more again is a recovery. No card waits (b), no width is up --
// every member down, held or drained (c) -- and the fleet's work switched off
// (d) end it too, and say so: NIdleEnded names the cause, never NIdleCleared's
// "working again". The episode machine is unchanged (AlarmOncePerEpisode,
// ClearFollowsAlarm), so tla/SprintRules.tla, Part "idle", is not touched.

// tickIdle runs the idle part and applies it.
func tickIdle(t *testing.T, w *world) Plan {
	t.Helper()
	p, _ := TickIdle(w.s, TickReq{IdleAlarm: true})
	return w.must(p)
}

// idleSaid is a snapshot with one idle episode begun and said: setup's two up
// members (width 64 each, 128 together) work no card while s1-9 waits, and the
// tick past IdleWindow pushes the note and marks the episode said.
func idleSaid(t *testing.T) *world {
	t.Helper()
	w := setup(t, 4)
	toReview(w, "s1-1")
	w.s.Work.Put(&Card{ID: "s1-9", Row: "s1", Col: Waiting, Score: 9, Rev: 1, Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s1", "brief": proBrief}})
	require.NotEmpty(t, tickIdle(t, w).Units, "the idle episode begins")
	w.tick(2 * IdleWindow)
	tickIdle(t, w)
	require.Len(t, w.notesOf(NIdle), 1, "the episode is said past the window")
	return w
}

// (c) Every member's width is taken away (fleet up <m> --width 0: held or
// drained): nothing works, and the note never says working again.
func TestAnIdleEpisodeEndedByNoWidthNeverSaysWorkingAgain(t *testing.T) {
	t.Parallel()
	w := idleSaid(t)
	for _, m := range []string{"m1", "m2"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Member: m, Drain: true, Who: "coordinator"}))
	}
	working, width := FleetWorking(w.s)
	require.Equal(t, 0, width, "every member drains: no width up")
	require.Equal(t, 0, working)
	tickIdle(t, w)
	assert.Empty(t, w.notesOf(NIdleCleared), "no width up is no recovery: never a working-again note")
	for _, n := range w.notes {
		assert.NotContains(t, n.What, "working again")
	}
	ended := w.notesOf(NIdleEnded)
	require.Len(t, ended, 1, "the end names the cause")
	assert.Contains(t, ended[0].What, "no width up: every member is down, held or drained (fleet up <member> --width <n>)")
}

// (a) The fleet works at half its width again: the recovery keeps the
// working-again note, and pushes no end note.
func TestAnIdleEpisodeEndedByWorkSaysWorkingAgain(t *testing.T) {
	t.Parallel()
	w := idleSaid(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 1, Who: "coordinator"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 1, Who: "coordinator"}))
	w.s.Fleet.Put(&Card{ID: WorkCardID("idle-a", 1), Row: "m1", Col: Working, Rev: 1, Fields: map[string]string{"kind": "primary"}})
	working, width := FleetWorking(w.s)
	require.Equal(t, 1, working, "one card works")
	require.Equal(t, 2, width, "two members of width 1")
	tickIdle(t, w)
	clears := w.notesOf(NIdleCleared)
	require.Len(t, clears, 1, "working half its width again: the recovery note")
	assert.Contains(t, clears[0].What, "working again")
	assert.Empty(t, w.notesOf(NIdleEnded), "no end note on the recovery")
}

// (b) No card waits: the episode ends naming that.
func TestAnIdleEpisodeEndedByNoWaitingCardSaysTheCause(t *testing.T) {
	t.Parallel()
	w := idleSaid(t)
	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-9"}}, Reason: "done with it", Who: "coordinator"}))
	tickIdle(t, w)
	ended := w.notesOf(NIdleEnded)
	require.Len(t, ended, 1, "no card waits: the end names it")
	assert.Contains(t, ended[0].What, "no card waits")
	assert.Empty(t, w.notesOf(NIdleCleared))
}

// (d) The fleet's work is switched off (set --fleet off): the episode ends
// naming the switch.
func TestAnIdleEpisodeEndedByTheFleetSwitchSaysTheCause(t *testing.T) {
	t.Parallel()
	w := idleSaid(t)
	w.must(Set(w.s, SetReq{Fleet: SwitchOff, Who: "coordinator"}))
	tickIdle(t, w)
	ended := w.notesOf(NIdleEnded)
	require.Len(t, ended, 1, "the switch ends the episode and names it")
	assert.Contains(t, ended[0].What, "the fleet's work is switched off (set --fleet on)")
	assert.Empty(t, w.notesOf(NIdleCleared))
}

// The alarm is raised once an episode, never once a tick (docs/SPEC-SPRINT.md section 14,
// the fleet is idle; tla/SprintRules.tla, Part "idle": AlarmOncePerEpisode, and its
// reversed witness MCSprintRulesIdleBrokenEveryTick, "everytick"). The live sprint was
// seen to raise "the fleet works below its alarm" about every 15 seconds, one a tick, and
// the store-level test of the rule was red on main. This drives 15-second ticks over an
// idle fleet: nothing before the window, then exactly one note, held at one for the
// rest of the episode and never zero; the fleet works and the episode ends; it goes idle
// again and the next episode raises exactly one more.
func TestTheIdleAlarmIsRaisedOnceAnEpisodeNeverOncePerTick(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	toReview(w, "s1-1")
	w.s.Work.Put(&Card{ID: "s1-9", Row: "s1", Col: Waiting, Score: 9, Rev: 1, Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s1", "brief": proBrief}})
	const step = 15 * time.Second
	perWindow := int(IdleWindow / step)
	// ticks runs n ticks of step each and checks the count after every one: want(i) is
	// how many alarms the tick i of the run should have left, so a tick that raises a
	// second, or a tick that loses the first, fails at that tick.
	ticks := func(n int, want func(i int) int, why string) {
		t.Helper()
		for i := 1; i <= n; i++ {
			w.tick(step)
			tickIdle(t, w)
			require.Len(t, w.notesOf(NIdle), want(i), "%s: after tick %d (%s)", why, i, w.s.Now.Format("15:04:05"))
		}
	}

	// episode one: the first tick begins it; the window is IdleWindow of running time
	require.NotEmpty(t, tickIdle(t, w).Units, "the idle episode begins")
	require.Empty(t, w.notesOf(NIdle))
	ticks(perWindow-1, func(int) int { return 0 }, "under the window: nothing yet")
	ticks(1, func(int) int { return 1 }, "the window is out: the one alarm")
	ticks(3*perWindow, func(int) int { return 1 }, "the rest of the episode: one, never zero, never a second")
	n := w.notesOf(NIdle)[0]
	assert.True(t, strings.HasPrefix(n.What, "fleet 0/128"), n.What)

	// the fleet works at half its width: the episode recovers, with its one clear note
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 1, Who: "coordinator"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 1, Who: "coordinator"}))
	id := WorkCardID("idle-a", 1)
	w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: Working, Rev: 1, Fields: map[string]string{"kind": "primary"}})
	ticks(perWindow, func(int) int { return 1 }, "the fleet works: no new alarm, the old one stays")
	require.Len(t, w.notesOf(NIdleCleared), 1, "one working-again note for the episode")

	// episode two: the work finishes and the fleet is idle again with a card waiting
	w.place(w.s.Fleet, id, "m1", DoneOK)
	require.NotEmpty(t, tickIdle(t, w).Units, "the second idle episode begins")
	ticks(perWindow-1, func(int) int { return 1 }, "the second episode, under its window: still the first alarm only")
	ticks(1, func(int) int { return 2 }, "the second window is out: one more alarm")
	ticks(3*perWindow, func(int) int { return 2 }, "the rest of the second episode: two in all, one an episode")
}
