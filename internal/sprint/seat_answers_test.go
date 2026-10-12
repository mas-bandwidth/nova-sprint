package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A card-holding judgment is never left unanswered (the owner, 2026-10-12: "This must never
// happen again."; docs/SPEC-SPRINT.md section 8, "A judgment that holds a card"). Work that
// came back failed, unanswered past its deadline, raises the one sprint-level judgment "the
// seat is not answering", once, naming the card it holds; it is raised again (and the held
// judgment with it) at the next deadline; neither wait nor ack quiets it; and it clears when
// the judgment is answered.
func TestAFailedWorkJudgmentUnansweredRaisesTheSeatNotAnsweringOnce(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 1, 0)
	r.tickBeating(time.Second, "m1", "m2")
	wc := r.takeOne("m1")
	r.must(store.FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: []string{wc}}, Gens: map[string]int{wc: r.snap().Fleet.Card(wc).Int("gen")}, Failed: true, Who: "m1"}))
	failed := r.openNote(sprint.NWorkFailed, "s1-1")
	require.NotNil(t, failed, "work came back failed: a judgment that holds s1-1")
	sprintWide := sprint.StreamSubject("")

	// within its deadline the seat has time: no alarm
	r.tickBeating(sprint.DeadlineJudgment/2, "m1", "m2")
	require.Nil(t, r.openNote(sprint.NSeatSilent, sprintWide), "no alarm within the deadline")

	// past its deadline unanswered: the seat is not answering, naming the card held
	r.tickBeating(sprint.DeadlineJudgment/2+time.Minute, "m1", "m2")
	alarm := r.openNote(sprint.NSeatSilent, sprintWide)
	require.NotNil(t, alarm, "past its deadline the seat-not-answering judgment is raised")
	assert.Contains(t, alarm.Primaries, "s1-1", "it names the card held")
	assert.Contains(t, alarm.What, "s1-1 held ", alarm.What)
	assert.Contains(t, alarm.What, "("+sprint.NWorkFailed+")", alarm.What)
	assert.Equal(t, []string{sprint.SeatSilentDecision}, alarm.Decisions, "no ack, no wait: the answers alone quiet it")

	// it cannot be waited, nor acknowledged, by any path
	_, _, err := r.st.Wait(r.ctx, alarm.ID, r.st.Now().Add(time.Hour))
	require.Error(t, err, "the store's wait")
	assert.Contains(t, err.Error(), "cannot be waited")
	res, err := r.st.Run(r.ctx, store.WaitStep(sprint.WaitReq{Note: alarm.ID, Until: r.st.Now().Add(time.Hour), Who: "coordinator"}))
	require.NoError(t, err)
	require.NotEmpty(t, res.Refused, "the wait step refuses it")
	assert.Contains(t, res.Refused[0].Why, "cannot be waited")
	res, err = r.st.Run(r.ctx, store.AckStep(sprint.AckReq{Notes: []string{alarm.ID}, Reason: "later", Who: "coordinator"}))
	require.NoError(t, err)
	require.NotEmpty(t, res.Refused, "the ack step refuses it")
	assert.Contains(t, res.Refused[0].Why, "cannot be acknowledged")

	// raised once: later ticks keep the one judgment, and at the next deadline it and the
	// held judgment are raised again as pushes to the seat
	for range 3 {
		r.tickBeating(time.Second, "m1", "m2")
	}
	r.tickBeating(sprint.DeadlineJudgment, "m1", "m2")
	again := r.openNote(sprint.NSeatSilent, sprintWide)
	require.NotNil(t, again)
	assert.Equal(t, alarm.ID, again.ID, "one judgment while it holds, never one a tick")
	assert.Equal(t, 1, alarmsWritten(r), "the alarm is written once")
	assert.GreaterOrEqual(t, r.countNotes(sprint.Happened, sprint.NRaisedAgain, alarm.ID+" ("+sprint.NSeatSilent+")"), 1, "the alarm is raised again at the next deadline")
	assert.GreaterOrEqual(t, r.countNotes(sprint.Happened, sprint.NRaisedAgain, failed.ID+" ("+sprint.NWorkFailed+") still holds s1-1"), 1, "the held judgment is raised again at its next deadline")

	// answered, it clears
	r.must(store.ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Fix: "handle the empty case", Who: "coordinator"}))
	r.tickBeating(time.Second, "m1", "m2")
	assert.Nil(t, r.openNote(sprint.NWorkFailed, "s1-1"), "the rework answered it")
	assert.Nil(t, r.openNote(sprint.NSeatSilent, sprintWide), "the alarm clears when the judgment is answered")
}

// alarmsWritten is the seat-not-answering judgments the sprint wrote, by id.
func alarmsWritten(r *holdRig) int {
	ids := map[string]bool{}
	for _, n := range r.allNotes() {
		if n.Kind == sprint.Judgment && n.Type == sprint.NSeatSilent {
			ids[n.ID] = true
		}
	}
	return len(ids)
}

// A card hard-pinned to a friend who is held waits for her only until the judgment's
// deadline: then it is offered on (to a friend up or the fleet), never left to the seat (the owner,
// 2026-10-12: "a card pinned to one friend waits while she is not up" held its card 31
// minutes unanswered). Before, the pin held for an hour.
func TestAHardPinToAHeldFriendIsReleasedAtItsDeadline(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 0, 0)
	r.tickBeating(time.Second, "m1", "m2")
	r.hold(sprint.HoldReq{Names: []string{"amy"}, Reason: "out of credits"})
	r.must(store.AddStep(sprint.AddReq{Stream: "p1", Cards: []sprint.CardAdd{{ID: "p1-1", Brief: friendsBrief("only friend amy")}}}))
	r.tickBeating(time.Second, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	require.NotNil(t, r.openNote(sprint.NStopPinWaits, "p1-1"), "the pin waits on her: the stop")

	r.tickBeating(sprint.DeadlineJudgment/2, "m1", "m2")
	require.Equal(t, sprint.Ready, r.snap().Work.Card("p1-1").Col, "within the deadline the pin waits for her")

	r.tickBeating(sprint.DeadlineJudgment/2+time.Second, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	got := r.snap().Work.Card("p1-1")
	require.Equal(t, sprint.Working, got.Col, "past the judgment's deadline the pin is released: dealt on")
	wc := r.snap().Fleet.Card(sprint.WorkCardID("p1-1", 1))
	require.NotNil(t, wc)
	assert.NotEqual(t, sprint.FriendRow("amy"), wc.Row, "dealt to someone up, never to the held friend")
}
