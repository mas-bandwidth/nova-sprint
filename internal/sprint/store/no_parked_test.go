package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A wait never parks a card (docs/SPEC-SPRINT.md section 8, "Judgments that hold a card";
// sprint.WaitRefused; tla/SprintRules.tla, FailedWithAFindingNeverParked). On 2026-10-11 the
// seat answered failed work and broken reads with `wait <id> --for 60m` for about two hours,
// and about 22 of 42 review cards froze while the fleet idled. Failed work and a broken read
// with a finding or a report rework themselves on the next tick below the bound; a wait on a
// judgment that holds a card is refused, naming the action instead; a wait on an alarm still
// holds it. On the mem twin, injected clock, no socket.

// waitOn is the seat's wait on the judgment, an hour.
func (h *harness) waitOn(id string) (bool, error) {
	h.t.Helper()
	_, held, err := h.st.Wait(h.ctx, id, h.now.Add(time.Hour))
	return held, err
}

func TestFailedWorkReworksItselfWithItsFinding(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	// the incident's shape: the same failure on several cards at once, which the failed rule
	// left to a mind (the fleet's failure) and the seat then waited on
	n := sprint.RuleSameFailureCards
	h.addReady("s1", n, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	report := "verdict not-done; the tests fail at TestA: internal/x/a.go:12 wants 2, got 3"
	for i := 1; i <= n; i++ {
		h.failTake(fmt.Sprintf("s1-%d.w1", i), report)
	}
	open := h.openOf(sprint.NWorkFailed)
	require.NotEmpty(t, open, "work came back failed: a judgment")
	_, err := h.waitOn(open[0].Note.ID)
	require.Error(t, err, "the seat's wait on failed work is refused")
	h.machine()
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("s1-%d", i)
		pr := h.snap().Work.Card(id)
		require.Equal(t, 2, pr.Int("attempt"), "%s: reworked at its next attempt by the tick, no seat action", id)
		assert.Equal(t, sprint.Working, h.state(id), "%s: working again", id)
		assert.Contains(t, pr.F("fix"), "internal/x/a.go:12 wants 2, got 3", "%s: its report is the fix", id)
	}
	assert.Empty(t, h.openOf(sprint.NWorkFailed), "every failed-work judgment answered")
	assert.Len(t, h.answeredBy(sprint.RuleFailed), n, "each answered by the failed rule")
	h.clean("failed work reworked itself")
}

func TestABrokenReadReworksItself(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(DealStep(sprint.DealReq{}))
	finding := "internal/x/a.go:40 drops the error from Close; return it"
	h.attemptFoundBroken("s1-1", finding)
	open := h.openOf(sprint.NReadBroken)
	require.Len(t, open, 1, "a reader found it broken: a judgment")
	_, err := h.waitOn(open[0].Note.ID)
	require.Error(t, err, "the seat's wait on a broken read is refused")
	h.startMachine()
	h.machine()
	pr := h.snap().Work.Card("s1-1")
	require.Equal(t, 2, pr.Int("attempt"), "reworked by the tick at its next attempt")
	assert.Equal(t, finding, pr.F("fix"), "the finding is the fix")
	assert.Empty(t, h.openOf(sprint.NReadBroken), "the judgment is answered")
	assert.NotEmpty(t, h.answeredBy(sprint.RuleReadBroken))
	h.clean("a broken read reworked itself")
}

func TestPastTheBoundItStaysASeatJudgment(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(SetStep(sprint.SetReq{Attempts: "2", Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{}))
	h.finishAttempt("s1-1", true, "")
	h.startMachine()
	h.machine()
	require.Equal(t, 2, h.snap().Work.Card("s1-1").Int("attempt"), "below the bound: reworked by the tick")
	h.finishAttempt("s1-1", true, "")
	h.machine()
	pr := h.snap().Work.Card("s1-1")
	assert.Equal(t, 2, pr.Int("attempt"), "past the bound the tick does not rework it")
	assert.Equal(t, sprint.Review, h.state("s1-1"), "it waits in review for the seat")
	open := h.openOf(sprint.NBriefWrong)
	require.Len(t, open, 1, "the bound's judgment is the seat's")
	// past the bound it is a mind's at once: no wait parks it either (wait_refused.go)
	_, err := h.waitOn(open[0].Note.ID)
	require.Error(t, err, "a wait on the bound's judgment is refused: it holds the card")
	assert.Contains(t, err.Error(), "brief it again, or drop it", "the refusal names the action instead")
	h.clean("past the bound: the seat's")
}

func TestWaitIsRefusedOnACardHoldingJudgment(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	h.st.AnswerRules = false // the judgment stays open for the seat
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(DealStep(sprint.DealReq{}))
	h.finishAttempt("s1-1", true, "")
	open := h.openOf(sprint.NWorkFailed)
	require.Len(t, open, 1)
	id := open[0].Note.ID
	held, err := h.waitOn(id)
	require.Error(t, err, "wait on failed work is refused")
	assert.False(t, held)
	assert.Contains(t, err.Error(), "rework", "the refusal names the action instead")
	assert.Contains(t, err.Error(), "drop", "the refusal names the action instead")
	after := h.openOf(sprint.NWorkFailed)
	require.Len(t, after, 1, "the judgment stays open")
	assert.True(t, after[0].Note.Review.IsZero(), "no review time was set: the card is not parked")
	// the one table: every kind that holds a card is refused by name
	for _, k := range []string{sprint.NWorkFailed, sprint.NReadBroken, sprint.NReturned, sprint.NConflict, sprint.NRed, sprint.NCross, sprint.NRejected, sprint.NBaseRed} {
		assert.Contains(t, sprint.WaitRefusedKinds(), k)
		assert.NotEmpty(t, sprint.WaitRefusal(sprint.Note{Kind: sprint.Judgment, Type: k, ID: "j"}), k)
	}
	h.clean("wait refused on failed work")
}

func TestWaitStillWorksOnAnAlarm(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 2}))
	h.startMachine()
	h.machine()
	open := h.openOf(sprint.NNoMember)
	require.Len(t, open, 1, "no member is up: the alarm")
	held, err := h.waitOn(open[0].Note.ID)
	require.NoError(t, err, "a wait on an alarm is allowed")
	assert.True(t, held, "the alarm is held until its time")
	for _, k := range []string{sprint.NNoMember, sprint.NAlarmReview, sprint.NAlarmFleet, sprint.NWorkLate, sprint.NCoordinatorBehind, sprint.NTimer} {
		assert.Empty(t, sprint.WaitRefusal(sprint.Note{Kind: sprint.Judgment, Type: k, ID: "j"}), "%s takes a wait", k)
	}
}

