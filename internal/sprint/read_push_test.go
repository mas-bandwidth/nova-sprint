package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A finished card is asked of a free reader with room in the finish's own
// step, never a tick later (docs/SPEC-SPRINT.md section 6,
// reads-start-on-finish-r-ns-b.w2; the wall-clock lens, the owner 2026-10-04:
// the time from add to landed, measured per stage, and the waits removed).
// The clock is the snapshot's, moved by hand: no test here sleeps.

// pushWorld is readers reader-m1 and reader-m2, up, named for members m1 and
// m2 of the widths given, and n flash primaries admitted on stream s1.
func pushWorld(t *testing.T, w1, w2, n int) *world {
	t.Helper()
	w := newWorld(t, "reader-m1", "reader-m2")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: w1}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: w2}))
	w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp, "reader-m2": ReaderUp}
	w.must(Add(w.s, AddReq{Stream: "s1", Count: n}))
	return w
}

// working deals and takes primary p's first work card, and returns its id.
func working(w *world, p string) string {
	w.t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{p}}}))
	wc := w.s.Fleet.Card(WorkCardID(p, 1))
	require.NotNil(w.t, wc, "%s was not dealt", p)
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	return wc.ID
}

// occupy fills the reader's lanes with reads of other work, reading.
func occupy(w *world, reader string, n int) {
	for i := range n {
		id := ReadCardID("x"+itoa(i), 1, reader)
		w.s.Readers.Put(&Card{ID: id, Row: reader, Col: Reading, Rev: 1, Fields: map[string]string{"kind": "read", "primary": "x" + itoa(i), "attempt": "1", "reader": reader}})
	}
	w.s.Readers.cells = nil
}

func TestAFinishAsksAFreeReaderInTheSameTick(t *testing.T) {
	t.Parallel()

	t.Run("a reader has room: the finish asks it", func(t *testing.T) {
		t.Parallel()
		w := pushWorld(t, 2, 2, 1)
		wc := working(w, "s1-1")
		w.tick(7 * time.Minute)
		finished := w.s.Now
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc), Head: "abc123"}))

		// no tick has run: the finish's own plan asked the read
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, Review, pr.Col)
		reads := readsAt(w.s, pr, 1)
		require.Len(t, reads, 1, "a flash card's one read is asked in the step of its finish")
		rc := reads[0]
		assert.Equal(t, Asked, rc.Col)
		assert.Equal(t, "abc123", rc.F("head"), "the read is of the head the finish reported")
		assert.Equal(t, stamp(finished), rc.F("asked"), "asked at the finish's own time")
		assert.Equal(t, rc.Row, pr.F("asked"), "the primary names its reader")
		// read_wait (cycletime.go: finished_at to the read's asked) is zero
		assert.Equal(t, pr.F(FieldFinishedAt), rc.F("asked"))
		assert.Equal(t, stamp(finished), readStamps([]*Card{rc})[FieldReadAskedAt])

		// the tick that follows has nothing left to ask
		p, due := TickAsk(w.s, TickReq{})
		assert.Empty(t, p.Units, "asked once, by the finish")
		assert.Zero(t, due)
	})

	t.Run("no reader has room: asked on the first free one", func(t *testing.T) {
		t.Parallel()
		w := pushWorld(t, 1, 1, 1)
		occupy(w, "reader-m1", 1)
		occupy(w, "reader-m2", 1)
		wc := working(w, "s1-1")
		w.tick(7 * time.Minute)
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc), Head: "abc123"}))
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, Review, pr.Col)
		assert.Empty(t, readsAt(w.s, pr, 1), "no reader has room: the finish asks none")
		p, due := TickAsk(w.s, TickReq{})
		assert.Empty(t, p.Units)
		assert.Equal(t, 1, due, "it waits for room, due, with no judgment")

		// reader-m2's lane frees: the first tick after asks it there
		w.tick(20 * time.Second)
		w.place(w.s.Readers, ReadCardID("x0", 1, "reader-m2"), "reader-m2", OK)
		w.part(TickAsk, TickReq{})
		reads := readsAt(w.s, pr, 1)
		require.Len(t, reads, 1)
		assert.Equal(t, "reader-m2", reads[0].Row)
		assert.Equal(t, stamp(w.s.Now), reads[0].F("asked"))
	})

	t.Run("a card already waiting keeps the free lane", func(t *testing.T) {
		t.Parallel()
		w := pushWorld(t, 1, 1, 2)
		occupy(w, "reader-m1", 1)
		occupy(w, "reader-m2", 1)
		first := working(w, "s1-1")
		second := working(w, "s1-2")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{first}}, Gens: gensOf(w.s, first)}))
		require.Empty(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1))
		// one lane frees with s1-1 waiting in review; s1-2 finishes before the tick
		w.place(w.s.Readers, ReadCardID("x0", 1, "reader-m2"), "reader-m2", OK)
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{second}}, Gens: gensOf(w.s, second)}))
		assert.Empty(t, readsAt(w.s, w.s.Work.Card("s1-2"), 1), "the finish does not take the lane the card already waiting is owed")
		w.part(TickAsk, TickReq{})
		assert.Len(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), 1, "the card that waited is asked first")
	})

	t.Run("failed work is not asked", func(t *testing.T) {
		t.Parallel()
		w := pushWorld(t, 2, 2, 1)
		wc := working(w, "s1-1")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc), Failed: true, Report: "tests red"}))
		assert.Empty(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1))
	})

	t.Run("a finish that read no reader states asks none", func(t *testing.T) {
		t.Parallel()
		w := pushWorld(t, 2, 2, 1)
		w.s.ReaderStates = nil
		wc := working(w, "s1-1")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc}}, Gens: gensOf(w.s, wc)}))
		assert.Empty(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), "who is up is unknown: the tick asks it")
	})
}
