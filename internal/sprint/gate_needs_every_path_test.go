package sprint

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// gateNeedsEveryPath is the one card every path below is offered: it needs darwin and
// arm64, and only a linux member is up until the last case.
const gateNeedsEveryPath = "tier: pro\nGATE-NEEDS: darwin,arm64\n\nThe task.\n"

// gateNeedsWorld is a fleet with one linux member and one darwin arm64 member, both up at
// width 2, and one ready card that needs darwin and arm64.
func gateNeedsWorld(t *testing.T) *world {
	t.Helper()
	w := fleetWorld(t, 1, 2, "m-linux", "m-linux2")
	w.s.MemberCtl("m-linux").Fields[FieldFacts] = "linux,go"
	w.s.MemberCtl("m-linux2").Fields[FieldFacts] = "linux,go"
	w.s.Work.Card("s1-1").Fields["brief"] = gateNeedsEveryPath
	return w
}

func workAttempt(w *world, id string, attempt int) *Card {
	return w.s.Fleet.Card(WorkCardID(id, attempt))
}

func noDeal(t *testing.T, w *world, id string) {
	t.Helper()
	for _, m := range w.s.Members() {
		for _, c := range w.s.Fleet.Cell(m, Ready) {
			assert.NotEqual(t, id, c.F("primary"), "%s was dealt to %s, which lacks its GATE-NEEDS", id, m)
		}
		for _, c := range w.s.Fleet.Cell(m, Working) {
			assert.NotEqual(t, id, c.F("primary"), "%s was dealt to %s, which lacks its GATE-NEEDS", id, m)
		}
	}
	require.NotEmpty(t, w.notesOf(NGateNeeds), "the wait is visible: one NGateNeeds note")
}

