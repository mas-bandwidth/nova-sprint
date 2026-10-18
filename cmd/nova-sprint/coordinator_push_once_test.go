package main

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
)

// A pass over a sprint that has not changed since the last full pass began is skipped: one
// short look on the line (the epoch, the seat, the inbox's end, the open notes) and nothing
// else. Anything the pass depends on changing, a failed send, or coordPushLook since the last
// full pass brings the full pass back. Before (2026-10-17), every log line ran a full pass.
func TestAPassOverAnUnchangedSprintIsSkipped(t *testing.T) {
	t.Parallel()
	const name = "push-skip"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ctx := context.Background()
	full := func() bool {
		if ta.a.coordPassSkip(ctx) {
			return false
		}
		require.True(t, ta.a.coordinatorPass(ctx))
		return true
	}
	require.True(t, full(), "the first pass is full")
	ta.step(coordPushFloor)
	before := maps.Clone(ta.m.Calls)
	require.True(t, ta.a.coordPassSkip(ctx), "nothing changed: skipped")
	assert.Equal(t, 1, ta.m.Calls["open"]-before["open"], "the skip reads the open notes once, and runs no step")
	assert.Equal(t, before["acquire"], ta.m.Calls["acquire"])

	ta.step(coordPushLook - coordPushFloor - time.Second)
	require.False(t, full(), "still within coordPushLook of the last full pass")
	ta.step(time.Second)
	require.True(t, full(), "coordPushLook since the last full pass: the backstop runs it")
	ta.step(coordPushFloor)
	require.False(t, full())

	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "a-new-judgment", What: "new", At: ta.a.now()}))
	require.NoError(t, err)
	ta.step(coordPushFloor)
	require.True(t, full(), "a new note: full")

	ta.a.coord.mu.Lock()
	ta.a.coord.failed = true
	ta.a.coord.mu.Unlock()
	ta.step(coordPushFloor)
	assert.False(t, ta.a.coordPassSkip(ctx), "a failed send to the holder is never skipped")
}

// Passes start at least coordPushFloor apart, however busy the log; a pass a while after the
// last starts at once.
func TestPassesStartAtLeastASecondApart(t *testing.T) {
	t.Parallel()
	const name = "push-floor"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ctx := context.Background()
	start := ta.a.now()
	began := ta.a.coordPushOnce(ctx, start)
	assert.Equal(t, start.Add(coordPushFloor), began, "a pass right after the last waits for the floor")
	later := ta.a.now()
	began = ta.a.coordPushOnce(ctx, later.Add(-10*time.Second))
	assert.Equal(t, later, began, "a pass long after the last starts at once")
}

// An overdue judgment is the deadline lane's; its keys stay told, so when its group comes
// back from overdue (its oldest note closed, a later note's deadline now the group's) the notes
// already told are not told again. Before (2026-10-17), the keys were forgotten while the group
// was overdue, and the whole group was told again each time it came back: one group 36 times.
func TestAnOverdueGroupIsNotToldAgainWhenItComesBack(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	g := sprint.Group{Kind: sprint.Judgment, ID: "g1", Type: "judgment-x", What: "x", Notes: []string{"n1", "n2"}, Due: now.Add(time.Minute)}
	l := coordLook{holder: "seat", groups: []sprint.Group{g}}
	told := map[string]bool{}
	sends := coordFresh(told, l, now)
	require.Len(t, sends, 1)
	for _, k := range sends[0].keys {
		told[k] = true
	}

	l.groups[0].Due = now.Add(-time.Second) // overdue
	require.Empty(t, coordFresh(told, l, now))
	assert.Len(t, told, 2, "an overdue group's keys stay told")

	l.groups[0].Due = now.Add(time.Hour) // back from overdue, the same notes
	assert.Empty(t, coordFresh(told, l, now), "not told again")

	l.groups[0].Notes = []string{"n1", "n2", "n3"}
	sends = coordFresh(told, l, now)
	require.Len(t, sends, 1, "a new note in it is told")
	assert.Equal(t, []string{noteKey("n3")}, sends[0].keys)
}

