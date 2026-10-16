package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/hook"
)

// The sprint's friend states are three (the owner, 2026-10-14, v1.3.6: "there is only up,
// down, held from the sprint POV"; FriendStates): up and down come from the nova-hook record
// alone, and held is the coordinator's hold. A down friend is not dealt, and an up friend is,
// whatever a judgment of her work says.

// hookStates is every state the nova-hook record can give a friend, with no record at all
// (""), and a proven record that carries a down reason.
var hookStates = []hook.Status{
	{}, {State: hook.StateNone}, {State: hook.StateUnhooked}, {State: hook.StateExpired},
	{State: hook.StatePartial}, {State: hook.StateLive}, {State: hook.StateProven},
	{State: hook.StateProven, Down: "missed 2 challenges"},
}

// An up friend whose card runs long, with no finish for forty-five minutes (past the
// friend-finish window) and her session quiet, is still up and still dealt: the coordinator's pass reports the fact (NFriendIdle, a judgment that
// changes nothing), the fleet's parts take nothing back and write no state of hers, and a new
// card goes to her row. The retired stall ladder took her cards back and marked her down here
// (2026-10-14: Stella up on the hook record, "stalled" in the sprint, skipped by the deal).
func TestAnUpFriendWithNoRecentFinishesIsStillDealt(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"))
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 1, Status: FriendHookStatus(hook.Status{Name: "amy", State: hook.StateProven}, false), Class: "flash,pro"})
	require.Equal(t, Up, amy.Status)
	dealWith(w, amy)
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "s1-1 is dealt to amy")
	amy.Running = []string{"s1-1.w1"} // she takes it, and finishes nothing after
	dealWith(w, amy)
	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "she took s1-1")

	w.tick(45 * time.Minute)
	amy.Running = nil // her beat no longer names it: quiet, no finish, no session write
	r := TickReq{Friends: workerSeatFixtures([]FriendSeat{amy})}
	for _, tu := range TickTables {
		if tu.Table != Fleet {
			continue
		}
		for _, part := range tu.Parts {
			if part.Fn != nil {
				w.part(part.Fn, r)
			}
		}
	}
	pass, _ := TickCoordinatorPass(w.s, r)
	idle := false
	for _, n := range pass.Notes {
		idle = idle || n.Type == NFriendIdle
	}
	assert.True(t, idle, "the pass reports the fact: she holds working cards and finished none")
	w.must(pass)
	assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "nothing took her card back")
	assert.True(t, friendDealable(w.s, amy), "she is up on the hook record, so she is dealt")

	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-2", Brief: friendBrief("friend amy")}}}))
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Card("s1-2.w1"), "s1-2 is dealt")
	assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row, "an up friend with no recent finish is dealt")
}

// A friend whose hook record says anything but proven, or proven with a down reason, is down
// and dealt nothing, held or not; a held friend proven on the hook is held and dealt nothing;
// only a proven, unheld friend is up and dealt.
func TestAnUnhookedFriendIsNotDealt(t *testing.T) {
	t.Parallel()
	for _, h := range hookStates {
		for _, held := range []bool{false, true} {
			w := friendWorld(t, friendBrief("friend"), friendBrief("friend"))
			amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: FriendHookStatus(h, held), Class: "flash,pro", Held: held})
			dealWith(w, amy)
			n := w.s.Fleet.Count(FriendRow("amy"), Ready) + w.s.Fleet.Count(FriendRow("amy"), Working)
			if amy.Status == Up {
				assert.Positive(t, n, "hook %q held %v: up, and dealt", h.State, held)
				continue
			}
			assert.Zero(t, n, "hook %q held %v: %s, and dealt nothing", h.State, held, amy.Status)
		}
	}
	unhooked := FriendHookStatus(hook.Status{Name: "amy", State: hook.StateUnhooked, Reason: "missed 2 challenges"}, false)
	assert.Equal(t, Down, unhooked, "an unhooked friend is down")
}
