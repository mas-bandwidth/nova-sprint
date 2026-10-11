package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The deal's plan and its refusals, driven through Deal (steps_work.go:954; its doc
// comment is the contract, with round.go, width.go and fleet_quiet.go): each chosen card
// is dealt to the next member round the fleet with room for it, and a card no up member
// can take is refused in the words the contract names.

// TestTheDealRefusesEveryCardWhenTheFleetsWorkIsOff: with the fleet's switch off, no card
// is dealt and each refusal names the set verb (steps_work.go:992, the fleet's work off).
func TestTheDealRefusesEveryCardWhenTheFleetsWorkIsOff(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Work.SetProp(PropFleet, SwitchOff)
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
	require.Empty(t, p.Units, "the fleet's work off dealt %+v", p)
	require.Len(t, p.Refused, 2, "%+v", p)
	for _, r := range p.Refused {
		assert.Contains(t, r.Why, "run: nova-sprint set --fleet on")
	}
	assert.Equal(t, Ready, w.state("s1-1"))
	assert.Equal(t, Ready, w.state("s1-2"))
}

// TestTheDealRefusesEveryCardWhenNoMemberIsUp: with no member up, every chosen card is
// refused "no fleet member is up" (steps_work.go:998, no up member).
func TestTheDealRefusesEveryCardWhenNoMemberIsUp(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 2, "%+v", p)
	for _, r := range p.Refused {
		assert.Contains(t, r.Why, "no fleet member is up")
	}
}

// TestTheDealRefusesEveryCardWhenEveryUpMemberIsHeldByAFault: a member held by an
// uncleared environment fault is no member of the deal, so with every up member held the
// cards are refused as if none were up (steps_work.go:991, envFaulted).
func TestTheDealRefusesEveryCardWhenEveryUpMemberIsHeldByAFault(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	for _, m := range []string{"m1", "m2"} {
		w.s.MemberCtl(m).Fields[FieldEnvFault] = EnvPush
	}
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
	require.Empty(t, p.Units, "a held member was dealt %+v", p)
	require.Len(t, p.Refused, 2, "%+v", p)
	for _, r := range p.Refused {
		assert.Contains(t, r.Why, "no fleet member is up")
	}
}

// TestTheDealRefusesABenchCardWhoseBenchIsDown: a card whose BENCH line names a member
// that is down waits in ready and is refused in the bench's words (bench_deal.go:136).
func TestTheDealRefusesABenchCardWhoseBenchIsDown(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 0, "m1", "m2")
	addBenched(t, w, "s1", map[string]string{"s1-1": "m1"})
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "waits for its bench m1")
	assert.Contains(t, p.Refused[0].Why, "fleet beat m1 on its machine")
	assert.Equal(t, Ready, w.state("s1-1"))
}

// TestTheDealRefusesABenchCardWhoseBenchIsAtItsRoom: with its bench up but at its room
// (DealAhead times its width), the refusal is the bench room's words (bench_deal.go:143).
func TestTheDealRefusesABenchCardWhoseBenchIsAtItsRoom(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 1, "m1")
	addBenched(t, w, "s1", map[string]string{"s1-1": "m1", "s1-2": "", "s1-3": ""})
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2", "s1-3"}}}))
	require.Equal(t, DealAhead*1, widthHeld(w, "m1"), "m1 is not at its room")
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "a bench card was dealt past the room %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "every member of its bench m1")
	assert.Contains(t, p.Refused[0].Why, "at its room (DealAhead times its width)")
}

// TestTheDealRefusalNamesTheRoomThenTheQuietMember: with one member quiet and the other at
// its room, a card with no BENCH line is refused with the room words, then "; " and
// quietWhy's words naming "<m> until <t>" (steps_work.go:1018-1024, fleet_quiet.go:126).
func TestTheDealRefusalNamesTheRoomThenTheQuietMember(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 1, "m1", "m2")
	until := w.s.Now.Add(time.Hour)
	w.must(FleetStep(w.s, FleetReq{Op: "quiet", Member: "m1", Until: until, Reason: "load 64", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 3}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1", "s1-2"}}})) // m1 quiet: both to m2, its room
	require.Equal(t, DealAhead*1, widthHeld(w, "m2"), "m2 is not at its room")
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-3"}}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, noRoomWhy)
	assert.Contains(t, p.Refused[0].Why, "; quiet: m1 until "+stamp(until))
}

// TestAWithdrawnCardUnderItsBoundIsDealtAgainAtTheNextGeneration: a card withdrawn because
// no member was up is the same card dealt again at a new generation, its attempt unchanged
// (steps_work.go:950-953, Deal's doc comment).
func TestAWithdrawnCardUnderItsBoundIsDealtAgainAtTheNextGeneration(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1", "m2")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	takeIt(w, "s1-1.w1")
	attempt := w.s.Work.Card("s1-1").Int("attempt")
	presenceWith(w) // no member beating: the take ends, the card withdraws
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Withdrawn, wc.Col, "the card did not withdraw with no member up")
	gen := wc.Int("gen")
	presenceWith(w, "m1", "m2")
	p := w.do(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Empty(t, p.Refused, "%+v", p)
	require.Len(t, p.Units, 1, "%+v", p)
	got := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, got)
	assert.Equal(t, gen+1, got.Int("gen"), "the same card dealt again at the next generation")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "the redeal changed the attempt")
	assert.Contains(t, []string{"m1", "m2"}, got.Row, "dealt to no member of the fleet")
	assert.Empty(t, StagingRefusers(got))
}

