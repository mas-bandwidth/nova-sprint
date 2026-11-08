package run

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
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
// and did not answer (v5) is run's, one judgement.
func TestJudgementOpLeavesTheAnsweringVersionToSteps(t *testing.T) {
	t.Parallel()
	a := &asker{snap: store.Snapshot{Seq: 7}, answers: policy.Answers{Version: 4}, failures: []policy.Failure{
		{Version: 4, Label: "v4", Question: "verdict", Subject: "c1", Line: 3, Msg: "verdict: not a string"},
		{Version: 5, Label: "v5", Line: 1, Msg: "syntax error"}}}
	op, ok := judgementOp(a, Observed{})
	require.True(t, ok)
	require.Len(t, op.Writes, 1)
	assert.Equal(t, policy.Group(5), op.Writes[0].(store.PutJudgement).Group)
}
