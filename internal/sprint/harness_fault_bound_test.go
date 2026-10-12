package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A harness fault is reworked on the same tier at most once per card (HarnessFaultBounded;
// docs/SPEC-SPRINT.md section 8, the rules table's row failed): the second identical harness
// fault on a card, or the same harness-fault class on RuleSameFailureCards cards within
// HarnessFaultWindow, raises one NHarnessFault judgment; the card waits for it and is not
// redealt. These drive the real Take and Finish path in the in-memory store.

// harnessPushRefused is the failure of the card's two takes in the pushed.
const harnessPushRefused = "push refused: the result's head does not descend from the staged commit"

// harnessCardBrief is a machine card brief.
const harnessCardBrief = "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task."

// takeTheFinish is one take and finish of the attempt's work card, failed with the report.
func takeAndFinish(t *testing.T, w *world, id, report string) {
	t.Helper()
	wc := w.s.Fleet.Card(id)
	require.NotNil(t, wc, "the attempt is dealt")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{id}}, Gens: gensOf(w.s, id)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{id}}, Gens: gensOf(w.s, id), Failed: true, Report: report}))
}

func TestARepeatedHarnessFaultIsDealtTwiceNotThriceAndRaisesTheJudgment(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, harnessCardBrief)
	dealWith(w)
	takeAndFinish(t, w, WorkCardID("s1-1", 1), harnessPushRefused)
	rules(w, on())
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, 1, pr.Int("reworks"), "the first harness fault is reworked once, on its tier")
	assert.Empty(t, openOf(w, NHarnessFault, "s1-1"), "the first is not the repeated one")

	dealWith(w)
	takeAndFinish(t, w, WorkCardID("s1-1", 2), harnessPushRefused)
	rules(w, on())
	// the second identical harness fault is dealt twice, not three times: judged, not reworked
	assert.Equal(t, 1, w.s.Work.Card("s1-1").Int("reworks"), "no third rework")
	require.Len(t, openOf(w, NHarnessFault, "s1-1"), 1, "the second identical harness fault raises the judgment")
	assert.Equal(t, Review, w.s.Work.Card("s1-1").Col, "the card waits for it")
	assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-1", 3)), "not dealt a third time")
	for _, o := range openOf(w, NHarnessFault, "s1-1") {
		assert.Contains(t, o.Note.What, "push refused", "names the class")
		assert.Contains(t, o.Note.What, "s1-1", "names the card")
	}
	// the deal part sees the harness block's own record, never the generic bound the finish
	// overrode: the harness judgment alone holds the card, no bound judgment beside it
	dealWith(w)
	assert.Empty(t, openOf(w, NBound, "s1-1"), "the harness judgment alone holds the card: no bound judgment beside it")
	assert.Len(t, openOf(w, NHarnessFault, "s1-1"), 1, "and the harness judgment stands")
}

