package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Idle-reap attribution and the cause-less hold's report (the owner, 2026-10-13: mini-m5,
// the fastest machine, held about ten hours on "three idle-reaps in 15 minutes, each with
// no model call" when the providers were at fault fleet-wide; tla/HoldCause.tla). These
// tests read only the tick's own words (the fields and note types as strings), so they
// compile at the base and fail there on behaviour. Every time is passed in: the world's
// clock moves by w.tick, never by the wall.

// idleReapTake is a take that ended with no result and no model call (no usage at all) on
// route, by member, at the minute m of the window.
func idleReapTake(route, member string, at time.Time) string {
	return ProviderTake{Route: route, Member: member, Finished: stamp(at), Error: "no result: the child was idle for 300s and wrote no RESULT.md"}.String()
}

// reapWorld is the setup world at epoch with one enabled route, ds, of tier pro.
func reapWorld(t *testing.T, epoch uint64) *world {
	w := setup(t, 1)
	w.s.Epoch = epoch
	w.s.Routes = []Route{{Name: "ds", Tier: "pro", Provider: "deepseek", Model: "deepseek-v4", Enabled: true}}
	return w
}

// putReap puts a withdrawn work card on member's row carrying one idle-reap take on route.
func putReap(w *world, id, route, member string, at time.Time) {
	w.s.Fleet.Put(&Card{ID: id, Row: member, Col: Withdrawn, Score: 1, Rev: 1, Fields: map[string]string{
		"kind": "work", FieldRoute: route, FieldProviderTake + "1": idleReapTake(route, member, at)}})
}

// putOK puts a work card member finished ok on route at at.
func putOK(w *world, id, route, member string, at time.Time) {
	w.s.Fleet.Put(&Card{ID: id, Row: member, Col: DoneOK, Score: 1, Rev: 1, Fields: map[string]string{
		"kind": "work", FieldRoute: route, "ok": "yes", "finished": stamp(at)}})
}

// dReapsOnM1 puts m1's three idle reaps on ds, at -9, -6 and -3 minutes.
func dReapsOnM1(w *world) {
	for i, ago := range []time.Duration{9, 6, 3} {
		putReap(w, "x"+itoa(i+1)+".w1", "ds", "m1", w.s.Now.Add(-ago*time.Minute))
	}
}

func forEpochs(t *testing.T, run func(t *testing.T, epoch uint64)) {
	for _, epoch := range []uint64{0, 3} {
		t.Run("epoch"+itoa(int(epoch)), func(t *testing.T) {
			t.Parallel()
			run(t, epoch)
		})
	}
}

// The same idle reap on two members on one route is the route's fault: the route rests
// (the route-rest path, its provider's note to the seat) and no member is held.
func TestAnIdleReapOnTwoMembersRestsTheRouteAndHoldsNoMember(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := reapWorld(t, epoch)
		now := w.s.Now
		putReap(w, "x1.w1", "ds", "m1", now.Add(-4*time.Minute))
		putReap(w, "x2.w1", "ds", "m2", now.Add(-3*time.Minute))
		putReap(w, "x3.w1", "ds", "m1", now.Add(-2*time.Minute))

		due := RestsDue(w.s)
		require.Len(t, due, 1, "two members reaping on one route rest it: %+v", due)
		assert.Equal(t, "ds", due[0].Route)
		assert.Equal(t, "no-model-call", due[0].Cause)
		assert.ElementsMatch(t, []string{"x1.w1", "x2.w1", "x3.w1"}, due[0].Cards)

		p := w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		for _, m := range []string{"m1", "m2"} {
			assert.Empty(t, w.s.MemberCtl(m).F("held"), "%s was held for the route's fault: %v", m, w.s.MemberCtl(m).Fields)
		}
		rest, ok := RouteRests(w.s.Routes, w.s.Fleet)["ds"]
		require.True(t, ok, "the route's rest was not written: %+v", p.Props)
		assert.True(t, rest.Resting(w.s.Now), "the route does not rest: %+v", rest)
		said := false
		for _, n := range w.notesOf(NRouteRested) {
			said = said || (n.To == w.s.Coordinator && strings.Contains(n.What, "m1") && strings.Contains(n.What, "m2"))
		}
		assert.True(t, said, "the seat was not told the route rests for two members' reaps: %+v", w.notesOf(NRouteRested))
	})
}

