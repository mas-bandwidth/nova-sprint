package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dNeedsLibfuzzer is a worker's HOLD naming the tool its member lacks.
const dNeedsLibfuzzer = "HOLD\nNEEDS-FACT: libfuzzer\n\nno libFuzzer on this machine: the gate cannot run"

// TestAMissingToolParksTheCardOnAFact: a finish whose report holds NEEDS-FACT parks the
// CARD on the fact, not the member: the card goes back to ready held to the fact, its
// attempt counts toward no bound or cap, the member is not held, and other cards are
// still dealt to it (env_wait.go; tla/WaitFor.tla FinishEnvMissing; docs/SPEC-SPRINT.md
// section 5, the fleet).
func TestAMissingToolParksTheCardOnAFact(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 2, 1, "m1")
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 2}}))
	first, second := w.s.Work.Card("s1-1").F("work"), w.s.Work.Card("s1-2").F("work")
	require.NotEmpty(t, first)
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first)}))

	attempt := w.s.Work.Card("s1-1").Int("attempt")
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first),
		Failed: true, Report: dNeedsLibfuzzer}))

	c := w.s.Fleet.Card(first)
	assert.Equal(t, Withdrawn, c.Col, "the work card withdraws for the deal")
	assert.Empty(t, c.F(FieldTakeEnded), "no redeal is spent")
	assert.Equal(t, Ready, w.state("s1-1"), "its primary is still ready, waiting on the fact")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "its attempt is unchanged")
	assert.Zero(t, w.s.Work.Card("s1-1").Int("failed"), "a missing tool is never the card's failure")
	assert.Equal(t, "libfuzzer", w.s.Work.Card("s1-1").F(FieldEnvFacts), "the card is held to the fact")
	assert.NotEmpty(t, w.s.Work.Card("s1-1").F(FieldEnvWaitUntil), "the card has an environment deadline")
	assert.False(t, w.s.EnvHeld("m1"), "the member is not held: the fault was the tool's, not the member's")
	assert.Empty(t, w.notesOf(NMemberEnvFault), "no member fault judgment")
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")

	// other cards still go to the member
	assert.Equal(t, Ready, w.s.Fleet.Card(second).Col, "the second card is still ready")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{second}}, Gens: w.gens(second)}))
	assert.Equal(t, Working, w.s.Fleet.Card(second).Col, "the member still takes another card")
}

// TestAnEnvWaitResumesWhenABeatBringsTheFact: a member's beat carrying the fact writes it
// on its control card and the deal gives the parked card to that member in the same
// tick, and to no member without it before (env_wait.go; tla/WaitFor.tla AutoResumeCard
// and FactAppears; docs/SPEC-SPRINT.md section 5, the fleet).
func TestAnEnvWaitResumesWhenABeatBringsTheFact(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1")
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	first := w.s.Work.Card("s1-1").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first),
		Failed: true, Report: dNeedsLibfuzzer}))

	// no fact on the member's beat: the card is not dealt
	w.part(TickPresence, TickReq{Beats: map[string]Beat{"m1": {At: w.s.Now, Facts: []string{"linux", "go"}}}})
	w.part(TickDeal, TickReq{})
	assert.Equal(t, Withdrawn, w.s.Fleet.Card(WorkCardID("s1-1", w.s.Work.Card("s1-1").Int("attempt"))).Col,
		"a member without the fact is dealt the parked card by no path")

	// the beat brings the fact: the deal resumes it in this tick
	w.part(TickPresence, TickReq{Beats: map[string]Beat{"m1": {At: w.s.Now, Facts: []string{"linux", "go", "libfuzzer"}}}})
	w.part(TickDeal, TickReq{})
	wc := w.s.Fleet.Card(WorkCardID("s1-1", w.s.Work.Card("s1-1").Int("attempt")))
	require.NotNil(t, wc, "the parked card is dealt in the tick the beat carries the fact")
	assert.Equal(t, Ready, wc.Col, "the parked card is dealt in the tick the beat carries the fact")
	assert.Equal(t, "m1", wc.Row)
}

