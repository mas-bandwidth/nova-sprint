package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cold read of #206 (2026-10-13): each finding's sequence, at an epoch past 0, with
// time passed in (w.tick) and nothing real (tla/HoldCause.tla).

// restNoteOn is the route-rest notes to the seat that name the route.
func restNoteOn(w *world, route string) []Note {
	var out []Note
	for _, n := range w.notesOf(NRouteRested) {
		if n.To == w.s.Coordinator && strings.HasPrefix(n.What, "route "+route+":") {
			out = append(out, n)
		}
	}
	return out
}

// Finding 2's second half at epoch 3: m1 is held rightly (m2 served ds after m1's first
// reap); the next tick m2 reaps too, ds rests for no model call, and m1's hold, placed for
// ds, ends in that same tick: the route's fault after all (NoHoldForARestedRoute).
func TestARestForNoModelCallEndsTheIdleReapHoldOnItInTheSameTick(t *testing.T) {
	t.Parallel()
	w := reapWorld(t, 3)
	dReapsOnM1(w)
	putOK(w, "y1.w1", "ds", "m2", w.s.Now.Add(-8*time.Minute))
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.Equal(t, CauseIdleReap, w.s.MemberCtl("m1").F(FieldHeldCause), "m1 is not held for its reaps: %v", w.s.MemberCtl("m1").Fields)
	require.Equal(t, "ds", w.s.MemberCtl("m1").F(FieldIdleReapRoutes))

	w.tick(time.Minute)
	putReap(w, "x4.w1", "ds", "m2", w.s.Now)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	rest, ok := RouteRests(w.s.Routes, w.s.Fleet)["ds"]
	require.True(t, ok, "ds did not rest for two members' reaps")
	assert.Equal(t, RestNoModelCall, rest.Cause)
	ctl := w.s.MemberCtl("m1")
	assert.Empty(t, ctl.F("held"), "m1 stayed held while ds rests for the route's fault: %v", ctl.Fields)
	assert.Equal(t, Up, ctl.F("status"))
	released := w.notesOf(NHoldReleased)
	require.Len(t, released, 1)
	assert.Contains(t, released[0].What, "route ds rests for no model call")
	assert.Empty(t, w.openOn(StreamSubject(MemberSubject("m1"))), "the idle-reap judgment stayed open")
}

// Finding 1 at epoch 3 to 4: a clear carries the idle-reap hold with its cause; the
// judgment that was its cause is retired by the clear, so the first tick of the new epoch
// ends the hold and says so. Nothing is left held with nothing to end it.
func TestAClearCarriesTheCauseAndTheNewEpochEndsTheHold(t *testing.T) {
	t.Parallel()
	w := reapWorld(t, 3)
	dReapsOnM1(w)
	putOK(w, "y1.w1", "ds", "m2", w.s.Now.Add(-8*time.Minute))
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.Equal(t, HeldByIdleReap, w.s.MemberCtl("m1").F(FieldHeldBy))

	sh := ShapeOf(w.s)
	n := newWorld(t)
	n.s.Epoch, n.s.Now = 4, w.s.Now.Add(time.Minute)
	n.s.Fleet.SetRows(sh.Members)
	n.must(RestoreShape(n.s, sh))
	ctl := n.s.MemberCtl("m1")
	require.NotEmpty(t, ctl.F("held"), "the clear dropped the hold: %v", ctl.Fields)
	assert.Equal(t, CauseIdleReap, ctl.F(FieldHeldCause), "the clear dropped the cause: %v", ctl.Fields)
	assert.Equal(t, "ds", ctl.F(FieldIdleReapRoutes))

	n.part(TickDeal, TickReq{Beats: freshBeats(n, "m1", "m2")})
	assert.Empty(t, n.s.MemberCtl("m1").F("held"), "the carried hold outlived its retired judgment: %v", n.s.MemberCtl("m1").Fields)
	released := n.notesOf(NHoldReleased)
	require.Len(t, released, 1)
	assert.Contains(t, released[0].What, "m1 released: cause idle-reap resolved")
}

// Finding 1(b) at epoch 2: a hold with no cause is reported whoever made it; a mark (the
// sync's here) is no cause.
func TestAHoldWithAMarkAndNoCauseIsReportedToo(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Epoch = 2
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", Who: "sync", HeldBy: HeldBySync}))
	require.Equal(t, HeldBySync, w.s.MemberCtl("m1").F(FieldHeldBy))
	w.tick(2*time.Hour + time.Minute)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m2")})
	notes := w.notesOf(NHoldNoCause)
	require.Len(t, notes, 1, "a marked hold with no cause went unreported")
	assert.Contains(t, notes[0].What, "m1 held 2h by the machine (sync) with no cause")
}

// A wait parks a judgment, it never resolves it (the coordinator's ruling on #206): a hold
// whose cause is waited stays held through the wait and when the tick raises the judgment
// again; answering it ends the hold.
func TestAWaitedCauseKeepsTheHold(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := setup(t, 1)
		w.s.Epoch = epoch
		j := judgment(NAlarmFleet, "", w.s.Now, 0)
		j.SprintLevel, j.What = true, "the fleet backlog alarm"
		w.note(j)
		id := w.notes[len(w.notes)-1].ID
		w.must(HoldNames(w.s, HoldReq{Names: []string{"m1"}, Reason: "the alarm", Cause: "j7", CauseNote: id, Who: "coordinator"}))
		require.Equal(t, NAlarmFleet, w.s.MemberCtl("m1").F(FieldHeldCauseType))

		// the seat waits it for 30m: the judgment leaves Open for an acknowledgement with a
		// review time, which the world keeps as the store does
		p := w.must(Wait(w.s, WaitReq{Note: id, Until: w.s.Now.Add(30 * time.Minute), Who: "coordinator"}))
		for _, u := range p.Units {
			for _, n := range u.Notes {
				if n.Kind == Acknowledged {
					n.ID = "ack-" + id
					for _, sub := range n.Subjects() {
						w.s.Acked = append(w.s.Acked, Open{Key: OpenKey(n.ID, sub), Note: n})
					}
				}
			}
		}
		require.False(t, judgmentOpen(w.s, id), "the wait left the judgment open: the test proves nothing")
		w.tick(time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "a waited cause released the hold")

		// the wait runs out and the tick raises the judgment again, a new note
		w.s.Acked = nil
		again := judgment(NAlarmFleet, "", w.s.Now, 0)
		again.SprintLevel, again.What = true, "the fleet backlog alarm"
		w.note(again)
		w.tick(time.Minute)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "the raised-again judgment did not keep the hold")

		// the seat answers it: the hold ends in the next tick
		w.closeAll(w.openOn(SprintSubject))
		w.tick(time.Second)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		assert.Empty(t, w.s.MemberCtl("m1").F("held"), "the answered cause kept the hold")
		require.Len(t, w.notesOf(NHoldReleased), 1)
	})
}
