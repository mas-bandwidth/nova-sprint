package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The brief's bound (docs/SPEC-SPRINT.md, "The brief is wrong, not the worker"; brief_bound.go).
// On 2026-10-03 two gating cards were reworked to attempts 262 and 17 with the same reader
// finding every time, by hand and by the automatic answer, because rework was offered and
// taken at each judgment. The same finding twice means the brief is wrong, not the worker:
// rework refuses, a --fix does not lift it, and a replaced brief resets the count.

// readBrokenAt has the attempt's readers read it (one on flash, reads_per_tier), the last of
// them finding it broken with the finding.
func (h *harness) readBrokenAt(id, finding string) {
	h.t.Helper()
	h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{id}}}))
	rc := h.snap().Readers.Of(id)
	require.NotEmpty(h.t, rc, "reads asked")
	for i, c := range rc {
		verdict, f := "ok", ""
		if i == len(rc)-1 {
			verdict, f = "broken", finding
		}
		h.must(ReadStep(sprint.ReadReq{Usage: "input=1000 output=100", As: c.Row, Verdict: verdict, Finding: f, Sel: sprint.Sel{IDs: []string{c.ID}}}))
	}
}

// attemptFoundBroken runs the primary's live attempt to a broken read with the finding.
func (h *harness) attemptFoundBroken(id, finding string) {
	h.t.Helper()
	h.finishAttempt(id, false, "h"+h.snap().Work.Card(id).F("attempt"))
	h.readBrokenAt(id, finding)
}

func TestReworkReworksACardWhoseLastTwoFindingsMatch(t *testing.T) {
	t.Parallel()
	rework := func(h *harness, fix string) []sprint.Refusal {
		h.t.Helper()
		return h.run(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Fix: fix, Who: "tester"})).Refused
	}
	t.Run("the same finding twice is reworked with the finding", func(t *testing.T) {
		t.Parallel()
		h := routeHarness(t, route("flash-a", "flash"))
		h.addReady("s1", 1, briefOf("flash", ""))
		h.must(DealStep(sprint.DealReq{}))
		h.attemptFoundBroken("s1-1", "files outside PATHS: internal/x.go")
		require.Empty(t, rework(h, ""), "the first finding is reworked")
		pr := h.snap().Work.Card("s1-1")
		assert.Equal(t, "1", pr.F(sprint.FieldFindingAttempt), "the rework records whose finding the primary carries")
		h.attemptFoundBroken("s1-1", "Files  outside its PATHS: internal/y.go, internal/z.go")
		require.Empty(t, rework(h, ""), "the same finding twice is still reworked, not refused at the bound")
		assert.Equal(t, 3, h.snap().Work.Card("s1-1").Int("attempt"))
		assert.Empty(t, h.openOf(sprint.NBriefWrong), "a broken finding is no brief defect")
		h.clean("reworked on the matching finding")
	})
	t.Run("too many attempts on one brief is still reworked for a broken finding", func(t *testing.T) {
		t.Parallel()
		h := routeHarness(t, route("flash-a", "flash"))
		h.addReady("s1", 1, briefOf("flash", ""))
		h.must(DealStep(sprint.DealReq{}))
		for a := 1; a <= sprint.AttemptsDefault+1; a++ {
			h.attemptFoundBroken("s1-1", "internal/f"+string(rune('0'+a))+".go:"+string(rune('0'+a))+": wrong")
			require.Empty(t, rework(h, ""), "attempt %d: a broken finding is reworked past the cap", a)
		}
		assert.Empty(t, h.openOf(sprint.NBriefWrong), "a broken finding is no brief defect")
		h.clean("reworked past the cap")
	})
	t.Run("a card never dealt, and one whose findings differ, are at no bound", func(t *testing.T) {
		t.Parallel()
		_, at := sprint.AtBriefBound(&sprint.Card{ID: "x", Fields: map[string]string{"attempt": "0"}}, "f", 0)
		assert.False(t, at)
		_, at = sprint.AtBriefBound(&sprint.Card{ID: "x", Fields: map[string]string{"attempt": "2", "finding": "a: one", sprint.FieldFindingAttempt: "1"}}, "b: two", 0)
		assert.False(t, at)
		assert.True(t, sprint.SameFinding("files outside PATHS: a.go", "two files outside its PATHS"), "one class however worded")
		assert.False(t, sprint.SameFinding("", ""), "an empty finding is never the same as another")
		// near misses: one first clause, different findings; and one finding, differently spaced and cased
		assert.False(t, sprint.SameFinding("the test fails. TestA at a.go:1 wants 2", "the test fails. TestB at b.go:9 wants 3"), "two findings that share a first sentence are two findings")
		assert.False(t, sprint.SameFinding("the test fails\nTestA at a.go:1", "the test fails\nTestB at b.go:9"), "and that share a first line")
		assert.True(t, sprint.SameFinding("The test  fails.\n  TestA at a.go:1", "the test fails. testa at a.go:1"), "one finding, whitespace collapsed and case folded")
	})
}

// The judgment raised for a card at the brief's bound on repeated failed work names the brief,
// not the worker: the two matching reports and their attempts, and its decisions are brief and
// drop, never rework. A broken finding at the bound is reworked, never left, so the bound itself
// is shown on failed work.
func TestTheBoundJudgmentNamesTheBriefNotTheWorker(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(SetStep(sprint.SetReq{Attempts: "2", Who: h.st.Actor}))
	h.must(DealStep(sprint.DealReq{}))
	h.finishAttempt("s1-1", true, "the tests fail")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Who: "tester"}))
	h.finishAttempt("s1-1", true, "the tests fail")
	assert.Empty(t, h.openOf(sprint.NWorkFailed), "the second failure is the brief's, not the worker's")
	open := h.openOf(sprint.NBriefWrong)
	require.Len(t, open, 1)
	n := open[0].Note
	assert.Equal(t, "a card has reached its bound: the brief is wrong, not the worker", n.Type)
	assert.Contains(t, n.What, "the brief is wrong, not the worker")
	assert.Contains(t, n.What, "findings: attempt 1: r; attempt 2: r")
	assert.Equal(t, []string{"brief", "drop"}, n.Decisions)
	for _, c := range h.commandsOf(sprint.NBriefWrong) {
		for _, l := range c.Lines {
			assert.NotContains(t, l, "rework", "the inbox prints no rework for it: %s", l)
		}
		assert.Contains(t, []string{"brief", "drop"}, c.Decision)
	}
	assert.True(t, strings.HasPrefix(h.commandsOf(sprint.NBriefWrong)[0].Lines[0], "nova-sprint brief s1-1 --brief-file"), h.commandsOf(sprint.NBriefWrong)[0].Lines)
	h.clean("the brief's bound judged")
}
