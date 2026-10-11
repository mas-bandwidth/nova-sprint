package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The machine's fault hold (HeldByFault) clears itself on the tick once the
// member is healthy: it beats, and it has finished a work card cleanly at or
// after the hold. The hold is placed by the machine itself, in the presence
// part, when a member stops beating (a down member is the machine's fault
// hold, held_by=fault with the stamp fault_since); the tick lifts the mark on
// a clean finish and tells the seat (NHoldCleared). A hold the coordinator
// made carries no held_by mark and is never lifted by the machine
// (docs/SPEC-SPRINT.md section 5; tla/DealFill.tla FaultHoldClears).

// freshBeats is every member beating at now: the machines are alive.
func freshBeats(w *world, members ...string) map[string]Beat {
	beats := map[string]Beat{}
	for _, m := range members {
		beats[m] = Beat{At: w.s.Now}
	}
	return beats
}

func TestAMachineFaultHoldClearsOnACleanFinish(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)

	// m1 stops beating: the machine's presence takes it down and holds it for
	// the fault, the machine's own hold, with the stamp the clear measures from.
	w.tick(time.Second)
	down, _ := presence(w.s, TickReq{Beats: map[string]Beat{"m2": {At: w.s.Now}}})
	w.must(down)
	ctl := w.s.MemberCtl("m1")
	require.True(t, FaultHeld(ctl), "the machine did not hold m1 for its down: %v", ctl.Fields)
	require.NotEmpty(t, ctl.F(FieldFaultSince), "no fault stamp: %v", ctl.Fields)
	require.Equal(t, Down, ctl.F("status"), "the member is not down: %v", ctl.Fields)

	// m1 beats again: presence brings it up to deal it a probe, and the fault
	// mark and its stamp stay until a clean finish proves it healthy.
	w.tick(time.Second)
	up, _ := presence(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	w.must(up)
	ctl = w.s.MemberCtl("m1")
	require.Equal(t, Up, ctl.F("status"), "the member is not up: %v", ctl.Fields)
	require.True(t, FaultHeld(ctl), "the fault mark was lost before the clean finish: %v", ctl.Fields)

	// the probe: m1 finishes a card cleanly, after the hold.
	w.s.Fleet.Put(&Card{ID: "m1-ok-1", Row: "m1", Col: DoneOK, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-1", "finished": stamp(w.s.Now.Add(time.Minute))}})

	// the next tick lifts the machine's own hold and says so to the seat.
	clear := faultClear(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	w.must(clear)
	require.False(t, FaultHeld(w.s.MemberCtl("m1")), "the fault hold did not clear: %v", w.s.MemberCtl("m1").Fields)
	require.Len(t, w.notesOf(NHoldCleared), 1, "the clear is not pushed to the seat")
}

// A beating fault-held member with no clean finish yet keeps its fault mark:
// presence brings it up for the probe, and the mark stays until the clean
// finish clears it.
func TestAFaultHeldMemberThatBeatsIsReleasedForAProbe(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.tick(time.Second)
	down, _ := presence(w.s, TickReq{Beats: map[string]Beat{"m2": {At: w.s.Now}}})
	w.must(down)

	w.tick(time.Second)
	up, _ := presence(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	w.must(up)
	ctl := w.s.MemberCtl("m1")
	require.Equal(t, Up, ctl.F("status"), "not up for the probe: %v", ctl.Fields)
	require.True(t, FaultHeld(ctl), "the fault mark was lost before the clean finish: %v", ctl.Fields)
}

// A hold the coordinator made carries no held_by mark and is never lifted by
// the tick, however healthy the member looks.
func TestACoordinatorsHoldIsNotClearedByTheTick(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m2", Who: "coordinator"}))
	ctl := w.s.MemberCtl("m2")
	require.Empty(t, ctl.F(FieldHeldBy), "the coordinator's hold carries no mark: %v", ctl.Fields)

	// a clean finish after the hold, and a fresh beat: still the coordinator's
	w.s.Fleet.Put(&Card{ID: "m2-ok-1", Row: "m2", Col: DoneOK, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-1", "finished": stamp(w.s.Now.Add(time.Minute))}})

	p := faultClear(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.True(t, p.Empty(), "the tick cleared a person's hold: %+v", p)
	require.NotEmpty(t, w.s.MemberCtl("m2").F("held"), "the coordinator's hold was lifted")
}
