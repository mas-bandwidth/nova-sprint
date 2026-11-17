package steps

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// movedBase is the sim's one watched base, o/r main (streams s1 and s2),
// moved to to by something other than the sprint's landing.
func movedBase(to string) map[string]BaseMove {
	return map[string]BaseMove{"o/r@main": {Repo: "o/r", Base: "main", To: to, At: t0, Streams: []string{"s1", "s2"}}}
}

// TestBaseMovedWakesTheCoordinator (2026-11-11: other work merged into main
// while the sprint's landing group built against main, and nothing landed
// for over an hour). A stream lands in batches into its own branch, which
// only its landings move; a base moved by anything else while cards wait to
// land on it is one base-moved judgement per repo@base (naming the affected
// streams and total waiting cards), pushed as NEEDS YOU, saying what to do.
// Legacy per-stream base-moved judgements expire cleanly.
func TestBaseMovedWakesTheCoordinator(t *testing.T) {
	t.Parallel()
	s := baseRedSim(t)
	s.run(2)
	s.must("review", store.SetCardState{Card: "p1", State: "review"})
	s.must("merging", store.SetCardState{Card: "p2", State: "merging"})
	s.must("legacy", store.PutJudgement{ID: "base-moved.s1.1", Kind: "base-moved", About: "s1", Question: "legacy"})
	s.run(1)
	require.Empty(t, s.openJudgements("base-moved"), "no foreign move: nothing, and legacy per-stream judgement cleared")
	assert.Contains(t, s.snap().Judgements["base-moved.s1.1"].Answer, "cleared:")

	s.moves = movedBase(strings.Repeat("f", 40))
	s.run(1)
	require.Equal(t, []string{"base-moved.o/r@main.1"}, s.openJudgements("base-moved"))
	j := s.snap().Judgements["base-moved.o/r@main.1"]
	assert.Equal(t, "o/r main (streams s1, s2) was moved to ffffffffffff by something other than this sprint's landing (2 cards wait to land on it): "+
		"a stream lands into its own branch, which only its landings move; stop merging into main; "+
		"put other work on its own branch and merge the stream into main once it is green", j.Question)
	assert.Equal(t, "nova-sprint judge "+ShellWord("base-moved.o/r@main.1")+" <choice> --reason <why>   choices: retry", j.Command)
	assert.False(t, j.Deadline.IsZero(), "it escalates unanswered, as the other watch kinds do")

	s.run(2)
	require.Equal(t, []string{"base-moved.o/r@main.1"}, s.openJudgements("base-moved"), "the same move is the same judgement")

	s.moves = movedBase(strings.Repeat("e", 40))
	s.run(1)
	require.Equal(t, []string{"base-moved.o/r@main.2"}, s.openJudgements("base-moved"), "a further foreign move replaces it, one open")
	assert.Equal(t, "the base was moved again (to eeeeeeeeeeee): reopened as base-moved.o/r@main.2", s.snap().Judgements["base-moved.o/r@main.1"].Answer)

	s.must("judge", store.AnswerJudgement{ID: "base-moved.o/r@main.2", Answer: "retry", Actor: "rowan"})
	s.run(2)
	require.Empty(t, s.openJudgements("base-moved"), "answered: quiet while the same move stands")

	s.moves = movedBase(strings.Repeat("d", 40))
	s.run(1)
	require.Equal(t, []string{"base-moved.o/r@main.3"}, s.openJudgements("base-moved"))

	s.moves = nil // a CIWait with no foreign move
	s.run(1)
	require.Empty(t, s.openJudgements("base-moved"))
	assert.Equal(t, "cleared: o/r main has not been moved by anything but the sprint's landing for one CIWait", s.snap().Judgements["base-moved.o/r@main.3"].Answer)
}
