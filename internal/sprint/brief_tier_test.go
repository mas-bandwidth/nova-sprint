package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief's tier is its pin: a heavy brief is pinned heavy and never dealt to
// flash or pro, a pro brief is never dealt to flash.
func TestABriefIsDealtOnItsTier(t *testing.T) {
	t.Parallel()
	routes := []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true, Deadline: 600},
	}
	for _, c := range []struct {
		name, tier string
		routes     []Route
	}{
		{"AHeavyBriefIsPinnedHeavyAndNeverDealtToFlashOrPro", cardhdr.RouteHeavy, routes},
		{"AProBriefIsNeverDealtToFlash", cardhdr.RoutePro, routes[:2]},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
			w.s.Routes = c.routes
			w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "RESULT: s1-1 sha=000000000000 tier: " + c.tier + "\n\nThe task."}}}))
			w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
			wc := w.s.Fleet.Card(WorkCardID("s1-1", 1))
			require.NotNil(t, wc)
			assert.Equal(t, c.tier+"-a", wc.F(FieldRoute), c.tier+" card dealt on "+c.tier+" route")
			assert.Equal(t, c.tier, w.s.DealTier(w.s.Primary("s1-1")), c.tier+" card pin is "+c.tier)
		})
	}
}

// A card brief with no RESULT-line tier is refused by the card lint.
func TestACardBriefWithNoResultLineTierIsRefusedByLint(t *testing.T) {
	t.Parallel()
	// The lint check is in internal/card. A card brief (with PATHS:) whose line 1 names no tier
	// is refused with a tier-line finding. A free task (no PATHS:) is not refused.
	const rest = "\nREPO: o/r\nPATHS: internal/x/*.go\n\nTHE TASK.\n"
	const restFree = "\nREPO: o/r\n\nTHE TASK.\n"

	brief := "RESULT: s1-1 sha=000000000000" + rest
	checks := []string{}
	for _, f := range card.Checks("s1-1", brief, card.Options{}) {
		checks = append(checks, f.Check)
	}
	assert.Equal(t, []string{"tier-line"}, checks, "a card brief whose line 1 names no tier is refused")

	briefFree := "c: the work (s1)" + restFree
	checksFree := []string{}
	for _, f := range card.Checks("s1-1", briefFree, card.Options{}) {
		checksFree = append(checksFree, f.Check)
	}
	assert.Empty(t, checksFree, "a free task (no PATHS:) is not refused for no tier")
}

// A brief edit that changes the declared tier changes the pin.
func TestABriefEditThatChangesTierChangesThePin(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
	}
	// Start with flash
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "c: the work (s1) tier: flash\n\nThe task."}}}))
	pr := w.s.Primary("s1-1")
	assert.Equal(t, cardhdr.RouteFlash, w.s.DealTier(pr), "initial tier is flash")
	// Simulate being dealt on flash
	w.s.Work.Card("s1-1").Fields[FieldTierNow] = cardhdr.RouteFlash

	// Edit to heavy
	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "s1-1", Brief: "c: the work (s1) tier: heavy\n\nThe task."}}, Who: "coordinator"}))
	assert.Contains(t, p.Said, "s1-1 tier=heavy", "brief says new tier")
	assert.Equal(t, cardhdr.RouteHeavy, w.s.DealTier(pr), "edit to heavy changes pin")

	// Re-edit to pro (weaker than heavy) - should keep heavy
	p = w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "s1-1", Brief: "c: the work (s1) tier: pro\n\nThe task."}}, Who: "coordinator"}))
	assert.Contains(t, p.Said, "s1-1 tier=heavy", "weaker tier is rejected")
	assert.Equal(t, cardhdr.RouteHeavy, w.s.DealTier(pr), "never lowered")
}
