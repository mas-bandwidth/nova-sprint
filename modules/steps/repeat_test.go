package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// TestRepeatedFailHolds: a work fail with the first reason line of the
// card's previous bounce leaves the producer waiting held on the second
// fail, long before the bounce limit or same_finding's top rank (two audits,
// 2026-11-15: the same merge conflict at w4-w7, GOPROXY off at w1-w6). The
// held judgement names both attempts and the repeated line; the card is
// never dealt a third time by itself, and the coordinator's unhold is a
// fresh run.
func TestRepeatedFailHolds(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	s.take("p1.w1")
	s.report("p1.w1", "fail", sha(1), "merge conflict in modules/x.go\nCONFLICT (content)")
	s.tick()
	s.requireState("p1.w2", "working")
	s.take("p1.w2")
	s.report("p1.w2", "fail", sha(2), "merge conflict in modules/x.go\nother detail")
	s.requireState("p1", "waiting")
	assert.Equal(t, "held: failed the same way twice running, p1.w1 and p1.w2: merge conflict in modules/x.go", s.snap().Cards["p1"].Reason)
	assert.Equal(t, 2, s.snap().Cards["p1"].Bounces, "the repeat spends its bounce")
	s.tick()
	js := s.openJudgements("held")
	require.Equal(t, []string{"held.p1.1"}, js)
	q := s.snap().Judgements[js[0]].Question
	for _, want := range []string{"p1.w1 on row r1", "p1.w2 on row r1", "merge conflict in modules/x.go", "nova-sprint replace p1 --by <card>"} {
		assert.Contains(t, q, want)
	}
	assert.NotContains(t, q, "other detail", "only the first line")
	s.run(4)
	s.requireState("p1", "waiting")
	_, third := s.snap().Cards["p1.w3"]
	assert.False(t, third, "never dealt a third time by itself")
	rc, ref := s.verb(Request{Actor: "rowan", Judgement: js[0], Answer: "unhold", Card: "p1", Event: card.UnholdCard{Reason: "rebased the base"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s.tick()
	assert.Empty(t, s.openJudgements("held"))
	s.run(2)
	s.take("p1.w3")
	s.report("p1.w3", "fail", "", "merge conflict in modules/x.go")
	s.tick()
	assert.Empty(t, s.openJudgements("held"), "an unhold is a fresh run: what it was held for never counts again")
	s.requireState("p1.w4", "working")
}

// TestRepeatedFailPassesOver: a different first line is no repeat; a fault
// between two same fails is passed over (it pushed no verdict); an ok
// between them ends the run.
func TestRepeatedFailPassesOver(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	end := func(c, result, head, reason string) {
		s.requireState(c, "working")
		s.take(c)
		s.report(c, result, head, reason)
		s.tick()
	}
	end("p1.w1", "fail", "", "GOPROXY off")
	end("p1.w2", "fail", "", "GOPROXY=off: go mod download refused")
	assert.Empty(t, s.openJudgements("held"), "a different first line")
	end("p1.w3", "fault", "", "exit 137")
	s.requireState("p1.w4", "working")
	s.take("p1.w4")
	s.report("p1.w4", "fail", "", "GOPROXY=off: go mod download refused")
	s.requireState("p1", "waiting")
	assert.Equal(t, "held: failed the same way twice running, p1.w2 and p1.w4: GOPROXY=off: go mod download refused", s.snap().Cards["p1"].Reason, "the fault between was passed over")
}
