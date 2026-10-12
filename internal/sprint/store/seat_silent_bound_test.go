package store

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A card-holding judgment is never left unanswered (the owner, 2026-10-12: "This must never
// happen again."; docs/SPEC-SPRINT.md section 8, "A judgment that holds a card"). A card at
// its bound is a mind's judgment (no rule reworks it on the tier it is on): unanswered past
// its deadline it raises the one sprint-level judgment "the seat is not answering", once,
// naming the card it holds; it is raised again (and the bound with it) at the next deadline;
// neither wait nor ack quiets it; and it clears when the bound is answered.
func TestABoundJudgmentUnansweredRaisesTheSeatNotAnsweringOnce(t *testing.T) {
	t.Parallel()
	h := flashAndPro(t)
	h.addReady("s1", 1, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	h.boundBy("s1-1", noResultLine)
	bound := h.openOf(sprint.NBound)
	require.Len(t, bound, 1, "the card reached its bound: a judgment that holds s1-1")
	held := bound[0].Note

	// within its deadline the seat has time: no alarm
	h.tick(sprint.DeadlineJudgment / 2)
	h.machine()
	require.Empty(t, h.openOf(sprint.NSeatSilent), "no alarm within the deadline")

	// past its deadline unanswered: the seat is not answering, naming the card held
	h.tick(sprint.DeadlineJudgment/2 + time.Minute)
	h.machine()
	open := h.openOf(sprint.NSeatSilent)
	require.Len(t, open, 1, "past its deadline the seat-not-answering judgment is raised")
	alarm := open[0].Note
	assert.Contains(t, alarm.Primaries, "s1-1", "it names the card held")
	assert.Contains(t, alarm.What, "s1-1 held ", alarm.What)
	assert.Contains(t, alarm.What, "("+sprint.NBound+")", alarm.What)
	assert.Equal(t, []string{sprint.SeatSilentDecision}, alarm.Decisions, "no ack, no wait: the answers alone quiet it")

	// it cannot be waited, nor acknowledged, by any path
	_, _, err := h.st.Wait(h.ctx, alarm.ID, h.st.Now().Add(time.Hour))
	require.Error(t, err, "the store's wait")
	assert.Contains(t, err.Error(), "cannot be waited")
	res := h.run(WaitStep(sprint.WaitReq{Note: alarm.ID, Until: h.st.Now().Add(time.Hour), Who: h.st.Actor}))
	require.NotEmpty(t, res.Refused, "the wait step refuses it")
	assert.Contains(t, res.Refused[0].Why, "cannot be waited")
	res = h.run(AckStep(sprint.AckReq{Notes: []string{alarm.ID}, Reason: "later", Who: h.st.Actor}))
	require.NotEmpty(t, res.Refused, "the ack step refuses it")
	assert.Contains(t, res.Refused[0].Why, "cannot be acknowledged")
	// nor is the bound it names: a wait would park its card (wait_refused.go)
	_, _, err = h.st.Wait(h.ctx, held.ID, h.st.Now().Add(time.Hour))
	require.Error(t, err, "the bound holds its card: no wait")

	// raised once: later ticks keep the one judgment, and at the next deadline it and the
	// held judgment are raised again as pushes to the seat
	for range 3 {
		h.tick(time.Second)
		h.machine()
	}
	h.tick(sprint.DeadlineJudgment)
	h.machine()
	again := h.openOf(sprint.NSeatSilent)
	require.Len(t, again, 1)
	assert.Equal(t, alarm.ID, again[0].Note.ID, "one judgment while it holds, never one a tick")
	assert.Equal(t, 1, alarmsWrittenIn(h), "the alarm is written once")
	assert.GreaterOrEqual(t, raisedAgain(h, alarm.ID+" ("+sprint.NSeatSilent+")"), 1, "the alarm is raised again at the next deadline")
	assert.GreaterOrEqual(t, raisedAgain(h, held.ID+" ("+sprint.NBound+") still holds s1-1"), 1, "the bound is raised again at its next deadline")

	// answered, it clears
	require.Empty(t, h.reworkOf("handle the empty case", "pro"), "the bound's answer: a rework on the tier above")
	h.tick(time.Second)
	h.machine()
	assert.Empty(t, h.openOf(sprint.NBound), "the rework answered it")
	assert.Empty(t, h.openOf(sprint.NSeatSilent), "the alarm clears when the judgment is answered")
}

// alarmsWrittenIn is the seat-not-answering judgments the sprint wrote, by id.
func alarmsWrittenIn(h *harness) int {
	ids := map[string]bool{}
	for _, n := range h.tickNotes(sprint.NSeatSilent) {
		if n.Kind == sprint.Judgment {
			ids[n.ID] = true
		}
	}
	return len(ids)
}

// raisedAgain is the pushes raising a judgment again whose what says word.
func raisedAgain(h *harness, word string) int {
	n := 0
	for _, x := range h.tickNotes(sprint.NRaisedAgain) {
		if x.Kind == sprint.Happened && strings.Contains(x.What, word) {
			n++
		}
	}
	return n
}
