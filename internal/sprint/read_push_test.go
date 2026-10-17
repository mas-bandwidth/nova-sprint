package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A finish and the reader part of its awakened tick use the same injected
// instant. A second finished card waits only until its reader has room.
func TestAFinishAsksAFreeReaderInTheSameTick(t *testing.T) {
	t.Run("free reader", func(t *testing.T) {
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		work := w.s.Fleet.Card(w.s.Work.Card("s1-1").F("work"))
		w.must(Take(w.s, TakeReq{As: work.Row, Sel: Sel{IDs: []string{work.ID}}, Gens: gensOf(w.s, work.ID)}))
		finishedAt := w.s.Now
		w.must(Finish(w.s, FinishReq{As: work.Row, Sel: Sel{IDs: []string{work.ID}}, Gens: gensOf(w.s, work.ID), Head: "head-1"}))
		plan, due := TickAsk(w.s, TickReq{})
		w.must(plan)
		require.Zero(t, due)
		reads := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
		require.Len(t, reads, 1)
		assert.Equal(t, "head-1", reads[0].F("head"))
		assert.Equal(t, stamp(finishedAt), reads[0].F("asked"))
		assert.True(t, w.s.Now.Equal(finishedAt), "ask ran without advancing the fake clock")
	})

	t.Run("first free reader", func(t *testing.T) {
		w := newWorld(t, "reader-m1")
		w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 1}))
		w.must(Add(w.s, AddReq{Brief: "tier: flash", Stream: "s1", Count: 2}))
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1", "s1-2"}}}))
		finish := func(id string) {
			t.Helper()
			work := w.s.Fleet.Card(w.s.Work.Card(id).F("work"))
			w.must(Take(w.s, TakeReq{As: work.Row, Sel: Sel{IDs: []string{work.ID}}, Gens: gensOf(w.s, work.ID)}))
			w.must(Finish(w.s, FinishReq{As: work.Row, Sel: Sel{IDs: []string{work.ID}}, Gens: gensOf(w.s, work.ID), Head: id + "-head"}))
		}
		finish("s1-1")
		plan, due := TickAsk(w.s, TickReq{})
		w.must(plan)
		require.Zero(t, due)
		first := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
		require.Len(t, first, 1)
		finish("s1-2")
		_, due = TickAsk(w.s, TickReq{})
		require.Equal(t, 1, due, "the reader is at width")
		require.Empty(t, readsAt(w.s, w.s.Work.Card("s1-2"), 1))
		w.must(Read(w.s, ReadReq{As: "reader-m1", Verdict: "ok", Sel: Sel{IDs: []string{first[0].ID}}}))
		w.tick(time.Second)
		plan, due = TickAsk(w.s, TickReq{})
		w.must(plan)
		require.Zero(t, due)
		second := readsAt(w.s, w.s.Work.Card("s1-2"), 1)
		require.Len(t, second, 1)
		assert.Equal(t, stamp(w.s.Now), second[0].F("asked"))
	})
}
