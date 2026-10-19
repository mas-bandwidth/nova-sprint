package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPushedBatchIsNeverRefusedForLandAfter pins the one LAND-AFTER judgment, at the
// mark before the push: an unlanded target blocks a new mark, a report honors a pushed
// mark when the queue changed after it, and a retry of a marked but unpushed head is
// judged again. tla/SoftDependency.tla BaseRecorded and BaseSoftOrder.
func TestAPushedBatchIsNeverRefusedForLandAfter(t *testing.T) {
	t.Parallel()

	t.Run("unlanded target blocks the mark", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", "a"}})
		assert.Equal(t, time.Unix(1000, 0).UTC(), w.s.Now)
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: w.s.Work.Card("b").F("head")}})
		require.Len(t, p.Refused, 1)
		assert.Equal(t, "b", p.Refused[0].Key)
		assert.Equal(t, "soft dependency on a not landed; b is skipped this batch", p.Refused[0].Why)
		assert.Empty(t, p.Units)
		w.do(p)
		assert.False(t, LandingMarked(w.s, "b"))
		assert.False(t, PushedUnreportedMatches(w.s, "b"))
	})

	t.Run("report honors the pushed mark", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", ""}})
		head := w.s.Work.Card("b").F("head")
		w.must(MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head}}))
		// a store edit between the mark and the report: b gains LAND-AFTER: a, and a is
		// merging and unlanded, so the report-time queue would block b
		w.s.Work.Card("b").Fields[FieldLandAfter] = "a"
		// a is merging but cannot be placed in the report-time queue (it waits for a need
		// that has not landed), so the order guard would filter b out were it re-applied
		w.s.Work.Card("a").Fields["needs"] = "not-landed"
		p := MergeStep(w.s, MergeReq{Stream: "s1", Landed: []LandedPin{{ID: "b", Head: head, InBase: true}}})
		assert.Empty(t, p.Refused, "a pushed batch is never refused for LAND-AFTER")
		w.must(p)
		assert.Equal(t, Landed, w.state("b"))
	})

	t.Run("a marked head is rechecked before a retry push", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", ""}})
		head := w.s.Work.Card("b").F("head")
		w.must(MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head}}))
		w.s.Work.Card("b").Fields[FieldLandAfter] = "a"
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head}})
		require.Len(t, p.Refused, 1)
		assert.Equal(t, "soft dependency on a not landed; b is skipped this batch", p.Refused[0].Why)
		assert.Empty(t, p.Units)
		assert.False(t, PushedUnreportedMatches(w.s, "b"), "the marked head has not been pushed")
	})
}
