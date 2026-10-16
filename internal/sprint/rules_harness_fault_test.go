package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A harness fault is the harness's, never the brief's (docs/SPEC-SPRINT.md section 8, the
// rules table's row failed; tla/SprintRules.tla, Part "answers"). max_attempts bounds
// attempts of every kind, and that count is kept (AttemptsSince), but it is not a vote that
// the brief is wrong: only a worker's or the brief's end counts toward the brief-defect mark
// and toward the brief-wrong wording of the bound judgment. A card whose attempts all ended
// on a harness fault reaches the bound worded "harness fault", is never marked a brief
// defect, and takes rework --carry.
func TestAHarnessFaultNeverCountsTowardTheBriefDefectBound(t *testing.T) {
	t.Parallel()

	t.Run("two idle-reap ends and one not-done on pro: harness fault, not a brief defect, rework --carry accepted", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
		w.s.MaxAttempts = "2" // two attempts since the decision reach the cap

		// attempt 1 is a worker's: it pushed a head the carry will stage from
		pushedFirstAttempt(w, "s1-1", carryHead1)

		// the seat's rework is the coordinator decision: the count starts at attempt 1
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "again"}))
		require.Equal(t, "1", w.s.Work.Card("s1-1").F(FieldDecidedAttempt), "the seat's rework is a decision")
		dealWith(w)

		// attempt 2 ends on a harness fault (a push refused), reworked by rule
		takeAndFinish(t, w, WorkCardID("s1-1", 2), harnessPushRefused)
		rules(w, on())
		dealWith(w)

		// attempt 3 ends on another harness fault (a deadline with no output), reaching the cap
		takeAndFinish(t, w, WorkCardID("s1-1", 3), "deadline: the take ran 601s past its deadline with no report")
		rules(w, on())

		pr := w.s.Work.Card("s1-1")
		open := openOf(w, NBriefWrong, "s1-1")
		require.Len(t, open, 1, "the bound's one judgment")
		assert.Contains(t, open[0].Note.What, "harness fault", "the bound is worded as the harness's, never the brief's")
		assert.NotContains(t, open[0].Note.What, "the brief is wrong", "a harness fault is not a vote the brief is wrong")
		assert.Empty(t, pr.F(FieldBriefDefect), "no brief_defect stamp: it is not marked a brief defect")

		// rework --carry is accepted: the refusal "brief-defect cards cannot be reworked" does not apply
		p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Carry: true, Fix: "the reader's named fix"})
		assert.Empty(t, p.Refused, "rework --carry accepted: %+v", p.Refused)
	})

	t.Run("one worker not-done and two harness ends in one window: harness fault, not a brief defect", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
		w.s.MaxAttempts = "3" // the three attempts, no decision between them, reach the cap
		dealWith(w)

		// attempt 1 is a worker's: a result came back, not-done (a vote)
		id1 := WorkCardID("s1-1", 1)
		wc1 := w.s.Fleet.Card(id1)
		require.NotNil(t, wc1, "the attempt is dealt")
		w.must(Take(w.s, TakeReq{As: wc1.Row, Sel: Sel{IDs: []string{id1}}, Gens: gensOf(w.s, id1)}))
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{id1}}, Gens: gensOf(w.s, id1), Failed: true, Head: carryHead1,
			Report: "NOT-DONE: the test wording does not match; a_test.go:12 expects the old line"}))
		rules(w, on())
		dealWith(w)

		// attempts 2 and 3 end on the harness (a push refused, a deadline with no output)
		takeAndFinish(t, w, WorkCardID("s1-1", 2), harnessPushRefused)
		rules(w, on())
		dealWith(w)
		takeAndFinish(t, w, WorkCardID("s1-1", 3), "deadline: the take ran 601s past its deadline with no report")
		rules(w, on())

		pr := w.s.Work.Card("s1-1")
		require.Equal(t, 3, pr.Int("attempt"), "three attempts ran")
		bb, at := AtBriefBound(pr, "", 3)
		require.True(t, at, "the cap is reached: every attempt counts toward max_attempts")
		assert.Contains(t, bb.String(), "harness fault", "worded as the harness's")
		assert.Contains(t, bb.String(), "2 ended on the harness, 1 on a worker's report", "two of the three ended on the harness: fewer than cap votes")
		assert.NotContains(t, bb.String(), "the brief is wrong", "never a vote the brief is wrong")
		assert.Empty(t, pr.F(FieldBriefDefect), "no brief_defect stamp")
		for _, o := range openOf(w, NBriefWrong, "s1-1") {
			assert.NotContains(t, o.Note.What, "the brief is wrong", "no judgment says the brief is wrong")
		}
		p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Carry: true, Fix: "the reader's named fix"})
		assert.Empty(t, p.Refused, "rework --carry accepted: %+v", p.Refused)
	})

	t.Run("two worker not-done ends with the same finding: marked a brief defect", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		finished(w, "s1-1", false)
		finding := "a.go:12 drops the error from Close; return it"
		brokenOnce(t, w, finding)
		rules(w, on())
		brokenOnce(t, w, finding)
		rules(w, on())

		briefWrong := openOf(w, NBriefWrong, "s1-1")
		require.Len(t, briefWrong, 1, "the same finding twice goes to the coordinator")
		assert.Contains(t, briefWrong[0].Note.What, "the brief is wrong", "a repeated worker finding is the brief's")
		assert.NotEmpty(t, w.s.Work.Card("s1-1").F(FieldBriefDefect), "marked a brief defect, as today")
	})

	t.Run("a lander refusal of the same way twice: marked", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
		pr := w.s.Work.Placed("s1-1")
		pr.Col = Merging
		pr.Fields[FieldRuleRefused] = RefusedConflict
		w.s.Work.Put(pr)
		w.s.Merge.Put(&Card{ID: "s1-1", Row: "s1", Col: Stuck, Fields: map[string]string{}})
		w.s.Merge.Put(&Card{ID: CtlID("s1"), Row: "s1", Col: Ctl, Fields: map[string]string{
			"state": StreamStopped, "cause": "conflict", "card": "s1-1", FieldConflictKind: "file"}})
		a := RuleAnswer{open: Open{Note: Note{Kind: Judgment, Type: NConflict, Stream: "s1",
			What: "the head does not merge: CONFLICT (content): Merge conflict in internal/x.go"}}}
		ruleConflict(w.s, &a)
		assert.Equal(t, RuleBriefDefect, a.Rule, "the same refusal way twice is the brief-defect rule")
		assert.Equal(t, ActMark, a.Act, "marked, as today")
	})

	t.Run("a harness end then an ok take: nothing raised", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
		dealWith(w)
		takeAndFinish(t, w, WorkCardID("s1-1", 1), harnessPushRefused)
		rules(w, on())
		dealWith(w)
		id := WorkCardID("s1-1", 2)
		wc := w.s.Fleet.Card(id)
		require.NotNil(t, wc, "the rework is dealt")
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{id}}, Gens: gensOf(w.s, id)}))
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{id}}, Gens: gensOf(w.s, id), Head: answerHead, Report: "LAND: done, the tests pass"}))
		rules(w, on())
		assert.Empty(t, openOf(w, NHarnessFault, "s1-1"), "a harness end then an ok take raises nothing")
	})

	// tla/SprintRules.tla AnLaneBound, reversed witness MCSprintRulesAnswersBrokenLaneBrokenMarks:
	// four harness ends in a row, each followed by the seat's rework (a decision), raise the
	// lane-broken judgment with one attempt since the last decision, below the cap; red at
	// release/v1.2.26 23997aa9, where ruleBrief marked it a brief defect on no vote.
	t.Run("a broken lane across the seat's decisions: left, never marked a brief defect", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
		w.s.MaxAttempts = "4"
		dealWith(w)
		for i := 1; i <= LaneBrokenBound; i++ {
			takeAndFinish(t, w, WorkCardID("s1-1", i), harnessPushRefused)
			if i < LaneBrokenBound {
				w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "again"}))
				dealWith(w)
			}
		}
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, "3", pr.F(FieldDecidedAttempt), "the seat decided at attempt 3: one attempt since, below the cap")
		open := openOf(w, NBriefWrong, "s1-1")
		require.Len(t, open, 1, "the lane-broken judgment")
		require.Contains(t, open[0].Note.What, "the lane is broken, not the brief")

		rules(w, on())
		pr = w.s.Work.Card("s1-1")
		assert.Empty(t, pr.F(FieldBriefDefect), "no brief_defect stamp: no attempt of the run is a vote")
		assert.NotContains(t, pr.F(FieldRuleAnswer), ActMark, "the brief rule never marks a broken lane")
	})
}
