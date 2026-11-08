package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngineNoHeadJudgementClearsWhenTheHeadIsRead is defect D6 (2026-11-07):
// a stream whose base tip the edge failed to read opens an engine judgement
// at the deal; once the inputs carry its head again the cause has cleared,
// and the judgement expires on its own instead of sitting until its
// deadline or the coordinator's retry. A stream still with no head keeps it.
func TestEngineNoHeadJudgementClearsWhenTheHeadIsRead(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 8, "pro")
	s.stream("s1")
	s.stream("s2")
	s.producer("p1", "s1", "pro", "a.go")
	s.producer("p2", "s2", "pro", "b.go")
	s.headless = set{"s1": {}, "s2": {}}
	s.run(2)
	require.Equal(t, "ready", s.state("p1"))
	require.Equal(t, []string{"engine.s1.1", "engine.s2.1"}, s.openJudgements("engine"))
	assert.Contains(t, s.snap().Judgements["engine.s1.1"].Question, "stream s1 has no head in the tick's inputs")
	delete(s.headless, "s1")
	s.run(3)
	require.Equal(t, "working", s.state("p1"), "s1 deals once its head is read")
	require.Equal(t, []string{"engine.s2.1"}, s.openJudgements("engine"), "s1's judgement cleared; s2 still has no head")
	j := s.snap().Judgements["engine.s1.1"]
	assert.Equal(t, "expired", j.State)
	assert.Equal(t, "stream s1 has a head in the tick's inputs again", j.Answer)
}
