package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A coordinator's hold with a reason stays held until it is unheld (2026-10-24: space,
// studio, hulk and captain, machines that never run a member loop by design, flipped held ->
// down every few minutes and raised "a fleet member is down and the coordinator did not hold
// it" and "a member is held with no cause"). Every time is passed in (w.tick); no beat is
// read but the ones the test gives.

// holdByCoordinator is the coordinator's fleet hold of m with a reason, as fleet hold
// --reason writes it, and the status presence derives for it.
func holdByCoordinator(t *testing.T, w *world, m, reason string) {
	t.Helper()
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: m, Who: "rowan", Reason: reason}))
	require.Equal(t, reason, w.s.MemberCtl(m).F(FieldHeldReason), "the hold carries no reason: %v", w.s.MemberCtl(m).Fields)
	w.s.MemberCtl(m).Fields["status"] = Held // presence's derived status of a held member
}

// Rule 1: the fleet sync does not hold again a member the coordinator holds, so its status
// never flips held -> down and its reason stays, sync after sync.
func TestAReasonedHoldStaysHeldThroughTheSync(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	holdByCoordinator(t, w, "m2", "reserved machine: no member loop by design")
	sync := FleetReq{Op: "sync", Who: "sync", Sync: []SyncMember{{Name: "m1", Width: DefaultWidth}}, Machines: []string{"m1", "m2"}}
	for _, d := range FleetDrift(w.s, sync.Sync, sync.Machines) {
		assert.NotEqual(t, "m2", d.Member, "the sync drifts a held member: %+v", d)
	}
	for range 3 {
		w.tick(5 * time.Minute)
		w.must(FleetStep(w.s, sync))
		ctl := w.s.MemberCtl("m2")
		require.NotEmpty(t, ctl.F("held"), "the sync ended the hold: %v", ctl.Fields)
		assert.Equal(t, Held, ctl.F("status"), "the sync flipped the held member down: %v", ctl.Fields)
		assert.Equal(t, "reserved machine: no member loop by design", ctl.F(FieldHeldReason), "the sync wiped the reason: %v", ctl.Fields)
	}
}

// Rule 2: a held member that never beats raises no down judgment, and its hold's cause
// ending does not release it down: it stays held until unheld.
func TestAHeldMemberThatNeverBeatsRaisesNoDownJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	id := seedJudgment(w)
	w.must(HoldNames(w.s, HoldReq{Names: []string{"m2"}, Reason: "Windows-only machine, no member loop", Cause: "j100", CauseNote: id, Who: "rowan"}))
	w.s.MemberCtl("m2").Fields["status"] = Held
	w.closeAll(w.openOn(SprintSubject)) // the cause is answered
	beats := TickReq{Beats: freshBeats(w, "m1")}
	for range 4 {
		w.tick(StopMemberAfter + time.Minute)
		w.part(TickDeal, beats)
		ctl := w.s.MemberCtl("m2")
		require.NotEmpty(t, ctl.F("held"), "the cause's end released a member that does not beat: %v", ctl.Fields)
		assert.Empty(t, memberStops(w.s, beats), "a held member is a down stop")
	}
	assert.Empty(t, w.notesOf(NHoldReleased), "a reasoned hold was released while its member does not beat")
	assert.Empty(t, w.notesOf(NStopMemberDown), "a held member raised a down judgment")

	// unhold ends it
	w.must(HoldNames(w.s, HoldReq{Names: []string{"m2"}, Release: true, Reason: "back in service", Who: "rowan"}))
	assert.Empty(t, w.s.MemberCtl("m2").F("held"), "unhold did not end it")
}

// Rule 3: a hold's reason counts as its cause: no "held with no cause" judgment, however
// long it stands.
func TestAHoldsReasonCountsAsItsCause(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	holdByCoordinator(t, w, "m2", "production game server, XDP-only")
	for range 3 {
		w.tick(HoldNoCauseEvery + time.Minute)
		require.Empty(t, holdNoCauseConds(w.s, TickReq{}), "a reasoned hold is a hold with no cause")
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1")})
	}
	assert.Empty(t, w.notesOf(NHoldNoCause), "a reasoned hold raised a no-cause judgment")
}
