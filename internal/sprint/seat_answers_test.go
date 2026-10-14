package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A card hard-pinned to a friend who is held is NOT answered by the machine at the judgment's
// deadline. Until 2026-10-13 the machine offered it on then (to a friend up or the fleet), a
// safe default for a card any worker can do; a card pinned to one friend by name is not that
// (WHO: only friend <name> is hers alone, docs/SPEC-SPRINT.md, WHO preference), and a research
// card pinned to one friend was done by another. The pin-waits stop is how the seat sees and
// answers it (ack, bring her up, unpin).
func TestAHardPinToAHeldFriendIsNotAnsweredAtTheJudgmentDeadline(t *testing.T) {
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
	require.Equal(t, sprint.Ready, got.Col, "past the judgment's deadline the pin still waits for her")
	assert.Nil(t, r.snap().Fleet.Card(sprint.WorkCardID("p1-1", 1)), "dealt to no one: bob up with room, the machines idle")
}

// The seat's own answer to the stop releases the pin: unpin removes the WHO line, so the card
// is anyone's and the next tick deals it on. The one release there is, and it is a person's.
func TestUnpinIsTheOnlyReleaseOfAHardPinToAHeldFriend(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 0, 0)
	r.tickBeating(time.Second, "m1", "m2")
	r.hold(sprint.HoldReq{Names: []string{"amy"}, Reason: "out of credits"})
	r.must(store.AddStep(sprint.AddReq{Stream: "p1", Cards: []sprint.CardAdd{{ID: "p1-1", Brief: friendsBrief("only friend amy")}}}))
	r.tickBeating(time.Second, "m1", "m2")
	r.tickBeating(3*sprint.DeadlineJudgment, "m1", "m2")
	require.Equal(t, sprint.Ready, r.snap().Work.Card("p1-1").Col)

	r.must(store.UnpinStep(sprint.UnpinReq{IDs: []string{"p1-1"}, Reason: "anyone may do it", Who: "coordinator"}))
	r.tickBeating(time.Second, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	require.Equal(t, sprint.Working, r.snap().Work.Card("p1-1").Col, "unpinned, it is dealt on")
	wc := r.snap().Fleet.Card(sprint.WorkCardID("p1-1", 1))
	require.NotNil(t, wc)
	assert.NotEqual(t, sprint.FriendRow("amy"), wc.Row, "never to the held friend")
}

// Hours the machine was STOPPED change nothing about a hard pin either: a sprint resumed
// after a long stop deals the card to no one but its friend.
func TestAHardPinIsStillHeldAfterTheMachineWasStopped(t *testing.T) {
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
	require.Equal(t, sprint.Ready, r.snap().Work.Card("p1-1").Col, "the pin still waits for her")

	r.tickBeating(sprint.DeadlineJudgment, "m1", "m2")
	r.tickBeating(time.Second, "m1", "m2")
	require.Equal(t, sprint.Ready, r.snap().Work.Card("p1-1").Col, "and past the deadline in running time too")
}