// overdueRig is a sprint with a holder and one judgment past its deadline, the owner glenn.
func overdueRig(t *testing.T, name string) (*testApp, *bytes.Buffer) {
	t.Helper()
	ta, _, _ := coordSprint(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "owed-decision", What: "owed", At: ta.a.now()}))
	require.NoError(t, err)
	ta.step(sprint.DeadlineJudgment)
	var log bytes.Buffer
	ta.a.serveLog = &log
	return ta, &log
}

// The owner's lane refused by the bus ("glenn is no known name", 2026-10-17) is sent again
// after a minute, then two, and so on to thirty, and said once until its text changes; the
// coordinator's lane is not held back by it. Before, the refused send and its line went every
// pass: two thirds of the server's log.
func TestARefusedDecisionDeliveryBacksOffAndIsSaidOnce(t *testing.T) {
	t.Parallel()
	const name = "owed-seat"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	owner := 0
	b := &tokenBus{}
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		if m.To[0] == "glenn" {
			owner++
			return &bus.Refusal{Problems: []string{"glenn is no known name"}}
		}
		return b.send(ctx, m, say)
	}
	ta.a.decisionPass(ctx)
	require.Equal(t, 1, owner)
	require.Len(t, b.delivered, 1, "the coordinator is told")
	for range 11 {
		ta.step(5 * time.Second)
		ta.a.decisionPass(ctx)
	}
	assert.Equal(t, 1, owner, "no send to the owner within the minute")
	ta.step(busRetryFirst - 55*time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 2, owner, "sent again at a minute")
	ta.step(2*busRetryFirst - time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 2, owner, "then two minutes")
	ta.step(time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 3, owner)
	assert.Equal(t, 1, strings.Count(log.String(), "DECISIONS OWED: owner delivery pending"), "said once: %s", log.String())
	assert.Contains(t, log.String(), "the bus refused it: sent again in 1m0s")

	// a bus that takes it again delivers it at its next due look, and the wait is gone
	ta.a.bus = b.send
	ta.step(busRetryCap)
	ta.a.decisionPass(ctx)
	assert.Len(t, b.delivered, 2, "the owner is told")
	assert.Empty(t, ta.a.coord.decisions)
}

// A send that went under its token before is a delivery, as the coordinator push counts it
// (busMessageWent): the receipt is recorded and the lane sends it no more.
func TestADecisionDeliveryThatWentBeforeIsDelivered(t *testing.T) {
	t.Parallel()
	const name = "went-seat"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	sends := 0
	ta.a.bus = func(context.Context, bus.Message, func(string)) error {
		sends++
		return &bus.Refusal{Problems: []string{"the token \"decisions:x\" sent 01M before: the message went, and is not sent again"}}
	}
	ta.a.decisionPass(ctx)
	require.Equal(t, 2, sends)
	ta.step(5 * time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 2, sends, "delivered: not sent again")
	assert.NotContains(t, log.String(), "delivery pending")
}

// With no holder of the seat the coordinator's lane prepares nothing and says nothing; the
// owner's lane goes on.
func TestNoHolderPreparesNoCoordinatorDelivery(t *testing.T) {
	t.Parallel()
	const name = "no-holder"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	require.NoError(t, ta.m.SetCoordinator(ctx, ""))
	b := &tokenBus{}
	ta.a.bus = b.send
	ta.a.decisionPass(ctx)
	require.Len(t, b.delivered, 1)
	assert.Equal(t, []string{"glenn"}, b.delivered[0].To)
	open, err := ta.m.OpenNotes(ctx)
	require.NoError(t, err)
	for _, o := range open {
		if d := o.Note.DecisionDelivery; d != nil {
			assert.NotEqual(t, "coordinator", d.Lane, "no coordinator delivery is prepared")
		}
	}
	assert.NotContains(t, log.String(), "no coordinator channel")

}
