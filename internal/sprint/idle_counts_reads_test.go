package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A friend holding a working card is idle only when she closed nothing in the window: no
// work finish and no read (a verdict on the reads ledger, or a read card she closed with a
// verdict or a return). Time is the snapshot's clock, set by the test.
func TestTheIdleAlarmCountsReadsAsFinishes(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	row := FriendRow("johnny")
	mk := func(t *testing.T) *world {
		w := setup(t, 0)
		w.s.Now = start
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
		w.s.Fleet.Put(&Card{ID: "s1-soak.w1", Row: row, Col: Working, Rev: 1,
			Fields: map[string]string{"kind": "work", "taken": stamp(start)}})
		w.s.Now = start.Add(2 * w.s.FriendFinishAfter())
		return w
	}
	idle := func(w *world) bool {
		for _, c := range idleConds(w.s, TickReq{}) {
			if c.typ == NFriendIdle {
				return true
			}
		}
		return false
	}
	recent := func(w *world) time.Time { return w.s.Now.Add(-time.Minute) }

	t.Run("nothing", func(t *testing.T) {
		t.Parallel()
		assert.True(t, idle(mk(t)), "no work finish and no read: the alarm holds")
	})
	t.Run("work finish", func(t *testing.T) {
		t.Parallel()
		w := mk(t)
		w.s.Fleet.Put(&Card{ID: "s1-other.w1", Row: row, Col: DoneOK, Rev: 2,
			Fields: map[string]string{"kind": "work", "finished": stamp(recent(w))}})
		assert.False(t, idle(w), "a work finish in the window: no alarm")
	})
	t.Run("ledger verdicts only", func(t *testing.T) {
		t.Parallel()
		w := mk(t)
		w.s.Fleet.SetProp(PropReadsWindow, ReadVerdict{At: recent(w), Reader: row, Verdict: "ok", Tier: "pro"}.line()+"\n"+
			ReadVerdict{At: recent(w), Reader: row, Verdict: "broken", Tier: "pro", Finding: "x.go:1"}.line())
		assert.False(t, idle(w), "verdicts filed in the window: no alarm")
	})
	t.Run("retired read cards only", func(t *testing.T) {
		t.Parallel()
		for _, by := range []string{"read", RetiredByReturned} {
			w := mk(t)
			w.s.Fleet.Put(&Card{ID: "s1-x.r2.johnny", Rev: 2, Fields: map[string]string{
				"kind": "read", "reader": "johnny", "retired": stamp(recent(w)), "retired_by": by}})
			assert.False(t, idle(w), "a read closed by %s in the window: no alarm", by)
		}
	})
	t.Run("old reads", func(t *testing.T) {
		t.Parallel()
		w := mk(t)
		w.s.Fleet.SetProp(PropReadsWindow, ReadVerdict{At: start.Add(-time.Hour), Reader: row, Verdict: "ok", Tier: "pro"}.line())
		assert.True(t, idle(w), "a read before the window does not count")
	})
	t.Run("another reader's verdicts", func(t *testing.T) {
		t.Parallel()
		w := mk(t)
		w.s.Fleet.SetProp(PropReadsWindow, ReadVerdict{At: recent(w), Reader: FriendRow("emma"), Verdict: "ok", Tier: "pro"}.line()+"\n"+
			ReadVerdict{At: recent(w), Reader: "m1", Verdict: "ok", Tier: "pro"}.line())
		assert.True(t, idle(w), "someone else's reads do not count for him")
	})
}
