package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A heavy brief is pinned heavy and never dealt to flash or pro.
func TestAHeavyBriefIsPinnedHeavyAndNeverDealtToFlashOrPro(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true, Deadline: 600},
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "RESULT: s1-1 sha=000000000000 tier: heavy\n\nThe task."}}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card(WorkCardID("s1-1", 1))
	require.NotNil(t, wc)
	assert.Equal(t, "heavy-a", wc.F(FieldRoute), "heavy card dealt on heavy route")
	assert.Equal(t, cardhdr.RouteHeavy, w.s.DealTier(w.s.Primary("s1-1")), "heavy card pin is heavy")
}

// A pro brief is never dealt to flash.
func TestAProBriefIsNeverDealtToFlash(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "RESULT: s1-1 sha=000000000000 tier: pro\n\nThe task."}}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card(WorkCardID("s1-1", 1))
	require.NotNil(t, wc)
	assert.Equal(t, "pro-a", wc.F(FieldRoute), "pro card dealt on pro route")
	assert.Equal(t, cardhdr.RoutePro, w.s.DealTier(w.s.Primary("s1-1")), "pro card pin is pro")
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
