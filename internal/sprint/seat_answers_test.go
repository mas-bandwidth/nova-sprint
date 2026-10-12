package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

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

// The pin's deadline is running time (Stella's read of #123): hours the machine was STOPPED
// do not count, so a sprint resumed after a long stop does not release every pin at once;
// the pin is released once the running time past its stop reaches the deadline. Before, the
// release read the wall clock, and the first tick after the resume dealt the card on.
func TestAPinIsNotReleasedByTheTimeTheMachineWasStopped(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 0, 0)
	r.tickBeating(time.Second, "m1", "m2")
	r.hold(sprint.HoldReq{Names: []string{"amy"}, Reason: "out of credits"})
	r.must(store.AddStep(sprint.AddReq{Stream: "p1", Cards: []sprint.CardAdd{{ID: "p1-1", Brief: friendsBrief("only friend amy")}}}))
	r.tickBeating(time.Second, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	require.NotNil(t, r.openNote(sprint.NStopPinWaits, "p1-1"), "the pin waits on her: the stop")
	r.tickBeating(sprint.DeadlineJudgment/2, "m1", "m2")

	// stopped for twice the deadline, then resumed
	_, _, _, err := r.st.SetMachine(r.ctx, false)
	require.NoError(t, err)
	r.mu.Lock()
	r.now = r.now.Add(2 * sprint.DeadlineJudgment)
	r.mu.Unlock()
	_, _, _, err = r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.tickBeating(time.Second, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	require.Equal(t, sprint.Ready, r.snap().Work.Card("p1-1").Col, "the stopped hours do not count: the pin still waits for her")

	// the rest of the deadline in running time: released, dealt on
	r.tickBeating(sprint.DeadlineJudgment/2, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	got := r.snap().Work.Card("p1-1")
	require.Equal(t, sprint.Working, got.Col, "past the deadline in running time the pin is released")
}
