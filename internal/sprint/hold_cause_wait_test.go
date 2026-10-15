package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reader's P2 on #206 (Stella, 2026-10-13): the idle-reap judgment listed wait, and
// Wait refused it, as its type was not a condition the tick keeps. Now it is one
// (idleReapConds, TickDecisions): a wait parks it until its time, the hold standing, and
// the tick raises it again when the wait runs out; an ack ends the hold. Every answer a
// judgment lists is run against the state the judgment leaves (the friction 21 pattern).
// Every time is passed in (w.tick); the world keeps an acknowledgement as the store does.

// idleReapHeld is a world at epoch 3 where the tick held m1 for three reaps on ds, a route m2
// served after the first: the hold, and the judgment that is its cause.
func idleReapHeld(t *testing.T) (*world, Open) {
	w := reapWorld(t, 3)
	dReapsOnM1(w)
	putOK(w, "y1.w1", "ds", "m2", w.s.Now.Add(-5*time.Minute))
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.Equal(t, CauseIdleReap, w.s.MemberCtl("m1").F(FieldHeldCause), "m1 was not held for its reaps: %v", w.s.MemberCtl("m1").Fields)
	judged := w.openOn(StreamSubject(MemberSubject("m1")))
	require.Len(t, judged, 1, "no judgment is the hold's cause: %+v", w.s.Open)
	return w, judged[0]
}

// park applies a wait's plan as the store does: the judgment leaves Open, and the
// acknowledgement with its review time enters Acked.
func park(w *world, p Plan) {
	for _, u := range p.Units {
		for _, n := range u.Notes {
			if n.Kind == Acknowledged {
				n.ID = "ack-" + u.Key
				for _, sub := range n.Subjects() {
					w.s.Acked = append(w.s.Acked, Open{Key: OpenKey(n.ID, sub), Note: n})
				}
			}
		}
	}
}

func TestEveryAnswerTheIdleReapJudgmentListsWorksOnItsState(t *testing.T) {
	t.Parallel()
	_, j := idleReapHeld(t)
	require.ElementsMatch(t, []string{"ack", "wait"}, j.Note.Decisions)
	for _, d := range j.Note.Decisions {
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			w, j := idleReapHeld(t)
			var p Plan
			switch d {
			case "ack":
				p = Ack(w.s, AckReq{Notes: []string{j.Note.ID}, Reason: "its harness and keys seen to", Who: w.s.Coordinator})
			case "wait":
				p = Wait(w.s, WaitReq{Note: j.Note.ID, Until: w.s.Now.Add(30 * time.Minute), Who: w.s.Coordinator})
			default:
				t.Fatalf("the judgment lists %q, which no test runs", d)
			}
			require.Empty(t, p.Refused, "%s is listed and refused: %+v", d, p.Refused)
			require.Len(t, p.Units, 1, "%s answered nothing: %+v", d, p)
		})
	}
}

func TestAWaitParksTheIdleReapJudgmentAndTheTickRaisesItAgain(t *testing.T) {
	t.Parallel()
	w, j := idleReapHeld(t)
	subject := StreamSubject(MemberSubject("m1"))

	// the seat waits it for 30m: the judgment leaves Open, the hold stands, and the tick
	// writes no second judgment while the wait runs
	p := w.must(Wait(w.s, WaitReq{Note: j.Note.ID, Until: w.s.Now.Add(30 * time.Minute), Who: w.s.Coordinator}))
	park(w, p)
	require.Empty(t, w.openOn(subject), "the wait left the judgment open: the test proves nothing")
	require.Len(t, w.s.Acked, 1, "the wait recorded no acknowledgement with a review time")
	w.tick(time.Minute)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "a waited idle-reap judgment released the hold")
	require.Empty(t, w.openOn(subject), "the tick raised the judgment again while its wait runs")

	// the wait runs out: the tick closes it and raises the judgment again, its answers the
	// same, and the hold stands throughout
	w.tick(31 * time.Minute)
	p = w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	closed := false
	for _, o := range p.Closes {
		closed = closed || o.Key == w.s.Acked[0].Key
	}
	require.True(t, closed, "the run-out wait was not closed: %+v", p.Closes)
	w.s.Acked = nil // as the store applies the close
	again := w.openOn(subject)
	require.Len(t, again, 1, "the judgment was not raised again when its wait ran out: %+v", w.s.Open)
	assert.NotEqual(t, j.Note.ID, again[0].Note.ID, "the raised-again judgment is the old note")
	assert.ElementsMatch(t, []string{"ack", "wait"}, again[0].Note.Decisions)
	assert.Contains(t, again[0].Note.What, "m1 held: 3 takes ended with no model call")
	require.NotEmpty(t, w.s.MemberCtl("m1").F("held"), "the hold ended with the wait")

	// the tick keeps it, once, while it is open
	w.tick(time.Minute)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	require.Len(t, w.openOn(subject), 1, "a second judgment was written beside the open one")

	// the seat acks it: the next tick ends the hold and raises nothing in its place
	w.must(Ack(w.s, AckReq{Notes: []string{again[0].Note.ID}, Reason: "its harness and keys seen to", Who: w.s.Coordinator}))
	w.tick(time.Second)
	w.part(TickDeal, TickReq{Beats: freshBeats(w, "m1", "m2")})
	ctl := w.s.MemberCtl("m1")
	assert.Empty(t, ctl.F("held"), "the hold outlived its answered judgment: %v", ctl.Fields)
	assert.Empty(t, w.openOn(subject), "a judgment was raised for a hold that ended: %+v", w.s.Open)
	require.Len(t, w.notesOf(NHoldReleased), 1)
}
