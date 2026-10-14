package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The bound rule's answers, reached only through RuleAnswers with an open
// NBound judgment (docs/SPEC-SPRINT.md section 8, the rules table;
// tla/SprintRules.tla LadderClimbs). ruleBound itself is never called.

const (
	ruleFlashBrief = "c: the work (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"
	ruleHeavyBrief = "c: the work (s1) tier: heavy\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"
)

// boundOn opens an NBound judgment on subject and hands back the rule's answer
// on it.
func boundOn(t *testing.T, w *world, subject string) RuleAnswer {
	t.Helper()
	w.note(judgment(NBound, "s1", w.s.Now, 0, subject))
	return answerOn(t, w, TickReq{}, NBound, subject)
}

// An NBound judgment on a card off the table, or not at a bound now, is left for a mind
// (rules.go:340-343, rules.go:360-370).
func TestRuleBoundLeavesACardOffTheTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, subject, why string }{
		{"RuleBoundLeavesACardOffTheTable", "ghost", "the card is off the table"},
		{"RuleBoundLeavesACardNotAtItsBound", "s1-1", "not at a bound now"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := boundOn(t, setup(t, 1), tc.subject)
			require.Equal(t, RuleBound, a.Rule)
			assert.Equal(t, ActLeft, a.Act)
			assert.Equal(t, tc.why, a.Why)
		})
	}
}

// fFailedOnBudget puts s1-1 in review, failed on budget at attempt 2, with brief.
func fFailedOnBudget(w *world, brief string) {
	pr := w.s.Work.Card("s1-1")
	pr.Col = Review
	pr.Fields["brief"] = brief
	pr.Fields["result"] = "failed"
	pr.Fields[FieldFailure] = "budget"
	pr.Fields["attempt"] = "2"
	pr.Rev++
	w.s.Work.cells, w.s.Work.byPrimary = nil, nil
}

// A failure budget exhaustion at attempt 2 raises the card a tier up, "failed
// on budget; a new attempt on the next tier" (rules.go:356-358).
func TestRuleBoundAnswersABudgetFailureAtAttemptTwo(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	fFailedOnBudget(w, ruleFlashBrief)

	a := boundOn(t, w, "s1-1")
	require.Equal(t, RuleBound, a.Rule)
	require.Equal(t, ActUp, a.Act, a.Why)
	assert.Equal(t, cardhdr.RoutePro, a.Tier, "a tier up")
	assert.Contains(t, a.Why, "failed on budget; a new attempt on the next tier")
}

// A heavy card at its bound goes to a friend: no tier above heavy is dealt
// (rules.go:258-266,372-386).
func TestRuleBoundSendsAHeavyCardAtItsBoundToAFriend(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	fFailedOnBudget(w, ruleHeavyBrief)

	a := boundOn(t, w, "s1-1")
	require.Equal(t, RuleBound, a.Rule)
	require.Equal(t, ActFriend, a.Act, a.Why)
	assert.True(t, a.Friend)
	assert.Contains(t, a.Why, "no tier of the ladder above heavy")
}

// A flash card at its real redeal bound escalates a tier by rule, to pro when a
// pro route is served: up, on pro, "reached its bound on flash" (rules.go:360-386).
func TestRuleBoundEscalatesAFlashCardAtItsRedealBound(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true},
	}
	w.s.Work.Card("s1-1").Fields["brief"] = ruleFlashBrief
	// the tick_laws_test.go recipe: deal, at the redeal count, take, member down
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	w.s.Fleet.Card("s1-1.w1").Fields["redeals"] = itoa(MaxRedeals)
	presenceWith(w, otherOf(w.s.Fleet.Card("s1-1.w1").Row))
	presenceWith(w, "m1", "m2")
	holder := w.s.Fleet.Card("s1-1.w1").Row
	takeIt(w, "s1-1.w1")
	presenceWith(w, otherOf(holder))
	require.NotNil(t, AtRedealBound(w.s, w.s.Work.Card("s1-1")), "at its redeal bound")

	a := boundOn(t, w, "s1-1")
	require.Equal(t, RuleBound, a.Rule)
	require.Equal(t, ActUp, a.Act, a.Why)
	assert.Equal(t, cardhdr.RoutePro, a.Tier, "a tier up on the ladder")
	assert.Contains(t, a.Why, "reached its bound on flash")
}

// A ready card every member up refused at staging needs a mind: left, "every
// member up refused it at staging" (rules.go:364-367).
func TestRuleBoundLeavesACardEveryMemberRefusedAtStaging(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	putWorkCard(w, "s1-1", "m1", Withdrawn, 1, map[string]string{
		FieldStagingTake + "1": ProviderTake{Member: "m1"}.String(),
		FieldStagingTake + "2": ProviderTake{Member: "m2"}.String(),
	})

	a := boundOn(t, w, "s1-1")
	require.Equal(t, RuleBound, a.Rule)
	assert.Equal(t, ActLeft, a.Act)
	assert.Equal(t, "every member up refused it at staging: the fleet's, a mind's", a.Why)
}

// With the bound rule named off in the rules settings, no answer acts on the
// card: Act off (rules.go:210-212; rules.go:164 RuleOff).
func TestRuleBoundOffLeavesTheJudgmentAlone(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	fFailedOnBudget(w, ruleFlashBrief)
	w.s.RulesOff = []string{RuleBound}

	a := boundOn(t, w, "s1-1")
	assert.Equal(t, ActOff, a.Act, a.Why)
	assert.False(t, a.Answers())
}
