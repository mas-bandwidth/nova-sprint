package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Cost visibility (docs/SPEC-SPRINT.md section 1; sprint/cost_view.go; the owner,
// 2026-10-04): the tick's where record counts every card by its brief's tier and carries,
// per stream, its tiers, its dollars per landed card and its spend by the tier each attempt
// ran on, from the cards the tick reads; where reads no card for them.

func TestTheWhereRecordCountsTiersAndCostsByTier(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"), route("pro-a", "pro"), route("heavy-a", "heavy"))
	h.must(SetStep(sprint.SetReq{Attempts: "6", Who: h.st.Actor}))
	// three tiers across two streams: s1 a flash card and a pro card, s2 a heavy card
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-f"}, Brief: briefOf("flash", "")}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-p"}, Brief: briefOf("pro", "")}))
	h.must(AddStep(sprint.AddReq{Stream: "s2", IDs: []string{"s2-h"}, Brief: briefOf("heavy", "")}))
	require.Equal(t, map[string]int{"flash": 1, "pro": 1, "heavy": 1}, sprint.TierCounts(h.snap()))

	// s1-f runs flash twice (a dollar each, failed), then pro once (two dollars, ok), and lands
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-f"}}}))
	fail := func(usd string) {
		h.t.Helper()
		wc := h.snap().Fleet.Card(h.snap().Work.Card("s1-f").F("work"))
		g := map[string]int{wc.ID: wc.Int("gen")}
		h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
		h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Failed: true, Report: "red in attempt " + wc.F("attempt"), Usage: "input=1 actual_usd=" + usd + " actual_by=harness"}))
	}
	fail("1")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-f"}}, Who: "tester"}))
	fail("1")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-f"}}, Tier: "pro", Who: "tester"}))
	wc := h.snap().Fleet.Card(h.snap().Work.Card("s1-f").F("work"))
	require.Equal(t, "pro", wc.F(sprint.FieldTier), "attempt 3 runs on pro")
	g := map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Head: "h3", Usage: "input=1 actual_usd=2 actual_by=harness"}))
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"s1-f"}}}))
	h.readAllOK("s1-f")
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"s1-f"}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 10}))
	require.Equal(t, sprint.Landed, h.state("s1-f"))

	costs := sprint.StreamTierCosts(h.snap())
	s1 := costs["s1"]
	assert.Equal(t, map[string]int{"flash": 1, "pro": 1}, s1.Tiers)
	assert.Equal(t, "$4.00", s1.PerLanded, "one landed card that cost four dollars")
	assert.Equal(t, map[string]string{"flash": "$2.00", "pro": "$2.00"}, s1.CostByTier, "the spend split by the tier each attempt ran on, not the card's ceiling")
	assert.Equal(t, "-", s1.InFlight, "s1 has no in-flight cards")
	s2 := costs["s2"]
	assert.Equal(t, map[string]int{"heavy": 1}, s2.Tiers)
	assert.Equal(t, "-", s2.PerLanded, "nothing landed")
	assert.Equal(t, "-", s2.InFlight, "s2 has no spend")
	assert.Empty(t, s2.CostByTier, "nothing spent")

	// the where record carries them, counted by the tick
	rec := whereOf(h.snap(), h.machineRecord(), h.st.now())
	assert.Equal(t, map[string]int{"flash": 1, "pro": 1, "heavy": 1}, rec.Tiers)
	assert.Equal(t, s1, rec.Streams["s1"])
	assert.Equal(t, "-", sprint.PerLandedOf("-", 0))
	assert.Equal(t, "$0.34", sprint.PerLandedOf("$1.00", 3), "a cent rounded up")
	h.clean("tiers and costs counted")
}

// Per-landed should be the total spend on landed cards divided by the landed count.
// In-flight spend should be shown separately so nothing is hidden.
func TestPerLandedUsesTotalSpendOnLandedCards(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.must(SetStep(sprint.SetReq{Attempts: "6", Who: h.st.Actor}))

	// Create three cards: two landed ($1 each, one with failed attempt), one in-flight ($10)
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"landed-1"}, Brief: briefOf("flash", "")}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"landed-2"}, Brief: briefOf("flash", "")}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"inflight"}, Brief: briefOf("flash", "")}))

	// landed-1: one successful take at $1
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"landed-1"}}}))
	wc := h.snap().Fleet.Card(h.snap().Work.Card("landed-1").F("work"))
	g := map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Head: "h1", Usage: "input=10 actual_usd=1 actual_by=harness"}))
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"landed-1"}}}))
	h.readAllOK("landed-1")
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"landed-1"}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 10}))
	require.Equal(t, sprint.Landed, h.state("landed-1"))

	// landed-2: one failed attempt ($1), one successful ($1) = $2 total
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"landed-2"}}}))
	wc = h.snap().Fleet.Card(h.snap().Work.Card("landed-2").F("work"))
	g = map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Failed: true, Usage: "input=10 actual_usd=1 actual_by=harness"}))
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"landed-2"}}, Fix: "fix"}))
	wc = h.snap().Fleet.Card(h.snap().Work.Card("landed-2").F("work"))
	g = map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Head: "h2", Usage: "input=10 actual_usd=1 actual_by=harness"}))
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"landed-2"}}}))
	h.readAllOK("landed-2")
	h.must(AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"landed-2"}}}))
	h.must(MergeStep(sprint.MergeReq{Stream: "s1", Batch: 10}))
	require.Equal(t, sprint.Landed, h.state("landed-2"))

	// inflight: one take at $10, still in waiting
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"inflight"}}}))
	wc = h.snap().Fleet.Card(h.snap().Work.Card("inflight").F("work"))
	g = map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Head: "hi", Usage: "input=100 actual_usd=10 actual_by=harness"}))
	// Card still in waiting, not landed

	h.beat()

	costs := sprint.StreamTierCosts(h.snap())
	s1 := costs["s1"]

	// Per-landed: ($1 + $2) / 2 landed cards = $1.50
	assert.Equal(t, "$1.50", s1.PerLanded, "per-landed is total spend on landed cards divided by landed count")

	// In-flight: $10 from the card still in waiting
	assert.Equal(t, "$10.00", s1.InFlight, "in-flight cost shows spend on cards not yet landed")

	// Total should be the sum of landed ($3) and in-flight ($10)
	assert.Equal(t, "$13.00", s1.TotalCost, "total cost is landed + in-flight")

	h.clean("per-landed uses total spend on landed cards")
}