// TestEveryDealPathHoldsGateNeeds: a card with GATE-NEEDS darwin,arm64 and only linux
// members up is dealt by no path — the tick's deal, the rule's rework after a failed
// finish, a coordinator's rework, the conflict redo, continue, twin, the friend deal, the
// level and the rebalance — each leaves the NGateNeeds wait; then a darwin arm64 member is
// up and it is dealt there. tla/WaitFor.tla Deal, docs/SPEC-SPRINT.md, the gate-host rule.
func TestEveryDealPathHoldsGateNeeds(t *testing.T) {
	t.Parallel()
	t.Run("tick deal", func(t *testing.T) {
		w := gateNeedsWorld(t)
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		noDeal(t, w, "s1-1")
	})
	t.Run("rule rework", func(t *testing.T) {
		w := gateNeedsWorld(t)
		putWorkCard(w, "s1-1", "m-linux", DoneFailed, 1, map[string]string{"report": "red", "dealt_facts": "linux,go"})
		w.s.Work.Card("s1-1").Col = Review
		w.s.Work.Card("s1-1").Fields["brief"] = gateNeedsEveryPath
		reworkByRule(w, "s1-1")
		require.Nil(t, workAttempt(w, "s1-1", 2), "the rule's rework dealt it to a member that lacks its needs")
		noDeal(t, w, "s1-1")
	})
	t.Run("coordinator rework", func(t *testing.T) {
		w := gateNeedsWorld(t)
		putWorkCard(w, "s1-1", "m-linux", DoneFailed, 1, map[string]string{"report": "red", "dealt_facts": "linux,go"})
		w.s.Work.Card("s1-1").Col = Review
		w.s.Work.Card("s1-1").Fields["brief"] = gateNeedsEveryPath
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "a fix", Who: "coordinator"}))
		require.Nil(t, workAttempt(w, "s1-1", 2))
		noDeal(t, w, "s1-1")
	})
	t.Run("conflict redo", func(t *testing.T) {
		w := stoppedForConflict(t)
		w.s.MemberCtl("m1").Fields[FieldFacts] = "linux,go"
		w.s.MemberCtl("m2").Fields[FieldFacts] = "linux,go"
		w.s.Work.Card("s1-2").Fields["brief"] = gateNeedsEveryPath
		w.must(Redo(w.s, RedoReq{Sel: Sel{IDs: []string{"s1-2"}}, Who: "coordinator"}))
		require.Nil(t, workAttempt(w, "s1-2", 2), "the conflict redo dealt it to a member that lacks its needs")
		noDeal(t, w, "s1-2")
	})
	t.Run("continue then deal", func(t *testing.T) {
		w := gateNeedsWorld(t)
		putWorkCard(w, "s1-1", "m-linux", DoneFailed, 1, map[string]string{"head": "0123456789012345678901234567890123456789"})
		w.s.Work.Card("s1-1").Col = Review
		w.s.Work.Card("s1-1").Fields["brief"] = gateNeedsEveryPath
		w.must(Continue(w.s, ContinueReq{ID: "s1-1", Carry: "0123456789012345678901234567890123456789", Who: "coordinator"}))
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		noDeal(t, w, "s1-1")
	})
	t.Run("twin then deal", func(t *testing.T) {
		w := gateNeedsWorld(t)
		w.must(Twin(w.s, TwinReq{ID: "s1-1", Who: "coordinator"}))
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2"}}}))
		noDeal(t, w, "s1-2")
	})
	t.Run("friend deal", func(t *testing.T) {
		w := gateNeedsWorld(t)
		amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Tiers: []string{cardhdr.RoutePro}})
		p, _, _ := friendDealPass(w.s, w.s.Work.Column(Ready), []FriendSeat{amy}, false)
		w.must(p)
		assert.Equal(t, 0, w.s.Fleet.Count(FriendRow("amy"), Ready), "the friend deal gave a GATE-NEEDS card to a friend with no facts")
	})
	t.Run("friend reclaim", func(t *testing.T) {
		w := aheadWorld(t, nil)
		ahead := w.s.Fleet.Cell("m1", Ready)[0]
		pr := w.s.Work.Card(ahead.F("primary"))
		pr.Fields["brief"] = gateNeedsEveryPath
		amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash", Tiers: []string{"flash"}})
		p, _, _ := friendDealPass(w.s, w.s.Work.Column(Ready), []FriendSeat{amy}, true)
		w.must(p)
		for _, c := range w.s.Fleet.Cell(FriendRow("amy"), Ready) {
			assert.NotEqual(t, ahead.F("primary"), c.F("primary"), "the friend reclaim dealt a GATE-NEEDS card to a friend with no facts")
		}
		assert.Equal(t, "m1", w.s.Fleet.Card(ahead.ID).Row, "the unfit friend reclaimed it")
	})
	t.Run("level", func(t *testing.T) {
		w := gateNeedsWorld(t)
		putWorkCard(w, "s1-1", "m-linux", Ready, 1, nil)
		w.s.Work.Card("s1-1").Fields["brief"] = gateNeedsEveryPath
		p := Plan{}
		level(w.s, &p, w.s.UpMembers(), dealRound(w.s), roundMoves{}, nil)
		for _, u := range p.Units {
			assert.NotContains(t, u.Moved, "-> m-linux2:", "the level moved a GATE-NEEDS card to a member that cannot run it")
		}
	})
	t.Run("rebalance", func(t *testing.T) {
		w := rbWorld(t, "m-linux", "m-linux2")
		w.s.MemberCtl("m-linux").Fields[FieldFacts] = "linux,go"
		w.s.MemberCtl("m-linux2").Fields[FieldFacts] = "linux,go"
		rbPlace(w, "s1-w", "pro", "m-linux", Working)
		rbPlace(w, "s1-1", "pro", "m-linux", Ready, "pr.brief", gateNeedsEveryPath)
		w.must(Rebalance(w.s, nil, "machine"))
		wc := w.s.Fleet.Card("s1-1.w1")
		require.NotNil(t, wc)
		assert.Equal(t, "m-linux", wc.Row, "the rebalance moved a GATE-NEEDS card to a member that cannot run it")
	})
	t.Run("then the darwin arm64 member takes it", func(t *testing.T) {
		w := gateNeedsWorld(t)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m-darwin", Width: 2}))
		w.s.MemberCtl("m-darwin").Fields[FieldFacts] = "darwin,arm64,go"
		w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m-linux"}))
		w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m-linux2"}))
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wc := workAttempt(w, "s1-1", 1)
		require.NotNil(t, wc)
		assert.Equal(t, "m-darwin", wc.Row, "the card is dealt where the facts satisfy it")
		assert.Contains(t, wc.F(FieldDealtFacts), "darwin")
	})
}