// TestThreeCardsWithTheSameHarnessFaultRaiseOneGroupedJudgment drives the real tick end (the
// deadlines part raises the fleet's failure and the grouped harness-fault judgment, the rules
// answer the failed work, the deal redeals what the rules moved). On the broken design the
// grouped judgment named no card, so the three cards were reworked and redealt; here it holds
// every card as a primary, and none is redealt until it is answered.
func TestThreeCardsWithTheSameHarnessFaultRaiseOneGroupedJudgment(t *testing.T) {
	t.Parallel()
	var briefs []string
	for range RuleSameFailureCards {
		briefs = append(briefs, harnessCardBrief)
	}
	w := friendWorld(t, briefs...)
	dealWith(w)
	for i := 1; i <= RuleSameFailureCards; i++ {
		takeAndFinish(t, w, WorkCardID("s1-"+itoa(i), 1), harnessPushRefused)
	}
	// the real tick end: deadlines (with the fleet's failure and the grouped harness-fault
	// judgment), then the rules, then the next tick's deal.
	w.part(tickDeadlinesAndFleetFailure, on())
	rules(w, on())
	// one event, one judgment: the grouped harness-fault judgment alone, never a fleet-failure
	// beside it (the harness-fault group covers the fleet's failure).
	var grouped []Open
	for _, o := range w.s.Open {
		if o.Note.Type == NHarnessFault {
			grouped = append(grouped, o)
		}
	}
	require.Len(t, grouped, RuleSameFailureCards, "the fleet-wide case is one grouped judgment, open on each of its cards")
	require.Empty(t, openOf(w, NFleetFailure, "s1-1"), "no fleet-failure judgment beside the harness-fault group")
	assert.Len(t, distinctJudgments(w.s.Open, NHarnessFault), 1, "exactly one harness-fault judgment of any kind")
	for i := 1; i <= RuleSameFailureCards; i++ {
		assert.Contains(t, grouped[0].Note.Primaries, "s1-"+itoa(i), "it names each card as a primary")
		assert.Contains(t, grouped[0].Note.What, "s1-"+itoa(i), "it names each card")
	}
	assert.Contains(t, grouped[0].Note.What, "push refused", "it names the class")

	// every card it names is held: none is reworked to attempt 2, and none is redealt
	for i := 1; i <= RuleSameFailureCards; i++ {
		pr := w.s.Work.Card("s1-" + itoa(i))
		assert.Equal(t, Review, pr.Col, "%s waits for the judgment", pr.ID)
		assert.Equal(t, 1, pr.Int("attempt"), "%s is not reworked to attempt 2", pr.ID)
		assert.Equal(t, 0, pr.Int("reworks"), "%s is not reworked", pr.ID)
	}
	dealWith(w)
	for i := 1; i <= RuleSameFailureCards; i++ {
		assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-"+itoa(i), 2)), "s1-%d is not redealt until the judgment is answered", i)
	}
}

// distinctJudgments is the distinct open judgment notes of the type, by note id: a grouped
// judgment open on several cards is one.
func distinctJudgments(open []Open, typ string) []Note {
	seen := map[string]bool{}
	var out []Note
	for _, o := range open {
		if o.Note.Kind == Judgment && o.Note.Type == typ && !seen[o.Note.ID] {
			seen[o.Note.ID] = true
			out = append(out, o.Note)
		}
	}
	return out
}

// Answering the grouped judgment releases the card it answers: a brief (the rebrief decision)
// closes the harness-fault judgment on that card and moves it to its next attempt, while the
// cards still held stay held.
func TestAnsweringTheGroupedHarnessFaultReleasesTheCardItAnswers(t *testing.T) {
	t.Parallel()
	var briefs []string
	for range RuleSameFailureCards {
		briefs = append(briefs, harnessCardBrief)
	}
	w := friendWorld(t, briefs...)
	dealWith(w)
	for i := 1; i <= RuleSameFailureCards; i++ {
		takeAndFinish(t, w, WorkCardID("s1-"+itoa(i), 1), harnessPushRefused)
	}
	w.part(tickDeadlinesAndFleetFailure, on())
	rules(w, on())
	require.Len(t, distinctJudgments(w.s.Open, NHarnessFault), 1, "the grouped judgment is open")

	next := "c: the work (s1)\nREPO: mas-bandwidth/nova-tools\n\nThe task, re-cut from the base tip.\n"
	w.must(Brief(w.s, BriefReq{ID: "s1-1", Brief: next, Who: "coordinator"}))
	assert.Empty(t, openOf(w, NHarnessFault, "s1-1"), "a brief answers and closes the judgment on the card")
	assert.Equal(t, Ready, w.s.Work.Card("s1-1").Col, "the card moves to its next attempt")
	require.Len(t, distinctJudgments(w.s.Open, NHarnessFault), 1, "the one grouped judgment is still open on the cards not yet answered")
	for i := 2; i <= RuleSameFailureCards; i++ {
		assert.Len(t, openOf(w, NHarnessFault, "s1-"+itoa(i)), 1, "s1-%d is still held", i)
	}
}

func TestAHarnessFaultFollowedByAnOkTakeRaisesNothing(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, harnessCardBrief)
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
	assert.Empty(t, openOf(w, NHarnessFault, "s1-1"), "an ok take after a harness fault raises nothing")
}
