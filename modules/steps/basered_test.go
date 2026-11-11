package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redBase is the sim's one watched base, o/r main, with streams s1 and s2
// on it, completed red at tip on failing.
func redBase(tip string, failing ...string) map[string]BaseCI {
	return map[string]BaseCI{"o/r@main": {Repo: "o/r", Base: "main", Tip: tip, Streams: []string{"s1", "s2"}, Red: true, Failing: failing,
		Lines: []string{"unit: --- FAIL: TestDecode (0.00s) | decode_test.go:41: got 3, want 4"}, FirstRed: strings.Repeat("9", 40), Since: t0}}
}

func greenBase(tip string) map[string]BaseCI {
	return map[string]BaseCI{"o/r@main": {Repo: "o/r", Base: "main", Tip: tip, Streams: []string{"s1", "s2"}}}
}

func baseRedSim(t *testing.T) *sim {
	s := newSim(t)
	s.row("r1", 2, "pro")
	s.stream("s1")
	s.stream("s2")
	s.producer("p1", "s1", "pro", "a.go")
	s.producer("p2", "s2", "pro", "b.go")
	s.producer("p3", "s1", "pro", "c.go")
	return s
}

// TestBaseRedWakesTheCoordinator (2026-11-10: a repository's main was red
// for eleven days and nothing pushed the coordinator one message saying
// so). A base whose tip completed red, with live cards on its streams, is
// one base-red judgement about <repo>@<base>, naming the tip, the red
// checks, the streams and their cards, since when, the first failing lines,
// and what to do; the same red set at a newer tip is the same judgement,
// not pushed again; a changed red set replaces it with the next episode in
// the same op (never two open); a green tip clears it; and no read (pending
// checks, a rate-limited forge) neither opens nor clears one.
func TestBaseRedWakesTheCoordinator(t *testing.T) {
	t.Parallel()
	s := baseRedSim(t)
	s.run(1)
	require.Empty(t, s.openJudgements("base-red"), "no read yet: no judgement (the old behaviour stays the default)")

	s.bases = redBase(strings.Repeat("a", 40), "unit", "lint")
	s.run(2)
	require.Equal(t, []string{"base-red.o/r@main.1"}, s.openJudgements("base-red"))
	j := s.snap().Judgements["base-red.o/r@main.1"]
	assert.Equal(t, "BASE RED at aaaaaaaaaaaa, o/r main: 2 checks red (lint, unit): the base is red: fix it now, before anything else; "+
		"nothing on streams s1, s2 lands by itself until it is green (0 merging, 3 more not landed); red since 999999999999, first seen 1970-01-01 00:16 UTC; "+
		"first failing lines: unit: --- FAIL: TestDecode (0.00s) | decode_test.go:41: got 3, want 4; "+
		"fix it with a child of your own or a blocker card onto the base, then answer retry: the run reads its checks again, and this clears by itself when a tip is green", j.Question)
	assert.Equal(t, "nova-sprint judge "+ShellWord("base-red.o/r@main.1")+" <choice> --reason <why>   choices: retry", j.Command)
	assert.False(t, j.Deadline.IsZero(), "it escalates unanswered, as the other watch kinds do")

	s.bases = redBase(strings.Repeat("b", 40), "lint", "unit") // a newer tip, the same checks red
	s.run(2)
	require.Equal(t, []string{"base-red.o/r@main.1"}, s.openJudgements("base-red"), "the same red set is the same judgement")

	s.bases = nil // the next tip's checks are running, or the forge rests: no fact
	s.run(2)
	require.Equal(t, []string{"base-red.o/r@main.1"}, s.openJudgements("base-red"), "no read never clears")

	s.bases = redBase(strings.Repeat("c", 40), "unit")
	s.run(1)
	require.Equal(t, []string{"base-red.o/r@main.2"}, s.openJudgements("base-red"), "a changed red set is the next episode, one open")
	js := s.snap().Judgements
	assert.Equal(t, "expired", js["base-red.o/r@main.1"].State)
	assert.Equal(t, "the red set changed (1 check red (unit) at cccccccccccc): reopened as base-red.o/r@main.2", js["base-red.o/r@main.1"].Answer)
	assert.True(t, strings.HasPrefix(js["base-red.o/r@main.2"].Question, "BASE RED at cccccccccccc, o/r main: 1 check red (unit): the base is red: fix it now"))

	s.bases = greenBase(strings.Repeat("d", 40))
	s.run(1)
	require.Empty(t, s.openJudgements("base-red"))
	assert.Equal(t, "cleared: o/r main is green at dddddddddddd", s.snap().Judgements["base-red.o/r@main.2"].Answer)
	s.run(2)
	require.Empty(t, s.openJudgements("base-red"), "green opens nothing")
}

