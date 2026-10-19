package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPushedBatchIsNeverRefusedForLandAfter pins the single pre-push judgment:
// unlanded targets block new marks, a report honors the mark made before push,
// and a retry of a marked but not-yet-pushed head is checked again.
func TestAPushedBatchIsNeverRefusedForLandAfter(t *testing.T) {
	t.Parallel()

	t.Run("an unlanded target blocks the mark", func(t *testing.T) {
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
		// Simulate a store edit after the push: the newly added target is still unlanded.
		w.s.Work.Card("b").Fields[FieldLandAfter] = "a"
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

	t.Run("a review target skips the cut before mark", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", "a"}})
		w.s.Work.Card("a").Col = Review
		w.s.Work.cells, w.s.Work.byPrimary = nil, nil
		w.s.Merge.Card("a").Col = ""
		w.s.Merge.cells, w.s.Merge.byPrimary = nil, nil
		b := w.s.Work.Card("b")
		ordered, why := MergeSoftOrder(w.s, []*Card{b})
		assert.Empty(t, ordered)
		assert.Equal(t, "b: soft dependency on a not landed; b is skipped this batch", why)
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: b.F("head")}})
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "soft dependency on a not landed")
		assert.Empty(t, p.Units)
		assert.Equal(t, Review, w.s.Work.Card("a").Col)
		assert.Equal(t, Merging, w.s.Work.Card("b").Col)
	})

}
