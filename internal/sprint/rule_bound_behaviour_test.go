package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// TestRuleBoundLeavesACardOffTheTable pins ruleBound's first answer
// (rules.go:340, docs/SPEC-SPRINT.md section 8, the rules table): a subject with
// no placed card needs a mind.
func TestRuleBoundLeavesACardOffTheTable(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.note(judgment(NBound, "s1", w.s.Now, 0, "s1-1"))
	a := answerOn(t, w, on(), NBound, "s1-1")
	assert.Equal(t, ActLeft, a.Act)
	assert.Equal(t, "the card is off the table", a.Why)
}

// TestRuleBoundLeavesACardNotAtItsBound pins ruleBound's "not at a bound now"
// answer (rules.go:368): a placed card at no bound is left to a mind.
func TestRuleBoundLeavesACardNotAtItsBound(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.note(judgment(NBound, "s1", w.s.Now, 0, "s1-1"))
	a := answerOn(t, w, on(), NBound, "s1-1")
	assert.Equal(t, ActLeft, a.Act)
	assert.Equal(t, "not at a bound now", a.Why)
}

// boundLadder serves pro and heavy from the routes, as a deal draws them.
func boundLadder() []Route {
	return []Route{
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true},
		{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "m", Enabled: true},
	}
}

// atBound puts pr in ready with its withdrawn work card at its redeal bound.
func atBound(w *world, brief, tierNow string) {
	w.t.Helper()
	w.s.Routes = boundLadder()
	w.s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: Ready, Score: 1, Rev: 1, Fields: map[string]string{
		"kind": "primary", "attempt": "1", "brief": brief, "tier_now": tierNow, "work": "s1-1.w1"}})
	w.s.Fleet.Put(&Card{ID: "s1-1.w1", Row: "m1", Col: Withdrawn, Score: 1, Rev: 1, Fields: map[string]string{
		"kind": "work", "primary": "s1-1", "attempt": "1", "redeals": itoa(MaxRedeals), FieldTakeEnded: stamp(t0)}})
	w.note(judgment(NBound, "s1", w.s.Now, 0, "s1-1"))
}

// TestRuleBoundRaisesAFlashCardToPro pins ruleBound's new attempt on the next
// tier (rules.go:382-386, docs/SPEC-SPRINT.md section 8): a flash card at its
// redeal bound goes up, on pro when a route serves it.
func TestRuleBoundRaisesAFlashCardToPro(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	atBound(w, "", cardhdr.RouteFlash)
	a := answerOn(t, w, on(), NBound, "s1-1")
	assert.Equal(t, ActUp, a.Act)
	assert.Equal(t, cardhdr.RoutePro, a.Tier)
	assert.False(t, a.Friend)
	assert.Contains(t, a.Why, "reached its bound on flash")
}

// TestRuleBoundSendsAHeavyCardToAFriend pins the ladder's top (rules.go:262,
// docs/SPEC-SPRINT.md section 8): above heavy there is no dealt tier, so the
// card becomes a friend's.
func TestRuleBoundSendsAHeavyCardToAFriend(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	atBound(w, "tier: heavy", cardhdr.RouteHeavy)
	a := answerOn(t, w, on(), NBound, "s1-1")
	assert.Equal(t, ActFriend, a.Act)
	assert.True(t, a.Friend)
	assert.Empty(t, a.Tier, "a friend's card climbs no tier: %+v", a)
}

// TestRuleBoundRaisesOnBudgetAtSecondAttempt pins ruleBound's budget branch
// (rules.go:356): budget exhaustion at the second attempt escalates a tier.
func TestRuleBoundRaisesOnBudgetAtSecondAttempt(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.s.Routes = boundLadder()
	w.s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: Review, Score: 1, Rev: 1, Fields: map[string]string{
		"kind": "primary", "attempt": "2", FieldFailure: "budget", "brief": "tier: pro"}})
	w.note(judgment(NBound, "s1", w.s.Now, 0, "s1-1"))
	a := answerOn(t, w, on(), NBound, "s1-1")
	assert.Equal(t, ActUp, a.Act)
	assert.Equal(t, cardhdr.RouteHeavy, a.Tier)
	assert.Contains(t, a.Why, "failed on budget; a new attempt on the next tier")
}

// TestRuleBoundTurnedOffDoesNotAct pins the rules switch (rules.go:210):
// with bound off, its answer acts on nothing.
func TestRuleBoundTurnedOffDoesNotAct(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	atBound(w, "", cardhdr.RouteFlash)
	w.s.RulesOff = []string{RuleBound}
	a := answerOn(t, w, on(), NBound, "s1-1")
	assert.Equal(t, ActOff, a.Act)
	assert.False(t, a.Answers())
	require.Equal(t, RuleBound, a.Rule)
}