// TestAnAttemptOnAnUnfitMemberCountsNoAttempt: a failed finish from a member whose facts at
// the deal lacked the card's GATE-NEEDS is the machine's fault: it spends no attempt toward
// the bound or the cap, keeps no finding, and the member is never dealt the card again in
// this attempt window; one happened note names the path that dealt it. With the guard
// removed the same finish spends the attempt (the reversed witness, factblind).
// tla/WaitFor.tla FinishFactBlind.
func TestAnAttemptOnAnUnfitMemberCountsNoAttempt(t *testing.T) {
	t.Parallel()
	scenario := func(t *testing.T) (*world, *Card, *Card) {
		w := gateNeedsWorld(t)
		putWorkCard(w, "s1-1", "m-linux", Working, 1, map[string]string{"attempt": "1", "dealt_facts": "linux,go", "dealt_by": "the tick's deal"})
		pr := w.s.Work.Card("s1-1")
		pr.Fields["brief"] = gateNeedsEveryPath
		wc := workAttempt(w, "s1-1", 1)
		require.NotNil(t, wc)
		return w, pr, wc
	}
	w, pr, wc := scenario(t)
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), As: "m-linux", Failed: true, Report: "HOLD: cannot run darwin"}))
	assert.Equal(t, 0, pr.Int("failed"), "no failure counted toward the brief's bound")
	assert.Empty(t, pr.F(FieldFindings), "no finding is kept")
	assert.Equal(t, "1", pr.F("attempt"), "the attempt is not spent")
	assert.Equal(t, Ready, w.state("s1-1"), "the card waits ready at the same attempt")
	notes := w.notesOf(NGateNeeds)
	require.Len(t, notes, 1, "one happened note names the machine's fault")
	assert.Contains(t, notes[0].What, "the tick's deal", "the note names the path that dealt it")
	// the fit member up: the same attempt is dealt there, never again to m-linux
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m-darwin", Width: 2}))
	w.s.MemberCtl("m-darwin").Fields[FieldFacts] = "darwin,arm64,go"
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m-linux"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m-linux2"}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc2 := workAttempt(w, "s1-1", 1)
	require.NotNil(t, wc2, "the same attempt is dealt again")
	assert.Equal(t, "m-darwin", wc2.Row)
	assert.False(t, slices.ContainsFunc(w.s.Fleet.Cell("m-linux", Ready), func(c *Card) bool { return c.F("primary") == "s1-1" }), "the unfit member is dealt it again")
	// the reversed witness: without the guard the same finish spends the attempt
	saved := factBlindGuard
	t.Cleanup(func() { factBlindGuard = saved })
	factBlindGuard = func(*Card, *Card) bool { return false }
	w2, pr2, wc2b := scenario(t)
	w2.must(Finish(w2.s, FinishReq{Sel: Sel{IDs: []string{wc2b.ID}}, Gens: gensOf(w2.s, wc2b.ID), As: "m-linux", Failed: true, Report: "HOLD: cannot run darwin"}))
	assert.Equal(t, 1, pr2.Int("failed"), "with the guard removed the attempt is spent: the witness fails")
}
