package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func overloaded(over bool, load int, d time.Duration) Overload {
	return Overload{Over: over, Cores: 64, Load1Centi: load, LowCenti: 35012, PeakCenti: 83055, For: d, SlotsUsed: 3, SlotsTotal: 16, Factor: 2, Span: 5 * time.Minute}
}

// TestOverloadWakesTheCoordinator (2026-11-10: a 64-thread bench sat at
// load 350-830 for hours under CI jobs sized by nproc, and the coordinator
// saw the board's load once and did nothing). A bench row over the bound
// for the span is one overload judgement about the row, naming its cores,
// its load now and over the span, its card work against its width and
// slots, and what to do; it stays one while the row stays over, clears by
// itself once the row is back under for the span, and no fact neither
// opens nor clears it. A friend row (not a bench) never gets one.
func TestOverloadWakesTheCoordinator(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 2, "pro")
	s.row("kin1", 2, "pro")
	delete(s.roster.Workers, "kin1") // a friend: on the bus, but no bench
	s.run(1)
	require.Empty(t, s.openJudgements("overload"), "no fact yet: no judgement (the old behaviour stays the default)")

	s.overloads = map[string]Overload{"r1": overloaded(true, 51200, 5*time.Minute+10*time.Second), "kin1": overloaded(true, 51200, time.Hour)}
	s.run(2)
	require.Equal(t, []string{"overload.r1.1"}, s.openJudgements("overload"), "one per bench row, none for a friend")
	j := s.snap().Judgements["overload.r1.1"]
	assert.Equal(t, "OVERLOAD row r1: this machine is overloaded: find what is loading it and fix it now (runner count or per-job parallelism for CI, width for card work); "+
		"load 512.00 on 64 cores now (8.0x), 350.12 to 830.55 over the last 5m10s, the bound is 2.0x (128.00) for 5m0s; "+
		"card work: 0 consumers working of width 2, nova-reserve slots 3 of 16 in use; "+
		"answer retry once fixed: it clears by itself when the load stays under the bound for 5m0s", j.Question)
	assert.Equal(t, "nova-sprint judge overload.r1.1 <choice> --reason <why>   choices: retry", j.Command)
	assert.False(t, j.Deadline.IsZero(), "it escalates unanswered, as the other watch kinds do")

	delete(s.overloads, "r1") // no fact (the watch restarted): it stands
	s.run(2)
	require.Equal(t, []string{"overload.r1.1"}, s.openJudgements("overload"))

	s.overloads = map[string]Overload{"r1": overloaded(false, 3000, 6*time.Minute)}
	s.run(1)
	require.Empty(t, s.openJudgements("overload"))
	assert.Equal(t, "cleared: row r1 is under 2.0x its 64 cores for 6m0s (load 30.00)", s.snap().Judgements["overload.r1.1"].Answer)
}

// TestOverloadEscalatesAndWaitsOnAnAnswer: unanswered past its deadline a
// row still over reopens escalated; answered, it stays quiet for the span
// (the load takes that long to show a fix), then opens again if still over.
// A row that is down gets none.
func TestOverloadEscalatesAndWaitsOnAnAnswer(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 2, "pro")
	s.overloads = map[string]Overload{"r1": overloaded(true, 51200, 10*time.Minute)}
	s.run(2)
	first := s.snap().Judgements["overload.r1.1"]
	require.Equal(t, "open", first.State)

	s.now = first.Deadline.Add(time.Second)
	s.tick()
	require.Equal(t, []string{"overload.r1.2"}, s.openJudgements("overload"))
	assert.Equal(t, "UNANSWERED 1 TIME, ESCALATED: "+first.Question, s.snap().Judgements["overload.r1.2"].Question)

	s.must("judge", store.AnswerJudgement{ID: "overload.r1.2", Answer: "retry", Actor: "rowan"})
	answered := s.snap().Judgements["overload.r1.2"]
	s.run(2)
	require.Empty(t, s.openJudgements("overload"), "answered: quiet for the span")
	s.now = answered.ClosedAt.Add(6 * time.Minute)
	s.tick()
	require.Equal(t, []string{"overload.r1.3"}, s.openJudgements("overload"))
	assert.True(t, strings.HasPrefix(s.snap().Judgements["overload.r1.3"].Question, "OVERLOAD row r1: this machine is overloaded"))

	q := newSim(t)
	q.row("r2", 2, "pro")
	q.must("down", store.SetRowState{Row: "r2", State: "down"})
	q.overloads = map[string]Overload{"r2": overloaded(true, 51200, time.Hour)}
	q.run(2)
	require.Empty(t, q.openJudgements("overload"), "a row that is down is not judged on its last beat")
}
