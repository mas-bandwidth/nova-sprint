package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A push is a fact, the record follows it (tla/WaitFor.tla Report,
// PushedIsRecorded; docs/SPEC-SPRINT.md section 7): a pushed batch is recorded landed
// whatever its needs or LAND-AFTER targets, and one happened note names each card and
// each dependency it landed before. Nothing is refused after the push.
func TestAPushedBatchIsRecordedWhateverItsNeeds(t *testing.T) {
	t.Parallel()

	t.Run("a hard need is not landed", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", ""}})
		w.s.Work.Card("b").Fields["needs"] = "a"
		head := w.s.Work.Card("b").F("head")
		p := MergeStep(w.s, MergeReq{Stream: "s1", Landed: []LandedPin{{ID: "b", Head: head, InBase: true}}})
		require.Empty(t, p.Refused, "a pushed batch is never refused for a hard need")
		w.must(p)
		assert.Equal(t, Landed, w.state("b"))
		got := w.notesOf(NLandedOutOfOrder)
		require.Len(t, got, 1, "one happened note for the out-of-order landing")
		assert.Equal(t, []string{"b"}, got[0].Primaries)
		assert.Contains(t, got[0].What, "b")
		assert.Contains(t, got[0].What, "a")
	})

	t.Run("the reversed witness: the old post-push check refuses", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", ""}})
		w.s.Work.Card("b").Fields["needs"] = "a"
		head := w.s.Work.Card("b").F("head")
		p := mergeStepSeam(w.s, MergeReq{Stream: "s1", Landed: []LandedPin{{ID: "b", Head: head, InBase: true}}}, true)
		require.NotEmpty(t, p.Refused, "the seam restores the refusal the first test pins")
		assert.NotEqual(t, Landed, w.state("b"), "with the post-push check restored the first test would fail")
	})

	t.Run("a LAND-AFTER target is not landed and the pin is not marked", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", "a"}})
		assert.False(t, LandingMarked(w.s, "b"), "the pin is not marked")
		head := w.s.Work.Card("b").F("head")
		p := MergeStep(w.s, MergeReq{Stream: "s1", Landed: []LandedPin{{ID: "b", Head: head, InBase: true}}})
		require.Empty(t, p.Refused, "a pushed batch is never refused for LAND-AFTER")
		w.must(p)
		assert.Equal(t, Landed, w.state("b"))
		got := w.notesOf(NLandedOutOfOrder)
		require.Len(t, got, 1, "one happened note for the out-of-order landing")
		assert.Equal(t, []string{"b"}, got[0].Primaries)
		assert.Contains(t, got[0].What, "LAND-AFTER target a")
	})
}

// Every dependency is judged before the push, hard needs (WaitsFor) as LAND-AFTER is
// (tla/WaitFor.tla Cut and Report; docs/SPEC-SPRINT.md section 7): MarkLanding refuses a
// pin whose need is not landed, writes no mark, and the lander pushes nothing.
func TestEveryDependencyCheckRunsBeforeThePush(t *testing.T) {
	t.Parallel()

	t.Run("a hard need is not landed", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", ""}})
		w.s.Work.Card("b").Fields["needs"] = "a"
		head := w.s.Work.Card("b").F("head")
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head}})
		require.Len(t, p.Refused, 1, "the mark refuses the unmet need")
		assert.Equal(t, "b", p.Refused[0].Key)
		assert.Contains(t, p.Refused[0].Why, "a")
		assert.Contains(t, p.Refused[0].Why, "not landed")
		assert.True(t, LandOrderSkipped(p.Refused[0]), "the dependency skip, as LAND-AFTER")
		assert.Empty(t, p.Units, "nothing is marked before the push")
		w.do(p)
		assert.False(t, LandingMarked(w.s, "b"))
		assert.False(t, PushedUnreportedMatches(w.s, "b"))
	})

	t.Run("a LAND-AFTER target is not landed", func(t *testing.T) {
		t.Parallel()
		w := landingMarkWorldOf(t, [][2]string{{"a", ""}, {"b", "a"}})
		head := w.s.Work.Card("b").F("head")
		p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head}})
		require.Len(t, p.Refused, 1, "the mark refuses the unmet LAND-AFTER")
		assert.Equal(t, "b", p.Refused[0].Key)
		assert.True(t, LandOrderSkipped(p.Refused[0]))
		assert.Empty(t, p.Units)
	})
}
