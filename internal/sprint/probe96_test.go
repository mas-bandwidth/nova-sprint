package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The cold read of PR 96's probes, kept: a need that HAS landed and a worker whose base still
// lacks it (a stale or other-repo BASE) reporting "not landed" every take was a free redeal
// with no bound (twelve rounds, no judgment). The table's facts decide: every need has
// landed, so the HOLD is judged as before (the brief-defect rule), never returned.
func TestProbe96LandedNeedHoldLoopsUnbounded(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Work.Card("s1-2").Col = Landed
	setNeeds(w.s, "s1-1", "s1-2")
	const hold = "HOLD: DEPENDS-ON s1-2 has not landed; the base lacks lib/x.go, which s1-2 adds"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Placed("s1-1.w1")
	require.NotNil(t, wc, "the fixture: s1-1 is dealt")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Who: wc.Row}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, As: wc.Row, Gens: gensOf(w.s, wc.ID), Failed: true, Report: hold}))
	require.Empty(t, w.notesOf(NNeedsUnlanded), "no return: every need has landed")
	require.Equal(t, Review, w.s.StateOf("s1-1"), "judged, not dealt again")
	require.NotEmpty(t, append(w.notesOf(NBriefDefect), w.notesOf(NWorkFailed)...), "a judgment for the seat")
}

// A real failure (not about needs) whose report mentions a landed need and the words "not
// landed" about something else stays the worker's.
func TestProbe96UnrelatedNotLandedWordsSkipTheFailure(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Work.Card("s1-2").Col = Landed
	setNeeds(w.s, "s1-1", "s1-2")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Placed("s1-1.w1")
	require.NotNil(t, wc)
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: wc.Row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
		Report: "FAIL: TestFoo red after building on s1-2's helper; the upstream fix has not landed in the vendored lib"}))
	require.Empty(t, w.notesOf(NNeedsUnlanded), "words are no fact: not a needs-stop")
	require.Len(t, w.notesOf(NWorkFailed), 1, "the worker's failed work")
	require.Equal(t, 1, w.s.Work.Card("s1-1").Int("failed"))
}
