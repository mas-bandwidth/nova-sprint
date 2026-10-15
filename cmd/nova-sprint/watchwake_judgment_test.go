package main

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/stretchr/testify/assert"
)

// The wake verb wakes on a judgment pushed over the bus, within one bus read, with
// the store look kept as the backstop. The model is tla/CoordinatorWake.tla
// (ArriveJudgment, ArriveMachine, ArriveSelf; EveryJudgmentWakes, SelfNeverWakes).
// The world is faked: the clock moves only when the verb sleeps, and no socket opens.

func TestWatchWakeWakesOnJudgmentBusMessages(t *testing.T) {
	t.Parallel()

	t.Run("a judgment the machine sent under the coordinator's name wakes at the next read", func(t *testing.T) {
		t.Parallel()
		for _, m := range []struct{ kind, subject string }{
			{bus.KindJudgment, "FRIEND DOWN bob: no answer in 20m"},
			{bus.KindJudgment, "FRIEND BACK bob"},
			{bus.KindJudgment, "nova-sprint: bob friend_down: she has stopped"},
			{bus.KindStatus, "FRIEND STATE bob proven->unhooked at=2026-10-15T14:00:00Z"},
			{bus.KindBlocker, "STREAM STOPPED s1 cause=red waiting=2 at=2026-10-15T14:00:00Z"},
		} {
			r := newWakeRig(t)
			start := r.now
			w := r.run(func(n int) {
				if n == 1 {
					r.f.send(r.now, "coord", m.subject, m.kind)
				}
			})
			assert.Equal(t, "bus", w.Kind, m.subject)
			assert.Contains(t, w.Evidence, m.subject)
			assert.Equal(t, r.cfg.every, r.elapsed(start), "one read after the event: %s", m.subject)
			assert.Less(t, r.elapsed(start), r.cfg.judgmentEvery, "the clock never reached the store's cadence")
		}
	})

	t.Run("a judgment from a friend wakes; a friend's status does not", func(t *testing.T) {
		t.Parallel()
		r := newWakeRig(t)
		start := r.now
		w := r.run(func(n int) {
			switch n {
			case 1:
				r.f.send(r.now, "friend", "chatter", bus.KindStatus)
			case 2:
				r.f.send(r.now, "friend", "bob is wedged on s1-2", bus.KindJudgment)
			}
		})
		assert.Equal(t, "bus", w.Kind)
		assert.Contains(t, w.Evidence, "bob is wedged")
		assert.NotContains(t, w.Evidence, "chatter")
		assert.Equal(t, 2*r.cfg.every, r.elapsed(start))
	})

	t.Run("what the seat sent itself under the coordinator's name never wakes", func(t *testing.T) {
		t.Parallel()
		r := newWakeRig(t)
		r.cfg.check = time.Hour
		start := r.now
		w := r.run(func(n int) {
			if n == 1 {
				// the kind alone does not make a message the machine's: its subject must
				r.f.send(r.now, "coord", "my own note to myself", bus.KindJudgment)
				r.f.send(r.now, "coord", "a status I set", bus.KindStatus)
				r.f.send(r.now, "coord", "a request I made", bus.KindRequest)
				r.f.send(r.now, "coord", "FRIEND STATE written by hand", bus.KindBlocker)
			}
		})
		assert.Equal(t, "check", w.Kind, "nothing the seat sent woke it")
		assert.GreaterOrEqual(t, r.elapsed(start), time.Hour)
	})

	t.Run("a pushed judgment dropped on the transport is caught once by the store look", func(t *testing.T) {
		t.Parallel()
		r := newWakeRig(t)
		r.cfg.check = 24 * time.Hour
		r.f.world.Judgments = []string{"old-1"}
		w := r.run(func(n int) {
			if n == 1 {
				r.f.world.Judgments = []string{"old-1", "j-2"}
			}
		})
		assert.Equal(t, "judgment", w.Kind)
		first := r.now

		// j-3 is raised and its bus push is lost: nothing is on the stream
		r.f.world.Judgments = []string{"old-1", "j-2", "j-3"}
		w = r.run(nil)
		assert.Equal(t, "judgment", w.Kind, "the backstop sweep caught it")
		assert.Contains(t, w.Evidence, "j-3")
		assert.NotContains(t, w.Evidence, "j-2")
		assert.GreaterOrEqual(t, r.elapsed(first), r.cfg.judgmentEvery, "at the backstop's cadence")

		// and only once
		r.cfg.check = 30 * time.Minute
		assert.Equal(t, "check", r.run(nil).Kind)
	})
}