// TestAWithdrawnCardEveryUpMemberRefusedAtStagingIsRefused: a withdrawn card every member
// up refused at staging is refused naming them and the fix (steps_work.go:1082).
func TestAWithdrawnCardEveryUpMemberRefusedAtStagingIsRefused(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1", "m2")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	takeIt(w, "s1-1.w1")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
		Report: cardhdr.EndStaging + ": no bench mirror"}))
	require.Equal(t, Ready, w.state("s1-1"))
	// the same card dealt again to the other member, which refuses it too
	p := w.do(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Empty(t, p.Refused, "%+v", p)
	require.Len(t, p.Units, 1, "%+v", p)
	takeIt(w, "s1-1.w1")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
		Report: cardhdr.EndStaging + ": no bench mirror"}))
	require.Equal(t, []string{"m1", "m2"}, StagingRefusers(w.s.Fleet.Card("s1-1.w1")))
	p = Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "%+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "s1-1.w1 was refused at staging by every member up (m1, m2): rework it with a fix, or drop it")
}

// TestAWithdrawnCardAtItsRedealBoundIsRefused: a withdrawn card at its bound, with no tier
// above it, is refused with its bound and the fix (steps_work.go:1043-1048).
func TestAWithdrawnCardAtItsRedealBoundIsRefused(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1", "m2")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	takeIt(w, "s1-1.w1")
	presenceWith(w) // no member up: the take ends, the card withdraws
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Withdrawn, wc.Col)
	require.NotEmpty(t, wc.F(FieldTakeEnded))
	wc.Fields["redeals"] = itoa(MaxRedeals)
	presenceWith(w, "m1", "m2")
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "a card at its bound was dealt %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, "its bound, and is not dealt again")
	assert.Contains(t, p.Refused[0].Why, ": rework it with a fix, or drop it")
}

// TestAWithdrawnCardAtItsBoundBelowItsCeilingEscalates: a withdrawn card at its redeal
// bound below its ceiling is dealt a new attempt on the next tier, its why naming the bound
// and the redeal count (steps_work.go:1050-1070; route.go, tierLadder).
func TestAWithdrawnCardAtItsBoundBelowItsCeilingEscalates(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 1, "m1", "m2")
	w.s.Routes = []Route{
		{Name: "flash-oc", Tier: cardhdr.RouteFlash, Provider: "p", Model: "flash-m", Enabled: true},
		{Name: "pro-oc", Tier: cardhdr.RoutePro, Provider: "p", Model: "pro-m", Enabled: true},
		{Name: "heavy-oc", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "heavy-m", Enabled: true},
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "c: the work (s1) tier: heavy\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"}}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Equal(t, "flash", w.s.Work.Card("s1-1").F(FieldTierNow), "a heavy card's first deal is on flash")
	takeIt(w, "s1-1.w1")
	presenceWith(w) // no member up: the take ends, the card withdraws
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Withdrawn, wc.Col)
	require.NotEmpty(t, wc.F(FieldTakeEnded))
	wc.Fields["redeals"] = itoa(MaxRedeals)
	presenceWith(w, "m1", "m2")
	p := w.do(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Empty(t, p.Refused, "a card below its ceiling was refused: %+v", p.Refused)
	require.Len(t, p.Units, 1, "%+v", p)
	require.Equal(t, 2, w.s.Work.Card("s1-1").Int("attempt"), "the escalation cut no new attempt")
	assert.Equal(t, "s1-1.w2", w.s.Work.Card("s1-1").F("work"))
	next := w.s.Fleet.Card("s1-1.w2")
	require.NotNil(t, next, "the new attempt's work card was not cut")
	assert.Equal(t, cardhdr.RoutePro, next.F(FieldTier), "the new attempt is not on the next tier")
	assert.Contains(t, next.F("why"), "attempt 1 reached its bound on flash (redealt 3 times)")
}

// TestTheDealGoesRoundTheFleetAndStopsAtEachMembersRoom: a selection dealt over a fleet of
// width-1 members goes round them, none past DealAhead times its width, and the card past
// the room is refused with the room words (steps_work.go:1018, round.go).
func TestTheDealGoesRoundTheFleetAndStopsAtEachMembersRoom(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 5, 1, "m1", "m2")
	p := w.do(Deal(w.s, DealReq{Sel: Sel{Limit: 5}}))
	require.Len(t, p.Units, 4, "dealt %d cards, want the fleet's room of 4: %+v", len(p.Units), p)
	require.Len(t, p.Refused, 1, "%+v", p)
	assert.Contains(t, p.Refused[0].Why, noRoomWhy)
	assert.Equal(t, DealAhead*1, widthHeld(w, "m1"), "m1 past its room")
	assert.Equal(t, DealAhead*1, widthHeld(w, "m2"), "m2 past its room")
	assert.Equal(t, Ready, w.state("s1-5"), "the card past the room is not dealt")
}
