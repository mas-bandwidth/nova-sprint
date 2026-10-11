package sprint

import (
	"testing"

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
