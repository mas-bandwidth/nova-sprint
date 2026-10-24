package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NotDoneVerdictLine reads the report's first verdict line: HOLD and not-done in any case
// and spelling are named; LAND, done, ok, no verdict line and prose are not.
func TestNotDoneVerdictLineReadsTheFirstVerdictLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		report, want string
	}{
		{"Verdict: HOLD\nHead: abc\n", "Verdict: HOLD"},
		{"verdict not-done; tests red in pkg/x", "verdict not-done; tests red in pkg/x"},
		{"Verdict: not done -- DONE-WHEN 3 unmet", "Verdict: not done -- DONE-WHEN 3 unmet"},
		{"summary first\n\nNOT DONE\n", "NOT DONE"},
		{"**Verdict:** **HOLD**", "**Verdict:** **HOLD**"},
		{"## Verdict: Hold.", "## Verdict: Hold."},
		{"- verdict: NOT_DONE", "- verdict: NOT_DONE"},
		{"VERDICT: notdone", "VERDICT: notdone"},
		{"pushed=9f3c2e1 to rowan/x: Verdict: HOLD, the gate is red", "pushed=9f3c2e1 to rowan/x: Verdict: HOLD, the gate is red"},
		{"hold", "hold"},
		// not a HOLD
		{"Verdict: LAND\nHead: abc\n", ""},
		{"Verdict: done", ""},
		{"verdict ok", ""},
		{"tests green", ""},
		{"", ""},
		// the first verdict line decides: a LAND that quotes a HOLD further down is a LAND
		{"Verdict: LAND\n\nthe last attempt said Verdict: HOLD", ""},
		{"Verdict: HOLD\nVerdict: LAND", "Verdict: HOLD"},
		// prose is no verdict line
		{"verdicts were mixed: not done in two places", ""},
		{"hold on, the tests are green", ""},
		{"not done yet but close", ""},
		{"the reader may say Verdict: HOLD", ""},
	} {
		assert.Equal(t, tc.want, NotDoneVerdictLine(tc.report), "report %q", tc.report)
	}
}

// A plain finish whose report's verdict line says HOLD or not done is refused, naming the
// line and the fix (--failed), and nothing moves; the same report with --failed finishes
// the card failed, and a LAND report or one with no verdict line finishes ok as before.
func TestAPlainFinishOfAHoldReportIsRefusedNamingTheFix(t *testing.T) {
	t.Parallel()
	for _, report := range []string{"Verdict: HOLD\nDONE-WHEN 2 unmet", "verdict not-done; tests red", "Verdict: not done", "NOT DONE"} {
		t.Run(report, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
			c := w.s.Fleet.Card("s1-1.w1")
			w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))

			p := Finish(w.s, FinishReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: "9f3c2e1", Report: report})
			require.Empty(t, p.Units, "a plain finish of a HOLD report moves nothing: %+v", p)
			require.Len(t, p.Refused, 1, "%+v", p)
			assert.Equal(t, c.ID, p.Refused[0].Key)
			line := NotDoneVerdictLine(report)
			assert.Contains(t, p.Refused[0].Why, "the report's verdict line "+line)
			assert.Contains(t, p.Refused[0].Why, "finish "+c.ID+" with --failed")
			require.Equal(t, Working, w.state("s1-1"), "refused: the card is still working")

			w.must(Finish(w.s, FinishReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: "9f3c2e1", Report: report, Failed: true}))
			assert.Equal(t, "no", w.s.Fleet.Card(c.ID).F("ok"), "the --failed finish records it failed")
		})
	}
}

func TestAPlainFinishOfALandOrUnmarkedReportIsOk(t *testing.T) {
	t.Parallel()
	for _, report := range []string{"Verdict: LAND\nHead: 9f3c2e1", "tests green", "Verdict: done", ""} {
		t.Run(report, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
			c := w.s.Fleet.Card("s1-1.w1")
			w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
			w.must(Finish(w.s, FinishReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID), Head: "9f3c2e1", Report: report}))
			assert.Equal(t, Review, w.state("s1-1"))
			assert.Equal(t, "yes", w.s.Fleet.Card(c.ID).F("ok"))
		})
	}
}