// TestAnEnvWaitPastItsDeadlineIsAJudgmentThatTakesAWait: at the deadline one NEnvWait
// judgment names the card, its decisions take wait, and wait is accepted on it
// (env_wait.go; tla/WaitFor.tla Overdue and ServerDefault; docs/SPEC-SPRINT.md section 5,
// the fleet).
func TestAnEnvWaitPastItsDeadlineIsAJudgmentThatTakesAWait(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1")
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	first := w.s.Work.Card("s1-1").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first),
		Failed: true, Report: dNeedsLibfuzzer}))
	w.tick(EnvWaitFor)

	w.part(TickOverdue, TickReq{})
	notes := w.notesOf(NEnvWait)
	require.Len(t, notes, 1, "one NEnvWait at the deadline: %v", notes)
	assert.Contains(t, notes[0].Decisions, "wait")
	assert.Empty(t, WaitRefusalIn(w.s, notes[0]), "the card waits on the environment, not a decision")
	until := w.s.Now.Add(30 * time.Minute)
	w.must(Wait(w.s, WaitReq{Note: notes[0].ID, Until: until, Who: w.s.Coordinator}))
}

// TestTheCoordinatorEnvWaitsForAFactOnAHeldCard: a card held by a judgment that refuses a
// plain wait (NBriefWrong) and one held by a server-default hold take the coordinator's
// wait on an environment fact: the holding judgment is closed, the hold released, and
// the card parked on the fact with the coordinator's deadline (env_wait.go;
// tla/WaitFor.tla FinishEnvMissing and ServerDefault; docs/SPEC-SPRINT.md section 5,
// the fleet).
func TestTheCoordinatorEnvWaitsForAFactOnAHeldCard(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 1, "m1")
	w.note(judgment(NBriefWrong, "s1", w.s.Now, 0, "s1-1"))
	require.Len(t, w.notesOf(NBriefWrong), 1)
	until := w.s.Now.Add(2 * time.Hour)
	w.must(EnvWaitOnCard(w.s, EnvWaitReq{Card: "s1-1", Facts: []string{"libfuzzer"}, Until: until, Who: w.s.Coordinator}))
	c := w.s.Work.Card("s1-1")
	assert.Equal(t, Ready, c.Col)
	assert.Equal(t, "libfuzzer", c.F(FieldEnvFacts))
	assert.Equal(t, stamp(until), c.F(FieldEnvWaitUntil))
	for _, o := range w.s.Open {
		assert.NotEqual(t, NBriefWrong, o.Note.Type, "the holding judgment is closed")
	}
	assert.Empty(t, c.F(FieldHeld), "no hold is left")

	// a server-default hold, by fields, as TickDecisionDefaults writes it
	w2 := fleetWorld(t, 1, 1, "m1")
	h := w2.s.Work.Card("s1-1")
	h.Fields[FieldHeld] = stamp(w2.s.Now)
	h.Fields["held_by"] = ServerDefaultActor
	h.Fields["held_reason"] = "unanswered"
	h.Fields["held_judgment"] = "j-old"
	h.Fields["decision_held_from"] = Ready
	w2.must(EnvWaitOnCard(w2.s, EnvWaitReq{Card: "s1-1", Facts: []string{"libfuzzer", "linux"}, Until: until, Who: w2.s.Coordinator}))
	c2 := w2.s.Work.Card("s1-1")
	assert.Equal(t, Ready, c2.Col)
	assert.Empty(t, c2.F(FieldHeld))
	assert.Empty(t, c2.F("held_by"))
	assert.Equal(t, "libfuzzer,linux", c2.F(FieldEnvFacts))
	assert.Equal(t, stamp(until), c2.F(FieldEnvWaitUntil))
}
