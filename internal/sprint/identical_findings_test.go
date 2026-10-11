package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bound is two identical findings (docs/SPEC-SPRINT.md, "The brief is wrong, not the
// worker"): a second attempt that comes back with the finding the first came back with,
// the same reader class at the same file and line however it is worded, stops the card
// and raises the brief defect at once, carrying both findings; the attempt cap stays for
// findings that differ; the bound is a setting, 2 by default.

// attemptFoundBrokenBy runs the primary's live attempt to its finish and the first read
// asked of it to broken with the finding: the reader who found it.
func attemptFoundBrokenBy(w *world, id, finding string) string {
	w.t.Helper()
	pr := w.s.Work.Card(id)
	wc := w.s.Fleet.Card(WorkCardID(id, pr.Int("attempt")))
	require.NotNil(w.t, wc, "attempt %s of %s has a work card", pr.F("attempt"), id)
	if wc.Col == Ready {
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	}
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{id}}}))
	for _, rc := range w.s.Readers.Of(id) {
		if rc.Col == Asked && rc.Int("attempt") == pr.Int("attempt") {
			w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rc.Row, Verdict: "broken", Finding: finding, Sel: Sel{IDs: []string{rc.ID}}}))
			return rc.Row
		}
	}
	w.t.Fatalf("no read of %s asked at attempt %s", id, pr.F("attempt"))
	return ""
}

// A broken finding at the brief's bound is reworked with the finding, whatever
// raised the bound: a reader's broken verdict at the same head outweighs the
// bound, so the card goes back to work with the finding rather than being left
// to a mind (docs/SPEC-SPRINT.md section 8, the read-broken rule;
// tla/SprintRules.tla, Part "reads": ReworkAtBound). The bound is still a
// setting, and a finding on a replaced brief is a first.
func TestABrokenFindingIsReworkedAtTheBriefBound(t *testing.T) {
	t.Parallel()
	rework := func(w *world) Plan { return Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}}) }
	briefWrong := func(w *world) []Note { return w.notesOf(NBriefWrong) }

	t.Run("the same finding twice is reworked with it, not stopped", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		first := attemptFoundBrokenBy(w, "s1-1", "internal/x.go:12: the guard is missing. Add it.")
		require.Empty(t, briefWrong(w), "one finding is no repeat")
		w.must(rework(w))
		require.Equal(t, 2, w.s.Work.Card("s1-1").Int("attempt"))
		assert.Equal(t, "attempt 1: "+first+" at internal/x.go:12", w.s.Work.Card("s1-1").F(FieldFindingKeys), "the rework keeps the attempt's key")

		// the same reader at the same file and line, worded otherwise: reworked
		// with the finding, never stopped at the bound
		second := attemptFoundBrokenBy(w, "s1-1", "Still broken at internal/x.go:12; the guard was never added")
		require.Equal(t, first, second, "the finder checks the fix")
		assert.Empty(t, briefWrong(w), "the same finding twice is no brief defect: it is reworked")
		w.must(rework(w))
		require.Equal(t, 3, w.s.Work.Card("s1-1").Int("attempt"), "the same finding twice is reworked again")
		assert.Equal(t, "Still broken at internal/x.go:12; the guard was never added", w.s.Work.Card("s1-1").F("fix"), "the finding is the fix")
	})

	t.Run("findings that differ past the attempt cap are still reworked", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		for a := 1; a <= AttemptsDefault+1; a++ {
			attemptFoundBrokenBy(w, "s1-1", "internal/x.go:"+itoa(10+a)+": wrong")
			require.Empty(t, briefWrong(w), "attempt %d: a broken finding is reworked, not stopped at the cap", a)
			w.must(rework(w))
		}
	})

	t.Run("another reader class at the same line is another finding", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "reader-a at internal/x.go:12", FindingKey("Reader-A", "see ./internal/x.go:12, then x.go:14"))
		assert.NotEqual(t, FindingKey("reader-a", "internal/x.go:12: a"), FindingKey("reader-b", "internal/x.go:12: a"))
		assert.NotEqual(t, FindingKey("reader-a", "internal/x.go:12: a"), FindingKey("reader-a", "internal/x.go:13: a"))
		assert.Equal(t, "work at a.go:3", FindingKey("", "tests red at a.go:3"), "failed work's report is the class work")
		assert.Equal(t, "files outside paths", FindingKey("reader-a", "Files outside its PATHS: internal/y.go"), "no file and line: the whole-text class")
		assert.Empty(t, FindingKey("reader-a", ""))
		c := &Card{ID: "x", Fields: map[string]string{"attempt": "2", "finding": "internal/x.go:12: a", FieldFindingAttempt: "1", FieldFindingReader: "reader-a"}}
		_, at := AtIdenticalFindings(c, "reader-b", "internal/x.go:12: a", 2)
		assert.False(t, at, "another reader")
		_, at = AtIdenticalFindings(c, "reader-a", "internal/x.go:12: b", 2)
		assert.True(t, at, "the same reader, file and line, a card admitted before the keys were kept")
	})

	t.Run("the bound is a setting, and a broken finding still reworks", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		assert.Equal(t, IdenticalDefault, w.s.IdenticalBound("s1"), "2 by default")
		w.s.Work.SetProp(PropIdentical, "3")
		require.Equal(t, 3, w.s.IdenticalBound("s1"))
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		for a := 1; a <= 3; a++ {
			attemptFoundBrokenBy(w, "s1-1", "internal/x.go:12: the guard is missing")
			require.Empty(t, briefWrong(w), "attempt %d: a broken finding is reworked, whatever the setting", a)
			w.must(rework(w))
		}
		w.s.Work.SetProp(PropIdentical, "1")
		assert.Equal(t, IdenticalDefault, w.s.IdenticalBound("s1"), "one finding is never a repeat: under 2 is no setting")
	})
}
