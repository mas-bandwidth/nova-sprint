package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The attempt cap's spin (v1.6.16, 2026-10-24 10:18-10:19, rn-examples-macos-windows): a card's
// fourth attempt held only for a file outside its PATHS, and the widen rule edited its brief in
// place at attempt 4 and sent it to ready. A rule's widen is no coordinator decision, so the
// card stayed at its attempt cap and the deal raised the cap's judgment; the retire then took
// the widen's brief_attempt (4, at the judgment's attempt 4) for a re-brief and answered it
// "the card's move: ... was re-briefed at attempt 4", with nothing moved; the next tick's deal
// raised it again. Over 600 raise -> answer -> raise rounds in a minute, each a notice on the
// coordinator's hook. On the twin store (store.Mem), every rule on; the clock starts at 1000.

// capSpinT0 is the rig's clock at its start: 1000 seconds, and a second a tick from there.
var capSpinT0 = time.Unix(1000, 0).UTC()

// capJudgmentsRaised is every attempt-cap judgment ever raised on the card, open or answered.
func (r *conflictRig) capJudgmentsRaised(id string) []sprint.Note {
	r.t.Helper()
	all, _, err := r.m.NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	var out []sprint.Note
	for _, n := range all {
		if n.Kind == sprint.Judgment && n.Type == sprint.NBriefWrong && len(n.Primaries) > 0 && n.Primaries[0] == id {
			out = append(out, n)
		}
	}
	return out
}

// fourthAttemptWidened runs r-1 to its cap with the fourth attempt widened in place by rule:
// the deadline, a finding outside PATHS (widened), a finding inside PATHS (reworked), then a
// HOLD at attempt 4 only for a file outside PATHS (the widen rule's, as on 2026-10-24).
func (r *conflictRig) fourthAttemptWidened() {
	r.t.Helper()
	r.failedAttempt("r-1", deadlineReport)
	r.tick()
	require.Equal(r.t, 2, r.attemptOf("r-1"))
	r.brokenRead("r-1", "internal/y/b.go:40 still calls the old name, outside PATHS; rename the call too")
	r.tick()
	r.tick()
	require.Equal(r.t, 3, r.attemptOf("r-1"))
	r.brokenRead("r-1", "internal/x/a.go:12 drops the error from Close; return it")
	r.tick()
	require.Equal(r.t, 4, r.attemptOf("r-1"))
	r.held("r-1", "Verdict: HOLD\nHead: "+readHead+"\nThe change is done and green; it needs files outside PATHS: docs/NOTE.md.\n")
	r.tick()
}

func TestACardAtItsCapWidenedByRuleRaisesItsJudgmentOnceAndNeverSpins(t *testing.T) {
	t.Parallel()
	r := newConflictRigAt(t, capSpinT0)
	r.readCard()
	r.fourthAttemptWidened()
	pr := r.snap().Work.Card("r-1")
	require.Equal(t, 4, pr.Int("attempt"), "attempt 4 is its last without a decision")
	require.Equal(t, 4, sprint.AttemptsSince(pr), "a rule's widen is no decision: the cap holds")
	require.Equal(t, sprint.Ready, pr.Col, "widened in place by rule at the cap: ready")
	r.tick()
	// the finish raised the cap's judgment and the widen answered it, moving the card; the
	// deal raises it once more on the ready card, and from there nothing changes
	before := len(r.capJudgmentsRaised("r-1"))
	require.Len(t, r.openOnCard(sprint.NBriefWrong, "r-1"), 1, "the deal judged the ready card at its cap")
	for range 30 {
		r.tick()
	}
	raised := len(r.capJudgmentsRaised("r-1")) - before
	assert.Equal(t, 0, raised, "with nothing changed the cap's judgment is never raised again: never raise -> answer -> raise: %d more raised in 30 ticks", raised)
	assert.Len(t, r.openOnCard(sprint.NBriefWrong, "r-1"), 1, "and it is open: the card still needs the coordinator")
	assert.Equal(t, 4, r.attemptOf("r-1"), "no attempt past the cap")
	assert.Nil(t, r.snap().Fleet.Card(sprint.WorkCardID("r-1", 5)), "no attempt 5 is dealt without a decision")
}

// rework --carry answers the cap's judgment on a ready card: the judgment's own first decision
// (Decisions[NBriefWrong], DecisionCarry) is the verb the coordinator runs, and it is taken. Red
// at v1.6.16: "not review (it is ready)".
func TestReworkCarryAnswersTheCapOfAReadyCard(t *testing.T) {
	t.Parallel()
	r := newConflictRigAt(t, capSpinT0)
	r.readCard()
	r.fourthAttemptWidened()
	r.tick()
	require.Equal(t, sprint.Ready, r.snap().Work.Card("r-1").Col, "widened in place: ready")
	require.Len(t, r.openOnCard(sprint.NBriefWrong, "r-1"), 1)

	r.must(store.ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"r-1"}}, Carry: true, Fix: "carry on from the widened head", Who: "coordinator"}))
	pr := r.snap().Work.Card("r-1")
	assert.Equal(t, "4", pr.F(sprint.FieldDecidedAttempt), "the carry is the coordinator's decision at attempt 4")
	assert.Equal(t, 5, pr.Int("attempt"), "attempt 5 is dealt at once, from the carried head")
	assert.Equal(t, 1, sprint.AttemptsSince(pr), "the cap's count starts again from the decision")
	assert.Contains(t, pr.F("brief"), "CARRY: r-1 attempt 4 head="+readHead, "the widen's carried head is the one carried")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "r-1"), "the judgment is answered by the carry")
	r.tick()
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "r-1"), "and no cap judgment is raised again")
}