// TestBaseRedEscalatesAndWaitsOnAnAnswer: unanswered past its deadline a
// base still red reopens escalated, as every watch kind does; answered
// (retry: the run reads the checks again), the same red set stays quiet for
// the judgement timeout, then opens again saying it is still red. A base
// with no live card on its streams opens nothing, and nothing is held,
// stopped or dealt differently: the push only.
func TestBaseRedEscalatesAndWaitsOnAnAnswer(t *testing.T) {
	t.Parallel()
	s := baseRedSim(t)
	s.bases = redBase(strings.Repeat("a", 40), "unit")
	s.run(2)
	first := s.snap().Judgements["base-red.o/r@main.1"]
	require.Equal(t, "open", first.State)
	s.requireState("p1", "working")
	s.requireState("p2", "working")

	s.now = first.Deadline.Add(time.Second)
	s.tick()
	require.Equal(t, []string{"base-red.o/r@main.2"}, s.openJudgements("base-red"))
	assert.Equal(t, "UNANSWERED 1 TIME, ESCALATED: "+first.Question, s.snap().Judgements["base-red.o/r@main.2"].Question)

	s.must("judge", store.AnswerJudgement{ID: "base-red.o/r@main.2", Answer: "retry", Actor: "rowan"})
	answered := s.snap().Judgements["base-red.o/r@main.2"]
	s.run(2)
	require.Empty(t, s.openJudgements("base-red"), "answered: quiet at the same red set for the judgement timeout")

	s.now = answered.ClosedAt.Add(48 * time.Hour)
	s.tick()
	require.Equal(t, []string{"base-red.o/r@main.3"}, s.openJudgements("base-red"))
	assert.True(t, strings.HasPrefix(s.snap().Judgements["base-red.o/r@main.3"].Question, "STILL RED 48h0m"), s.snap().Judgements["base-red.o/r@main.3"].Question)

	// Answered, then a green tip, then the same checks red again: a new red
	// stretch (the watch dates it after the answer) opens at once, with no
	// STILL RED, whether or not a tick saw the green between.
	s.must("judge", store.AnswerJudgement{ID: "base-red.o/r@main.3", Answer: "retry", Actor: "rowan"})
	again := s.snap().Judgements["base-red.o/r@main.3"]
	s.run(1)
	require.Empty(t, s.openJudgements("base-red"))
	s.bases = greenBase(strings.Repeat("b", 40))
	s.run(1)
	s.bases = redBase(strings.Repeat("c", 40), "unit")
	stretch := s.bases["o/r@main"]
	stretch.FirstRed, stretch.Since = strings.Repeat("c", 40), again.ClosedAt.Add(time.Minute)
	s.bases["o/r@main"] = stretch
	s.run(1)
	require.Equal(t, []string{"base-red.o/r@main.4"}, s.openJudgements("base-red"), "green then red again inside the quiet: pushed at once")
	assert.True(t, strings.HasPrefix(s.snap().Judgements["base-red.o/r@main.4"].Question, "BASE RED at cccccccccccc"), s.snap().Judgements["base-red.o/r@main.4"].Question)

	q := newSim(t)
	q.row("r1", 2, "pro")
	q.stream("s1")
	q.bases = map[string]BaseCI{"o/r@main": {Repo: "o/r", Base: "main", Tip: strings.Repeat("a", 40), Streams: []string{"s1"}, Red: true, Failing: []string{"unit"}}}
	q.run(2)
	require.Empty(t, q.openJudgements("base-red"), "no live card behind it: nothing to wake the coordinator for")
}