// Reversed: one member idle-reaps three times on a route that serves another member in
// the same window: the member's fault, and it is held with its cause set (the judgment it
// raises); answering that judgment ends the hold in the next tick.
func TestAnIdleReapOnOneMemberOfAServingRouteHoldsItWithItsCause(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := reapWorld(t, epoch)
		dReapsOnM1(w)
		putOK(w, "y1.w1", "ds", "m2", w.s.Now.Add(-5*time.Minute))

		assert.Empty(t, RestsDue(w.s), "one member's reaps on a route that serves rest nothing")
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl := w.s.MemberCtl("m1")
		require.NotEmpty(t, ctl.F("held"), "m1 reaped alone on a serving route and is not held: %v", ctl.Fields)
		assert.Equal(t, "idle-reap", ctl.F("held_cause"), "the hold carries no cause: %v", ctl.Fields)
		assert.Equal(t, "idle-reap", ctl.F("held_by"), "the hold is not the machine's: %v", ctl.Fields)
		assert.Empty(t, w.s.MemberCtl("m2").F("held"), "m2 served the route and was held")
		judged := w.openOn(StreamSubject(MemberSubject("m1")))
		require.Len(t, judged, 1, "no judgment is the hold's cause: %+v", w.s.Open)

		// the seat answers it: the next tick ends the hold, says so, and m1 is up
		w.closeAll(judged)
		w.tick(time.Second)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		ctl = w.s.MemberCtl("m1")
		assert.Empty(t, ctl.F("held"), "the hold outlived its cause: %v", ctl.Fields)
		assert.Equal(t, Up, ctl.F("status"), "m1 beats and is not up: %v", ctl.Fields)

		// the reaps before the hold never hold it again
		w.tick(time.Second)
		w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
		assert.Empty(t, w.s.MemberCtl("m1").F("held"), "the same reaps held m1 again")
	})
}

// A member that reaps on a route nothing else serves in the window is not held: no
// evidence the route serves says the fault is the member's. A dead provider is the likelier
// cause, so the route rests, and never silently: the seat is told the route, the count and
// the member, and that the member is not held (the coordinator's ruling on #206; a route
// only one member used would otherwise never rest and say nothing). An ok end before the
// first reap says nothing of the route since (the cold read of #206, finding 2, the
// reader's b1 probe at epoch 3).
func TestAnIdleReapOnARouteNoOneServesSinceHoldsNoMember(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		epoch    uint64
		okBefore bool
	}{
		{"AnIdleReapOnARouteThatServesNoOneHoldsNoMember", 1, false},
		{"AnOkEndBeforeTheFirstReapHoldsNoMember", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := reapWorld(t, tc.epoch)
			if tc.okBefore {
				putOK(w, "y1.w1", "ds", "m2", w.s.Now.Add(-14*time.Minute))
			}
			dReapsOnM1(w)
			w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
			assert.Empty(t, w.s.MemberCtl("m1").F("held"), "m1 was held with no route serving since its first reap: %v", w.s.MemberCtl("m1").Fields)
			rest, ok := RouteRests(w.s.Routes, w.s.Fleet)["ds"]
			require.True(t, ok, "the dead route did not rest")
			assert.Equal(t, RestNoModelCall, rest.Cause)
			notes := restNoteOn(w, "ds")
			require.Len(t, notes, 1, "the seat was not told: %+v", w.notesOf(NRouteRested))
			assert.Contains(t, notes[0].What, "route ds: 3 no-model-call ends, all on m1; route rested, m1 not held")
		})
	}
}

// A hold the coordinator made with no cause is reported to the seat every two hours,
// never silently: one judgment a period, the old one closed as the new one opens.
func TestACauseLessHoldIsReportedEveryTwoHours(t *testing.T) {
	t.Parallel()
	forEpochs(t, func(t *testing.T, epoch uint64) {
		w := setup(t, 1)
		w.s.Epoch = epoch
		w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", Who: "rowan", Reason: "looking at it", Finish: true}))
		const typ = "a member is held with no cause"
		beats := TickReq{Beats: freshBeats(w, "m2")}

		w.tick(2*time.Hour - time.Minute)
		w.part(TickDeal, beats)
		require.Empty(t, w.notesOf(typ), "reported before two hours")

		w.tick(2 * time.Minute)
		w.part(TickDeal, beats)
		require.Len(t, w.notesOf(typ), 1, "not reported at two hours")
		first := w.notesOf(typ)[0]
		assert.Contains(t, first.What, "m1 held 2h by rowan with no cause")
		assert.Contains(t, first.What, "nova-sprint unhold m1")

		w.tick(time.Hour)
		w.part(TickDeal, beats)
		assert.Len(t, w.notesOf(typ), 1, "reported again inside the same period")

		w.tick(time.Hour)
		w.part(TickDeal, beats)
		require.Len(t, w.notesOf(typ), 2, "not reported again at four hours")
		assert.Contains(t, w.notesOf(typ)[1].What, "m1 held 4h by rowan with no cause")
		open := 0
		for _, o := range w.s.Open {
			if o.Note.Type == typ {
				open++
			}
		}
		assert.Equal(t, 1, open, "the two-hour report stayed open beside the four-hour one")
	})
}
