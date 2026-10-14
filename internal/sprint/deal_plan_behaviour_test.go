package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deal's plan (Deal, steps_work.go:954; its doc comment is the contract):
// each primary in work order is dealt to the next member round the fleet, a
// card no up member has room for is refused, a card withdrawn because no
// member was up is dealt again at a new generation with its attempt unchanged,
// and each refusal names the reason. These tests drive Deal and assert on the
// plan's units and each refusal's text, never on dealPlan's internals.

// TestDealRefusesEveryCardWhenTheFleetsWorkIsOff: off (nova-sprint set
// --fleet off, settings.go FleetOff), the deal hands no work card to a machine.
func TestDealRefusesEveryCardWhenTheFleetsWorkIsOff(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Work.SetProps(map[string]string{PropFleet: SwitchOff})
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
	require.Empty(t, p.Units, "the fleet's work off deals nothing: %+v", p)
	require.Len(t, p.Refused, 2, "every chosen card is refused: %+v", p)
	for _, r := range p.Refused {
		assert.Equal(t, "the fleet's work is off: no card is dealt to a machine; run: nova-sprint set --fleet on", r.Why)
	}
	assert.Equal(t, Ready, w.state("s1-1"), "nothing was dealt")
	assert.Equal(t, Ready, w.state("s1-2"), "nothing was dealt")
}

// TestDealRefusesEveryCardWhenEveryUpMemberIsEnvironmentHeld: a member held by
// an uncleared environment fault is dealt nothing (steps_work.go EnvHeld), so
// with every up member so held each card is refused "no fleet member is up".
func TestDealRefusesEveryCardWhenEveryUpMemberIsEnvironmentHeld(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	for _, m := range []string{"m1", "m2"} {
		w.s.MemberCtl(m).Fields[FieldEnvFault] = "push"
	}
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
	require.Empty(t, p.Units, "every up member env-held deals nothing: %+v", p)
	require.Len(t, p.Refused, 2, "every chosen card is refused: %+v", p)
	for _, r := range p.Refused {
		assert.Equal(t, "no fleet member is up: a member is up while its machine beats; start nova-sprint fleet beat <member> on a machine, or release a hold with nova-sprint fleet up <member>", r.Why)
	}
	assert.Equal(t, Ready, w.state("s1-1"), "nothing was dealt")
}

// TestDealRefusesABenchCardWhoseBenchIsDownAndOneAtItsRoom: a card whose BENCH
// line names a member that is down is refused with the bench's words, and one
// whose bench is up but at its room with the bench room words (bench_deal.go).
func TestDealRefusesABenchCardWhoseBenchIsDownAndOneAtItsRoom(t *testing.T) {
	t.Parallel()
	t.Run("bench down", func(t *testing.T) {
		t.Parallel()
		w := fleetWorld(t, 0, 0, "m1", "m2")
		w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", Who: "coordinator"}))
		addBenched(t, w, "s1", map[string]string{"s1-1": "m1"})
		p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
		require.Empty(t, p.Units, "%+v", p)
		require.Len(t, p.Refused, 1, "%+v", p)
		assert.Contains(t, p.Refused[0].Why, "waits for its bench m1: no member of it is up")
		assert.Contains(t, p.Refused[0].Why, "nova-sprint fleet beat m1")
	})
	t.Run("bench at its room", func(t *testing.T) {
		t.Parallel()
		w := fleetWorld(t, 0, 1, "m1", "m2")
		addBenched(t, w, "s1", map[string]string{"s1-1": "m1", "s1-2": "m1", "s1-3": "m1"})
		p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
		require.Empty(t, p.Refused, "%+v", p)
		w.must(p)
		p = Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-3"}}})
		require.Empty(t, p.Units, "%+v", p)
		require.Len(t, p.Refused, 1, "%+v", p)
		assert.Contains(t, p.Refused[0].Why, "every member of its bench m1 is at its room (DealAhead times its width): the tick deals it when one has room")
	})
}

// TestDealRefusesACardWhenOneMemberIsQuietAndTheOtherAtItsRoom: the refusal is
// the room words, then "; " and the quiet's words naming "<m> until <t>"
// (fleet_quiet.go quietWhy).
func TestDealRefusesACardWhenOneMemberIsQuietAndTheOtherAtItsRoom(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 5, 1, "m1", "m2")
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	until := t0.Add(30 * time.Minute)
	w.must(FleetStep(w.s, FleetReq{Op: "quiet", Member: "m1", Until: until, Reason: "load 64", Who: "coordinator"}))
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-5"}}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	want := noRoomWhy + "; quiet: m1 until " + until.UTC().Truncate(time.Second).Format(time.RFC3339)
	assert.Equal(t, want, p.Refused[0].Why)
	assert.Equal(t, Ready, w.state("s1-5"), "nothing was dealt")
}

