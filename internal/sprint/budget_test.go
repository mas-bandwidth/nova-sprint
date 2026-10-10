package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBudgetExhaustionEscalatesNotBriefDefect verifies that budget exhaustion follows
// the normal tier escalation path: two budget-exhausted attempts on the same tier
// lead to a third attempt on the next tier, not a brief-defect judgment.
func TestBudgetExhaustionEscalatesNotBriefDefect(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)

	// Deal attempt 1
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")

	// Take and finish with budget exhaustion (m1 takes)
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	budgetReport := "PROMPT-DEFECT task=s1-1.w1 reason=budget cache_read=1500 max=1000 turns=50"
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: budgetReport}))

	// First attempt failed with budget exhaustion - card in review, no readers asked
	pr := w.s.Work.Card("s1-1")
	assert.Equal(t, Review, pr.Col, "failed work goes to review")
	assert.Empty(t, w.s.Readers.Of("s1-1"), "failed work does not go to readers")

	// Rework (attempt 2) - assign to m2 which is ready
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	pr = w.s.Work.Card("s1-1")
	assert.Equal(t, Working, pr.Col, "rework goes to working")
	assert.Equal(t, "s1-1.w2", pr.F("work"), "second attempt work card")

	// Complete attempt 2 with budget exhaustion again (m2 takes)
	wc = w.s.Fleet.Card("s1-1.w2")
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Finish(w.s, FinishReq{As: "m2", Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: budgetReport}))

	// Second attempt failed with budget exhaustion
	pr = w.s.Work.Card("s1-1")
	assert.Equal(t, Review, pr.Col, "after second budget exhaust, card in review")
	// Check for brief-defect judgments (NBriefWrong), not all judgments
	briefDefects := 0
	for _, o := range w.s.Open {
		if o.Note.Type == NBriefWrong && o.Subject() == "s1-1" {
			briefDefects++
		}
	}
	assert.Equal(t, 0, briefDefects, "no brief-defect judgment after second budget exhaust")

	// Rework again (attempt 3) - should escalate to next tier (pro -> heavy)
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	pr = w.s.Work.Card("s1-1")
	assert.Equal(t, Working, pr.Col, "third attempt goes to working")
	assert.Equal(t, "s1-1.w3", pr.F("work"), "third attempt work card")
	// Check tier escalation (pro should go to heavy, since pro is the current tier)
	// The tier escalation sets FieldRuleTier in the primary's fields
	assert.NotEmpty(t, pr.F(FieldRuleTier), "tier should be set for escalation")
	assert.Equal(t, "0", pr.F(FieldRuleFails), "fail count reset after escalation")
}

// TestHarnessFaultBudgetClass verifies that budget exhaustion is recognized as a harness fault.
func TestHarnessFaultBudgetClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		report string
		class  string
	}{
		{"budget exhaustion", "PROMPT-DEFECT task=s1-1.w1 reason=budget cache_read=1500 max=1000", "budget"},
		{"budget with extra info", "PROMPT-DEFECT task=s1-1 reason=budget cache_read=2000 max=1000 turns=100; run failed", "budget"},
		{"non-budget prompt defect", "PROMPT-DEFECT task=s1-1 reason=timeout", ""},
		{"no defect", "tests red", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.class, HarnessFault(tc.report))
		})
	}
}
