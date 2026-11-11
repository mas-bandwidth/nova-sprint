package run

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoDealNamesTheStoredVersionToActivate: with no policy active the deal's
// gate names the fix: policy activate of the newest stored version when
// versions are stored and none is active (the live store of 2026-11-07, set
// before the first set activated; defect D3), policy set when none is
// stored. An active version that answered lifts the gate. Unit: store.Fake.
func TestNoDealNamesTheStoredVersionToActivate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "no policy is active (v3 is stored, none active): nova-sprint policy activate v3 --reason <why>",
		noDeal(policy.Versions{}, policy.Answers{}, nil, 3))
	assert.Equal(t, "no policy is active: nova-sprint policy set <file> --reason <why> (the first set activates; nova-sprint up installs the default)",
		noDeal(policy.Versions{}, policy.Answers{}, nil, 0))
	active := store.PolicyVersion{Version: 2, Active: true}
	assert.Equal(t, "", noDeal(policy.Versions{Active: &active}, policy.Answers{Version: 2}, nil, 3))

	f := store.NewFake() // nothing stored, nothing active
	must(t, f, "start", store.SetStopped{Stopped: false, Reason: "test"})
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	require.NoError(t, err)
	assert.Equal(t, 0, tk.storedPolicy(context.Background()), "nothing stored")
	for i := range 3 {
		must(t, f, "policy "+string(rune('0'+i)), store.PutPolicy{Kind: "script", Name: "p", Body: "p" + string(rune('0'+i))})
	}
	assert.Equal(t, 3, tk.storedPolicy(context.Background()), "the newest stored version")
}

// TestStepsAnswersAttributesFailuresToTheirOwnVersion: v5 failed its verdict
// question and v4 answered the tick; v4's verdicts are the tick's and no
// failure is filed under v4 (v5's failure is its own policy judgement).
func TestStepsAnswersAttributesFailuresToTheirOwnVersion(t *testing.T) {
	t.Parallel()
	p := policy.Answers{Version: 4, Verdict: map[string]string{"c1": "accept"}}
	fs := []policy.Failure{{Version: 5, Label: "v5", Question: "verdict", Subject: "c1", Line: 3, Msg: "verdict: not a string"}}
	a := stepsAnswers(p, fs)
	assert.Equal(t, 4, a.Version)
	assert.Equal(t, "accept", a.Verdict["c1"])
	assert.Empty(t, a.Failed, "v5's failure is not v4's")
}

// TestJudgementOpLeavesTheAnsweringVersionToSteps: a failed question of the
// version that answered is the steps' policy judgement (one per question,
// group policy:v4), so run's op opens none for it; a version that failed
// and did not answer (v5) is run's, one judgement, whose deadline is the
// tick's now plus timeouts judgement (the engine's when none answered).
func TestJudgementOpLeavesTheAnsweringVersionToSteps(t *testing.T) {
	t.Parallel()
	a := &asker{snap: store.Snapshot{Seq: 7}, answers: policy.Answers{Version: 4}, failures: []policy.Failure{
		{Version: 4, Label: "v4", Question: "verdict", Subject: "c1", Line: 3, Msg: "verdict: not a string"},
		{Version: 5, Label: "v5", Line: 1, Msg: "syntax error"}}}
	now := time.Unix(1000, 0)
	op, ok := judgementOp(a, Observed{Now: now})
	require.True(t, ok)
	require.Len(t, op.Writes, 1)
	assert.Equal(t, policy.Group(5), op.Writes[0].(store.PutJudgement).Group)
	assert.Equal(t, now.Add(time.Duration(steps.Engine.Timeouts.Judgement)*time.Second), op.Writes[0].(store.PutJudgement).Deadline, "the engine's judgement timeout when the version names none")
	a.answers.Timeouts.Judgement = 600
	op, _ = judgementOp(a, Observed{Now: now})
	assert.Equal(t, now.Add(600*time.Second), op.Writes[0].(store.PutJudgement).Deadline, "the answering version's judgement timeout")
}

// unanswered is a policy whose every version fails the tick (Version -1, as
// at a tick timeout).
type unanswered struct{}

func (unanswered) AnswerContext(context.Context, policy.Versions, store.Snapshot, roster.Roster, policy.Questions) (policy.Answers, []policy.Failure) {
	return policy.Answers{Version: -1}, []policy.Failure{{Version: 2, Label: "v2", Line: 1, Msg: "over the tick's time budget"}}
}

// TestNoAnswerKeepsTheLastAnsweredBounds (776-H1): a tick no version
// answered gives the steps the last answered tick's limits and timeouts (a
// custom policy's bounces 16 and pro 14400), so one tick timeout neither
// fails a live pro worker at the engine's 7200 s nor holds a producer at its
// 12 bounces.
func TestNoAnswerKeepsTheLastAnsweredBounds(t *testing.T) {
	t.Parallel()
	f := store.NewFake()
	tk, err := NewTicker(context.Background(), f, &healthy{}, unanswered{}, t0)
	require.NoError(t, err)
	tk.limits = policy.Limits{Bounces: 16, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3,
		EscalateAfter: 2, FrontierAfter: 4, FlapLimit: 3, IdleFloor: 0.5, LandTarget: 0.3}
	tk.timeouts = policy.Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 1200, Judgement: 3600, RouteRestFor: 1800, ReadyNag: 30, ReadyTake: 180,
		BaseRegate: []int{120, 300}, Consumer: map[string]int{"flash": 3600, "pro": 14400, "heavy": 7200, "frontier": 7200}}
	a := tk.asker(context.Background(), roster.Roster{})
	out := a.Ask(steps.Questions{})
	assert.False(t, a.asked, "no version answered")
	assert.Equal(t, "no policy version answered", out.Failed["*"])
	require.NotNil(t, out.KeptLimits)
	require.NotNil(t, out.KeptTimeouts)
	assert.Equal(t, 16, out.KeptLimits.Bounces)
	assert.Equal(t, 14400, out.KeptTimeouts.Consumer["pro"])
}

// TestStepsAnswersCarryEveryLimit: every limit policy validates reaches the
// steps (the steering numbers were dropped, so the engine's ran whatever
// the policy said).
func TestStepsAnswersCarryEveryLimit(t *testing.T) {
	t.Parallel()
	p := policy.Answers{Version: 1, Limits: policy.Limits{Bounces: 4, Faults: 5, SameFinding: 3, ReportsPerTick: 7, RouteRestWindow: 9, RouteRestAfter: 2,
		EscalateAfter: 3, FrontierAfter: 6, FlapLimit: 8, FleetWidthCap: 4, IdleFloor: 0.25, LandTarget: 0.75}}
	l := stepsAnswers(p, nil).Limits
	require.NotNil(t, l)
	assert.Equal(t, [9]any{4, 5, 3, 7, 9, 2, 3, 6, 8}, [9]any{l.Bounces, l.Faults, l.SameFinding, l.ReportsPerTick, l.RouteRestWindow, l.RouteRestAfter, l.EscalateAfter, l.FrontierAfter, l.FlapLimit})
	assert.Equal(t, [2]float64{0.25, 0.75}, [2]float64{l.IdleFloor, l.LandTarget})
}