// TestAWithdrawnCardIsRedealtAtANewGenerationNeverToAStagingRefuser: a card
// withdrawn because no member was up, under its redeal bound, is dealt again at
// generation+1 with its attempt unchanged, never to a member StagingRefusers
// names (steps_work.go:1036-1099).
func TestAWithdrawnCardIsRedealtAtANewGenerationNeverToAStagingRefuser(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	gen0, attempt := wc.Int("gen"), wc.Int("attempt")
	refuser := wc.Row
	w.must(Take(w.s, TakeReq{As: refuser, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	require.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "no member up withdraws the card")
	wc = w.s.Fleet.Card("s1-1.w1")
	wc.Fields[FieldStagingTake+itoa(wc.Int("gen"))] = ProviderTake{Member: refuser}.String()
	w.up("m1")
	w.up("m2")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc = w.s.Fleet.Card("s1-1.w1")
	assert.Greater(t, wc.Int("gen"), gen0, "dealt again at a new generation")
	assert.Equal(t, attempt, wc.Int("attempt"), "its attempt is unchanged")
	assert.NotEqual(t, refuser, wc.Row, "never dealt to a member that refused it at staging")
	assert.Equal(t, otherOf(refuser), wc.Row, "dealt to the other member")
}

// TestAWithdrawnCardEveryUpMemberRefusedAtStagingIsRefused: a withdrawn card
// every up member refused at staging is refused, naming them (steps_work.go:1077-1084).
func TestAWithdrawnCardEveryUpMemberRefusedAtStagingIsRefused(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	wc = w.s.Fleet.Card("s1-1.w1")
	wc.Fields[FieldStagingTake+"1"] = ProviderTake{Member: "m1"}.String()
	wc.Fields[FieldStagingTake+"2"] = ProviderTake{Member: "m2"}.String()
	w.up("m1")
	w.up("m2")
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Equal(t, "s1-1.w1 was refused at staging by every member up (m1, m2): rework it with a fix, or drop it", p.Refused[0].Why)
	assert.Equal(t, Ready, w.state("s1-1"), "nothing was dealt")
}

// TestAWithdrawnCardAtItsRedealBoundEscalatesBelowItsCeiling: at its redeal
// bound below the tier ceiling, a withdrawn card is a new attempt on the next
// tier, its why naming the bound and the redeals (steps_work.go:1037-1070).
func TestAWithdrawnCardAtItsRedealBoundEscalatesBelowItsCeiling(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	w.up("m2")
	w.s.Routes = []Route{
		{Name: "small", Tier: cardhdr.RouteFlash, Provider: "p", Model: "small", Enabled: true},
		{Name: "next", Tier: cardhdr.RoutePro, Provider: "p", Model: "next", Enabled: true},
		{Name: "big", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "big", Enabled: true},
	}
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: "c: the work (s1) tier: heavy\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"}))
	// a card dealt on flash below its ceiling before line 1 became the tier it is dealt on:
	// the escalation ladder still carries it
	w.s.Work.Card("s1-1").Fields[FieldTierNow] = "flash"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Equal(t, "flash", w.s.Work.Card("s1-1").F(FieldTierNow), "its deal is on flash")
	wc := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	wc = w.s.Fleet.Card("s1-1.w1")
	wc.Fields["redeals"] = itoa(MaxRedeals)
	wc.Fields[FieldTakeEnded] = "t"
	w.up("m1")
	w.up("m2")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	pr := w.s.Work.Card("s1-1")
	assert.Equal(t, 2, pr.Int("attempt"), "a new attempt")
	assert.Equal(t, "pro", pr.F(FieldTierNow), "escalated to the next tier")
	wc = w.s.Fleet.Card("s1-1.w2")
	require.NotNil(t, wc, "the next attempt's card is cut")
	assert.Contains(t, wc.F("why"), "attempt 1 reached its bound on flash (redealt 3 times)", "the card says why it was redealt")
}

// TestAWithdrawnCardAtItsRedealBoundAtItsCeilingIsRefused: the same card at
// its ceiling (no next tier) is refused with its bound and the remedy
// (steps_work.go:1042-1048).
func TestAWithdrawnCardAtItsRedealBoundAtItsCeilingIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	w.up("m2")
	w.s.Routes = []Route{
		{Name: "small", Tier: cardhdr.RouteFlash, Provider: "p", Model: "small", Enabled: true},
		{Name: "next", Tier: cardhdr.RoutePro, Provider: "p", Model: "next", Enabled: true},
	}
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: "c: the work (s1) tier: pro\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Equal(t, "pro", w.s.Work.Card("s1-1").F(FieldTierNow), "a pro card's first deal is on pro")
	wc := w.s.Fleet.Card("s1-1.w1")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	wc = w.s.Fleet.Card("s1-1.w1")
	wc.Fields["redeals"] = itoa(MaxRedeals)
	wc.Fields[FieldTakeEnded] = "t"
	w.up("m1")
	w.up("m2")
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "s1-1.w1: attempt 1 was redealt 3 times, its bound, and is not dealt again")
	assert.Contains(t, p.Refused[0].Why, ": rework it with a fix, or drop it")
	assert.Equal(t, Ready, w.state("s1-1"), "nothing was dealt")
}

// TestDealGoesRoundTheFleetAndNeverPastAWidth: a selection of four over two
// members of width 1 is dealt round the fleet, two each, and the card past the
// room is refused with the room words (width.go DealAhead).
func TestDealGoesRoundTheFleetAndNeverPastAWidth(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 5, 1, "m1", "m2")
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 4}})
	require.Empty(t, p.Refused, "%+v", p)
	require.Len(t, p.Units, 4, "four cards dealt: %+v", p)
	w.must(p)
	assert.Equal(t, 2, w.s.Fleet.Count("m1", Ready), "m1 is dealt DealAhead times its width, no more")
	assert.Equal(t, 2, w.s.Fleet.Count("m2", Ready), "m2 is dealt DealAhead times its width, no more")
	p = Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-5"}}})
	require.Empty(t, p.Units, "a card past the room is never dealt: %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Equal(t, noRoomWhy, p.Refused[0].Why)
	assert.Equal(t, Ready, w.state("s1-5"), "the card past the room stays ready")
}
