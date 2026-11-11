package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnsweredJudgementStaysQuiet (2026-11-10, seen live: a stall answered
// retry reopened seconds later, again and again, waking the coordinator
// every couple of minutes). A watch judgement (its one choice is retry)
// answered while its cause still holds does not reopen until the judgement
// timeout has passed since the answer; then it reopens saying it still
// holds after the answer, as base-red does.
func TestAnsweredJudgementStaysQuiet(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 8, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a.go")
	s.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Needs: []string{"p1"}})
	s.run(4)
	require.Equal(t, []string{"idle.fleet.1"}, s.openJudgements("idle"))

	s.must("judge", store.AnswerJudgement{ID: "idle.fleet.1", Answer: "retry", Actor: "rowan"})
	answered := s.snap().Judgements["idle.fleet.1"]
	s.run(3)
	require.Empty(t, s.openJudgements("idle"), "answered with its cause still there: quiet for the judgement timeout, not reopened at once")

	s.now = answered.ClosedAt.Add(2 * time.Hour)
	s.tick()
	require.Equal(t, []string{"idle.fleet.2"}, s.openJudgements("idle"))
	q := s.snap().Judgements["idle.fleet.2"].Question
	assert.True(t, strings.HasPrefix(q, "STILL HOLDS 2h0m"), q)
}
