package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tier on line 1, one rule in both forms (docs/CLI.md, add): `<title> tier: <t>` or the
// RESULT line `RESULT: <id> sha=<sha12> tier: <t>`. flash, pro and heavy are read as the
// tier the card is stored with and dealt on; frontier, a model pin and a line with no tier
// store none (frontier is the friends', a pin's model decides).
func TestTheTierOnLineOneIsReadInBothForms(t *testing.T) {
	t.Parallel()
	for brief, want := range map[string]string{
		"RESULT: every-need sha=000000000000 tier: heavy\nREPO: x/y\n\nThe task.":           "heavy",
		"RESULT: a-card sha=0123456789ab tier: pro\n\nThe task.":                            "pro",
		"c: the work (s1) tier: flash\n\nThe task.":                                         "flash",
		"c: the work tier: heavy\n\nThe task.":                                              "heavy",
		"RESULT: a-card sha=000000000000 tier: frontier\n\nThe task.":                       "",
		"RESULT: a-card sha=000000000000\n\nThe task.":                                      "",
		"c: no tier here\n\nThe task.":                                                      "",
		"c: a pin tier: pro\nmodel: openrouter/x\ntokens: 1000\ndeadline: 600\n\nThe task.": "",
	} {
		assert.Equal(t, want, LineOneTier(brief), "%q", brief)
	}
}

// add stores a card with the tier its line 1 names and says it; a card whose line 1 names
// none takes its stream's default tier when the stream names one (stream set --default-tier;
// a card brief with neither is refused before the step, by the card checks' tier-line).
func TestAddStoresTheTierLineOneNamesOrItsStreamsDefault(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	p := w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "RESULT: s1-1 sha=000000000000 tier: heavy\n\nThe task."}}}))
	assert.Equal(t, cardhdr.RouteHeavy, w.s.DealTier(w.s.Work.Card("s1-1")), "stored with the tier line 1 names: its deal draws it")
	require.NotEmpty(t, p.Units)
	assert.Contains(t, p.Units[0].Moved, "tier=heavy", "add says the tier it stored")
	_, ceiling := CardTiers(w.s.Work.Card("s1-1"))
	assert.Equal(t, cardhdr.RouteHeavy, ceiling)

	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-2", Brief: "RESULT: s1-2 sha=000000000000\n\nThe task."}}}))
	assert.Equal(t, cardhdr.RouteFlash, w.s.DealTier(w.s.Work.Card("s1-2")), "no tier and no default: flash, the start")

	ctl := w.s.Merge.Card(CtlID("s1"))
	require.NotNil(t, ctl)
	ctl.Fields[FieldDefaultTier] = cardhdr.RoutePro
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-3", Brief: "RESULT: s1-3 sha=000000000000\n\nThe task."}}}))
	assert.Equal(t, cardhdr.RoutePro, w.s.DealTier(w.s.Work.Card("s1-3")), "the stream's default tier")
}

// A heavy card is dealt on heavy at its first deal, never on flash, while a flash route
// serves (the seat, 2026-10-12: six cards whose line 1 said tier: heavy were dealt on flash).
func TestAHeavyCardsFirstDealDrawsAHeavyRoute(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.setWidth("m1", 2)
	w.up("m1")
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
		{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true, Deadline: 600},
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "RESULT: s1-1 sha=000000000000 tier: heavy\n\nThe task."}}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card(WorkCardID("s1-1", 1))
	require.NotNil(t, wc)
	assert.Equal(t, "heavy-a", wc.F(FieldRoute), "the first deal draws the tier line 1 names")
}

// brief re-tiers a card by its new line 1 (steps_edit.go, Brief): it says <id> tier=<t>, and
// a tier stronger than the one the card was dealt on raises tier_now, so its next attempt is
// dealt on it; a weaker line 1 never lowers a tier the card is on.
func TestABriefWhoseLineOneNamesAStrongerTierRaisesItsTierNow(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "c: the work (s1) tier: flash\n\nThe task."}}}))
	w.s.Work.Card("s1-1").Fields[FieldTierNow] = cardhdr.RouteFlash // dealt on flash before

	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "s1-1", Brief: "c: the work (s1) tier: heavy\n\nThe task."}}, Who: "coordinator"}))
	assert.Contains(t, p.Said, "s1-1 tier=heavy", "brief says the tier the card is dealt on")
	assert.Equal(t, cardhdr.RouteHeavy, w.s.Work.Card("s1-1").F(FieldTierNow), "a stronger line 1 raises tier_now")

	p = w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "s1-1", Brief: "c: the work (s1) tier: pro\n\nThe task."}}, Who: "coordinator"}))
	assert.Contains(t, p.Said, "s1-1 tier=heavy", "a weaker line 1 keeps the stronger tier the card is on")
	assert.Equal(t, cardhdr.RouteHeavy, w.s.Work.Card("s1-1").F(FieldTierNow), "never lowered")
}