// A failure the fleet shares is judged at once: one sprint-level judgment, at the cards' first
// failure, naming the failure and the cards, that takes no wait; each card reworks itself on
// its tier meanwhile, and the judgment is not raised again while it is open.
func TestASharedFailureIsOneFleetJudgmentAtOnce(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	n := sprint.RuleSameFailureCards
	h.addReady("s1", n, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	report := "step 2 broken: post: exit0 go vet ./...: exit status 126; exec: go: Permission denied"
	for i := 1; i <= n; i++ {
		h.failTake(fmt.Sprintf("s1-%d.w1", i), report)
	}
	h.machine()
	open := h.judgmentsOf(sprint.NFleetFailure)
	require.Len(t, open, 1, "one judgment for the fleet's failure, at the first failure")
	what := open[0].Note.What
	assert.Contains(t, what, "step 2 broken", "it names the failure")
	for i := 1; i <= n; i++ {
		assert.Contains(t, what, fmt.Sprintf("s1-%d", i), "it names the cards")
		assert.Equal(t, 2, h.snap().Work.Card(fmt.Sprintf("s1-%d", i)).Int("attempt"), "each card reworks itself meanwhile")
	}
	_, err := h.waitOn(open[0].Note.ID)
	require.Error(t, err, "the fleet's failure takes no wait")
	h.machine()
	assert.Len(t, h.judgmentsOf(sprint.NFleetFailure), 1, "never raised again while it is open")
}

// A brief that pins its model has no ladder to climb: its failed work is reworked on its tier
// with its report, never left to the seat below the bound.
func TestAPinnedModelFailureReworksOnItsTier(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	h.addReady("s1", 1, briefOf("flash", "model: x/y\ntokens: 100\ndeadline: 60"))
	h.startMachine()
	h.machine()
	h.failTake("s1-1.w1", "verdict not-done; the tests fail at TestA: internal/x/a.go:12")
	a := answerOf(t, h, sprint.NWorkFailed, "s1-1")
	assert.Equal(t, sprint.ActRework, a.Act, a.Why)
	assert.Empty(t, a.Tier, "on its tier")
	assert.Contains(t, a.Why, "pins a model", a.Why)
	h.machine()
	pr := h.snap().Work.Card("s1-1")
	assert.Equal(t, 2, pr.Int("attempt"), "reworked by the tick")
	assert.Empty(t, pr.F(sprint.FieldTier), "no climb")
	assert.Empty(t, h.openOf(sprint.NWorkFailed))
}

// A brief on the frontier tier has no tier above it: its failed work is reworked on its tier
// with its report, never left to the seat below the bound.
func TestAFrontierTierFailureReworksOnItsTier(t *testing.T) {
	t.Parallel()
	h := ruled(t)
	h.addReady("s1", 1, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	h.failTake("s1-1.w1", "verdict not-done; the tests fail at TestA: internal/x/a.go:12")
	h.st.AnswerRules = false // the queue drained with no answer, then the brief made frontier
	h.machine()
	h.setPrimary("s1-1", map[string]string{"brief": briefOf("frontier", "")})
	a := answerOf(t, h, sprint.NWorkFailed, "s1-1")
	assert.Equal(t, sprint.ActRework, a.Act, a.Why)
	assert.Empty(t, a.Tier, "on its tier: no tier above frontier")
	assert.Contains(t, a.Why, "frontier", a.Why)
}

// answerOf is the rule answer on the open judgment of the type on the subject.
func answerOf(t *testing.T, h *harness, typ, subject string) sprint.RuleAnswer {
	t.Helper()
	for _, a := range sprint.RuleAnswers(h.snap(), sprint.TickReq{AnswerRules: true}) {
		if a.Type == typ && a.Subject == subject {
			return a
		}
	}
	t.Fatalf("no %q judgment open on %s", typ, subject)
	return sprint.RuleAnswer{}
}
