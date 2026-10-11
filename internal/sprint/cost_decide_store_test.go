package sprint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
)

// A brief decision's record at add is priced by the store's route of the decide model: the
// add step reads the routes (store.AddStep, Routes), so the routes come from the store, never
// from a snapshot a test filled. A count estimated from the text asked over is kept as
// tokens and never priced (cost.go, UsageEstimated). On the twin store (store.Mem).
func TestABriefDecisionAtAddIsPricedByTheStoresRouteAndAnEstimateNever(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	r.m.SetRoutes([]sprint.Route{
		{Name: "flash-a", Tier: "flash", Provider: "prov-flash-a", Model: "model-flash-a", Tokens: 1000, Deadline: conflictRouteSeconds, Enabled: true},
		{Name: "pro-a", Tier: "pro", Provider: "prov-pro-a", Model: "model-pro-a", Tokens: 1000, Deadline: conflictRouteSeconds, Enabled: true},
		{Name: "decide-jev", Tier: "decide", Provider: "typesafe", Model: "jev-latest",
			Prices: cardcost.Prices{Input: "1", Output: "2", AsOf: "2026-10-11"}},
	})
	brief := "c: the work (s9) tier: flash\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"
	r.must(store.AddStep(sprint.AddReq{Stream: "s9", IDs: []string{"s9-a", "s9-b"}, Brief: brief,
		BriefOps: map[string]string{"s9-a": "s9-a@brief-1", "s9-b": "s9-b@brief-2"}, BriefRecord: "brief.jsonl",
		BriefUsage: map[string]string{"s9-a": sprint.DecideUsage(2000000, 0, ""), "s9-b": sprint.DecideUsage(0, 0, "twelve bytes")}}))

	a := sprint.CardCostOf(r.snap().Work.Card("s9-a"))
	require.Len(t, a.Consumers, 1, "the card is born with its brief decision's record")
	assert.Equal(t, sprint.ConsumerDecide, a.Consumers[0].Kind)
	assert.Equal(t, "decide-jev", a.Consumers[0].Usage.Route, "priced by the store's route of the decide model")
	assert.Equal(t, "2", a.Total.Charged, "2,000,000 input at $1 a million")

	b := sprint.CardCostOf(r.snap().Work.Card("s9-b"))
	require.Len(t, b.Consumers, 1)
	assert.Equal(t, int64(3), b.Consumers[0].Usage.Tokens.Input, "the estimate is kept as tokens")
	assert.Empty(t, b.Total.Charged, "an estimated count is never priced")
	assert.Equal(t, cardcost.WhyNoRoute, b.Consumers[0].Usage.Unpriced)
}
