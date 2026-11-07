package steps

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toReview takes p1 of s1 through one work to review, its first read dealt.
func toReview(s *sim) {
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 4, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.requireState("p1", "review")
}

// faultReads faults n reads of p1's review in turn, each dealt again.
func faultReads(t *testing.T, s *sim, first, n int) {
	for i := first; i < first+n; i++ {
		id := fmt.Sprintf("p1.r1.%d", i)
		s.requireState(id, "working")
		s.take(id)
		s.report(id, "fault", "", "exit 137")
		s.tick()
		s.requireState("p1", "review")
	}
}

// TestTickFaultedReadIsOwedAgain: a faulted read is not a read (directive
// 58): it is owed again, no verdict is asked on it, and at policy's faults
// one read-faults judgement asks the coordinator, with no read dealt.
func TestTickFaultedReadIsOwedAgain(t *testing.T) {
	s := newSim(t)
	toReview(s)
	faultReads(t, s, 1, Engine.Limits.Faults-1)
	assert.Empty(t, s.openJudgements("read-faults"), "under the limit the read is owed again")
	last := fmt.Sprintf("p1.r1.%d", Engine.Limits.Faults)
	s.take(last)
	s.report(last, "fault", "", "exit 137")
	s.run(3)
	s.requireState("p1", "review")
	require.Equal(t, []string{"read-faults.p1.1"}, s.openJudgements("read-faults"))
	j := s.snap().Judgements["read-faults.p1.1"]
	assert.Contains(t, j.Command, "choices: read | yes | fix")
	assert.Contains(t, j.Question, "faulted (the limit is 3)")
	for _, c := range s.v.Consumers("p1") {
		assert.NotEqual(t, "working", c.State, "no read is dealt past the limit: %s", c.ID)
	}
}

// TestTickReadFaultsAnswers: read starts the count again (a read is dealt);
// yes moves the producer to merging and fix to ready with the finding.
func TestTickReadFaultsAnswers(t *testing.T) {
	s := newSim(t)
	toReview(s)
	faultReads(t, s, 1, Engine.Limits.Faults)
	s.run(2)
	_, ref := s.verb(Request{Actor: "rowan", Judgement: "read-faults.p1.1", Answer: "read"})
	require.Nil(t, ref)
	s.run(2)
	next := fmt.Sprintf("p1.r1.%d", Engine.Limits.Faults+1)
	s.requireState(next, "working")
	assert.Empty(t, s.openJudgements("read-faults"))
	faultReads(t, s, Engine.Limits.Faults+1, Engine.Limits.Faults)
	s.run(2)
	require.Equal(t, []string{"read-faults.p1.2"}, s.openJudgements("read-faults"), "the count starts again after read")
	_, ref = s.verb(Request{Actor: "rowan", Card: "p1", Event: card.JudgedYes{Reason: "read the diff: sound"}, Judgement: "read-faults.p1.2", Answer: "yes"})
	require.Nil(t, ref)
	s.requireState("p1", "merging")

	f := newSim(t)
	toReview(f)
	faultReads(t, f, 1, Engine.Limits.Faults)
	f.run(2)
	_, ref = f.verb(Request{Actor: "rowan", Card: "p1", Event: card.JudgedFix{Finding: "a/x:3 the guard is inverted"}, Judgement: "read-faults.p1.1", Answer: "fix"})
	require.Nil(t, ref)
	f.requireState("p1", "ready")
	assert.Equal(t, "a/x:3 the guard is inverted", f.card("p1").Finding)
	assert.Equal(t, 1, f.card("p1").Bounces)
}
