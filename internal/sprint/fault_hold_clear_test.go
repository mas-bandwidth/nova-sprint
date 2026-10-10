package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The machine's fault hold (HeldByFault) clears itself on the tick once the
// member is healthy: it beats, and it has finished a work card cleanly at or
// after the hold. A coordinator's hold carries no held_by mark and is never
// lifted by the tick (docs/SPEC-SPRINT.md section 5; tla/DealFill.tla
// FaultHoldClears). The model is a two-step probe: the tick releases a
// beating fault-held member so the deal gives it a card, then clears the
// whole hold on the clean finish.

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
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	require.Equal(t, Working, w.s.Fleet.Card(wc.ID).Col)

	// the machine holds the member for an exit fault, letting its working card finish
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: wc.Row, HeldBy: HeldByFault, Finish: true, Who: "machine"}))
	require.True(t, FaultHeld(w.s.MemberCtl(wc.Row)), "held by the machine for a fault: %v", w.s.MemberCtl(wc.Row).Fields)
	require.NotEmpty(t, w.s.MemberCtl(wc.Row).F(FieldFaultSince), "the fault hold carries its stamp: %v", w.s.MemberCtl(wc.Row).Fields)

	// the member finishes the card cleanly, after the hold
	w.tick(time.Second)
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))

	// the next tick lifts the machine's own hold and says so to the seat
	p := faultClear(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	w.must(p)
	require.False(t, FaultHeld(w.s.MemberCtl(wc.Row)), "the fault hold did not clear: %v", w.s.MemberCtl(wc.Row).Fields)
	require.Equal(t, Up, w.s.MemberCtl(wc.Row).F("status"), "the member is not up: %v", w.s.MemberCtl(wc.Row).Fields)
	require.Len(t, w.notesOf(NHoldCleared), 1, "the clear is not pushed to the seat")
}

// A beating fault-held member with no clean finish yet is released for the
// probe: its down comes off so the deal gives it a card, and the fault mark
// stays until the clean finish.
func TestAFaultHeldMemberThatBeatsIsReleasedForAProbe(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", HeldBy: HeldByFault, Finish: true, Who: "machine"}))

	p := faultClear(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	w.must(p)
	ctl := w.s.MemberCtl("m1")
	require.Equal(t, "", ctl.F("held"), "the down was not released for the probe: %v", ctl.Fields)
	require.True(t, FaultHeld(ctl), "the fault mark was lost before the clean finish: %v", ctl.Fields)
	require.Equal(t, Up, ctl.F("status"), "not up for the probe: %v", ctl.Fields)
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
	require.True(t, faultCleanFinish(w.s, "m2", ctl.F("held")), "the seeded clean finish was not seen")

	p := faultClear(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.True(t, p.Empty(), "the tick cleared a person's hold: %+v", p)
	require.NotEmpty(t, w.s.MemberCtl("m2").F("held"), "the coordinator's hold was lifted")
}
