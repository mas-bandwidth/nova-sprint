package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A member whose cards are timing out is overloaded (overload.go; the owner, 2026-10-03:
// "the overload is defined as -- cards are timing out. not any CPU%"): three cards ended on
// a timeout of any kind within the window, counted from the finishes the member reported,
// raise "a member is overloaded" naming each card and its kind, offering half the width or
// a wait; two do not; the judgment closes once the window holds fewer than three. On the
// mem twin, the clock injected, no real time.
func TestTheTickRaisesOverloadedOnThreeTimeoutsInTheWindow(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"), route("flash-b", "flash"))
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m2"})) // every card goes to m1
	h.addReady("s1", 3, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		require.Equal(t, sprint.Working, h.state(id))
	}
	const usageLine = "budget: unverifiable: the usage source stopped answering, tokens 12,345, $0.10: no RESULT.md shape"
	h.failTake("s1-1.w1", cardhdr.EndStaging+": "+sprint.TimeoutStaging)
	h.tick(time.Minute)
	h.failTake("s1-2.w1", "deadline: no RESULT.md shape")
	h.machine()
	assert.Empty(t, h.openOf(sprint.NOverloaded), "two timeouts are not an overload")
	h.tick(time.Minute)
	h.failTake("s1-3.w1", usageLine)
	h.machine()
	open := h.openOf(sprint.NOverloaded)
	require.Len(t, open, 1, "three timeouts within the window")
	n := open[0].Note
	assert.Equal(t, "m1 is overloaded: 3 cards ended on a timeout in the last 15m0s: s1-1.w1 (stage-timeout), s1-2.w1 (deadline), s1-3.w1 (usage source); halve its width: nova-sprint fleet up m1 --width 512, or wait 15m", n.What)
	assert.Equal(t, []string{"fleet up m1 --width 512", "wait 15m"}, n.Decisions)
	cmds := h.commandsOf(sprint.NOverloaded)
	require.Len(t, cmds, 2)
	assert.Equal(t, []string{"nova-sprint fleet up m1 --width 512"}, cmds[0].Lines)
	assert.Equal(t, "nova-sprint wait "+n.ID+" --for 15m", cmds[1].Lines[0])
	h.machine()
	assert.Len(t, h.openOf(sprint.NOverloaded), 1, "raised once while it holds")
	assert.Equal(t, 1, h.written(sprint.NOverloaded))
	// the window moves past the first timeout: two remain, and the judgment closes
	h.tick(sprint.OverloadWindow - time.Minute)
	h.machine()
	assert.Empty(t, h.openOf(sprint.NOverloaded), "the window holds two")
	o, ok := sprint.Overloaded(h.snap(), "m1")
	assert.False(t, ok, "%+v", o)
	assert.Len(t, sprint.MemberTimeouts(h.snap(), "m1"), 2)
	h.clean("overloaded, then not")
}

func TestTimeoutKindReadsTheMembersReports(t *testing.T) {
	t.Parallel()
	for report, kind := range map[string]string{
		"staging refused: stage-timeout": sprint.TimeoutStaging,
		"stage-timeout":                  sprint.TimeoutStaging,
		"deadline: no RESULT.md shape":   sprint.TimeoutDeadline,
		"budget: unverifiable: the usage source stopped answering, tokens 1, $0": sprint.TimeoutUsage,
		"staging refused: no bench mirror":                                       "",
		"provider failure: 529":                                                  "",
		"budget: tokens 100 of 100: no RESULT.md shape":                          "",
		"verdict not-done; tests red":                                            "",
	} {
		assert.Equal(t, kind, sprint.TimeoutKind(report), report)
	}
}

// A friend whose cards are timing out is overloaded (overload.go, docs/SPEC-SPRINT.md):
// three timeouts within OverloadWindow on her row friend.<name> raise the judgment with
// her roster width halved, even when nothing is ready in the sprint (friendSeats reads the
// friend rows whenever the roster has a friend, not only when a friend card is ready).
func TestTheTickRaisesFriendOverloadedWithNothingReady(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 4}})
	require.NoError(t, err)
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)

	brief := "c: a friend's card\nWHO: friend amy\n\nThe task.\n"
	h.addReady("s1", 3, brief)
	h.startMachine()
	h.machine()

	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		require.Equal(t, sprint.Working, h.state(id))
	}

	failFriend := func(card, report string) {
		t.Helper()
		wc := h.snap().Fleet.Card(card)
		require.NotNil(t, wc)
		gens := map[string]int{wc.ID: wc.Int("gen")}
		h.must(FinishStep(sprint.FinishReq{
			As:     sprint.FriendRow("amy"),
			Sel:    sprint.Sel{IDs: []string{card}},
			Gens:   gens,
			Failed: true,
			Report: report,
			Usage:  "wall=450.00s budget=1/1000",
			Who:    sprint.FriendRow("amy"),
		}))
	}

	// Two timeouts: no overload yet
	failFriend("s1-1.w1", "friend amy HOLD: deadline: the run passed its deadline")
	h.tick(time.Minute)
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)
	failFriend("s1-2.w1", "friend amy FAIL: budget: unverifiable: the usage source stopped answering, tokens 12,345")
	h.machine()
	assert.Empty(t, h.openOf(sprint.NOverloaded), "two timeouts are not an overload")

	// Third timeout
	h.tick(time.Minute)
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)
	failFriend("s1-3.w1", "friend amy HOLD: stage-timeout")

	// Verify nothing is ready anywhere in the sprint
	snap := h.snap()
	assert.Empty(t, snap.Work.Column(sprint.Ready), "nothing ready in work")
	assert.Empty(t, snap.Fleet.Column(sprint.Ready), "nothing ready in fleet")

	// a held or down friend raises none, as a member not up
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "amy", true, "coordinator", "test hold", time.Time{}, 0))
	h.machine()
	assert.Empty(t, h.openOf(sprint.NOverloaded), "a held friend raises none")

	// Release hold and beat so amy is up
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "amy", false, "coordinator", "", time.Time{}, 0))
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)

	h.machine()

	open := h.openOf(sprint.NOverloaded)
	require.Len(t, open, 1, "three timeouts within the window with nothing ready still raise the judgment")
	n := open[0].Note
	assert.Equal(t, sprint.MemberSubject(sprint.FriendRow("amy")), n.Stream)
	assert.Equal(t, "friend.amy is overloaded: 3 cards ended on a timeout in the last 15m0s: s1-1.w1 (deadline), s1-2.w1 (usage source), s1-3.w1 (stage-timeout); halve its width: nova-config friend set amy --width 2, then nova-sprint friend sync, or wait 15m", n.What)
	assert.Equal(t, []string{"friend set amy --width 2", "wait 15m"}, n.Decisions)
	cmds := h.commandsOf(sprint.NOverloaded)
	require.Len(t, cmds, 2)
	assert.Equal(t, []string{"nova-config friend set amy --width 2", "nova-sprint friend sync"}, cmds[0].Lines)
	assert.Equal(t, "nova-sprint wait "+n.ID+" --for 15m", cmds[1].Lines[0])

	h.machine()
	assert.Len(t, h.openOf(sprint.NOverloaded), 1, "raised once while it holds")
	assert.Equal(t, 1, h.written(sprint.NOverloaded))

	// the window moves past the first timeout: two remain, and the judgment closes
	h.tick(sprint.OverloadWindow - time.Minute)
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)
	h.machine()
	assert.Empty(t, h.openOf(sprint.NOverloaded), "the window holds two")

	o, ok := sprint.FriendOverloaded(h.snap(), sprint.FriendSeat{Name: "amy", Width: 4, Status: sprint.Up})
	assert.False(t, ok, "%+v", o)
	assert.Len(t, sprint.MemberTimeouts(h.snap(), sprint.FriendRow("amy")), 2)
	h.clean("friend overloaded, then not")
}
