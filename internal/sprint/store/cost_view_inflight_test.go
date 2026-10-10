package store

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Per-landed cost is the landed cards' spend per landed card, not all spend so far.
// Spend on in-flight cards is tracked separately in TotalCost, so stream totals
// agree with the sprint total and nothing is hidden.
func TestPerLandedIsLandedCardsSpendOverLandedCount(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro", "pro"))
	h.must(SetStep(sprint.SetReq{Attempts: "6", Who: h.st.Actor}))
	
	// Two landed cards: s1-1 costs $1, s1-2 costs $1
	// One in-flight card: s2-1 costs $10
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: briefOf("pro", "")}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-2"}, Brief: briefOf("pro", "")}))
	h.must(AddStep(sprint.AddReq{Stream: "s2", IDs: []string{"s2-1"}, Brief: briefOf("pro", "")}))
	
	// s1-1: take, finish with cost $1, accept, merge, land
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	wc := h.snap().Fleet.Card(h.snap().Work.Card("s1-1").F("work"))
	g := map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, 
		Usage: "input=1 actual_usd=1 actual_by=harness"}))
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	h.readAllOK("s1-1")
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1}))
	
	// s1-2: take, finish with cost $1, accept, merge, land
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-2"}}}))
	wc = h.snap().Fleet.Card(h.snap().Work.Card("s1-2").F("work"))
	g = map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g,
		Usage: "input=1 actual_usd=1 actual_by=harness"}))
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"s1-2"}}}))
	h.readAllOK("s1-2")
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"s1-2"}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 2}))
	
	// s2-1: take, finish with cost $10 (in-flight)
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s2-1"}}}))
	wc = h.snap().Fleet.Card(h.snap().Work.Card("s2-1").F("work"))
	g = map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g,
		Usage: "input=1 actual_usd=10 actual_by=harness"}))
	
	costs := sprint.StreamTierCosts(h.snap())
	s1 := costs["s1"]
	
	// PerLanded should be $2/2 = $1.00 (landed cards' spend / landed count)
	// NOT $12/2 = $6.00 (all spend / landed count)
	assert.Equal(t, "$1.00", s1.PerLanded, "per landed is landed cards' spend ($2) over landed count (2)")
	// TotalCost should be $2 (s1-1 + s1-2)
	assert.Equal(t, "$2.00", s1.TotalCost, "stream total is its landed cards' spend")
	
	s2 := costs["s2"]
	// PerLanded should be "-" (nothing landed)
	assert.Equal(t, "-", s2.PerLanded, "per landed is dash when nothing landed")
	// TotalCost should be $10 (in-flight card's spend)
	assert.Equal(t, "$10.00", s2.TotalCost, "in-flight card's spend is in stream total")
	
	h.clean("per landed is landed cards' spend over landed count")
}
