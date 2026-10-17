package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
)

// knownNamesBus is a bus that knows only the coordinator names of the test and
// refuses any other From or To with an error that says "is no known name",
// recording every attempt and every refusal.
type knownNamesBus struct {
	names    map[string]bool
	attempts []bus.Message
	refusals []error
}

func (b *knownNamesBus) send(_ context.Context, m bus.Message, _ func(string)) error {
	b.attempts = append(b.attempts, m)
	for _, n := range append([]string{m.From}, m.To...) {
		if !b.names[n] {
			err := fmt.Errorf("%s is no known name", n)
			b.refusals = append(b.refusals, err)
			return err
		}
	}
	return nil
}

// The server's owner pushes (the seat-silent and need-blocked raises of
// tellSeatSilent, and the decisions-owed owner lane) go to whoever holds the
// coordinator seat at push time, sent as the coordinator's bus login. After a
// handoff the next push goes to the new holder; a bus that knows only the
// coordinators' names refuses none of them (owner-pushes-route-to-coordinator).
func TestOwnerPushRoutesToCoordinator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ta := newTestApp(t)
	env := map[string]string{
		"NOVA_SPRINT_REDIS": "mem:0",
		"NOVA_SPRINT_ACTOR": "coordinator",
		busRedisEnv:         "bus.test:6379",
	}
	ta.a.getenv = func(k string) string { return env[k] }
	ta.a.serveAddr = "mem:0"
	ta.ok("init --readers reader-a --members m1 --owner glenn --coordinator seat-a")
	b := &knownNamesBus{names: map[string]bool{"seat-a": true, "seat-b": true}}
	ta.a.bus = b.send
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "coordinator"}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)

	overdue := func(typ string) {
		_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: typ, What: typ, At: ta.a.now()}))
		require.NoError(t, err)
	}
	seatSilentRaise := func() {
		_, err := st.Run(ctx, store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NSeatSilent, StreamLevel: true, Primaries: []string{"s1-1"}, What: "the seat is not answering: 1 judgments hold 1 cards past their deadline", Decisions: []string{sprint.SeatSilentDecision}}))
		require.NoError(t, err)
	}

	overdue("decision-a")
	seatSilentRaise()
	ta.step(sprint.DeadlineJudgment)
	ta.a.decisionPass(ctx)
	ta.a.tellSeatSilent(ctx, st)

	require.NotEmpty(t, b.attempts)
	for _, m := range b.attempts {
		assert.Equal(t, []string{"seat-a"}, m.To, "every owner push goes to the coordinator holder")
		assert.NotEqual(t, "nova-sprint", m.From)
		assert.NotEqual(t, "glenn", m.From)
	}
	assert.Empty(t, b.refusals, "the bus knows the coordinators: nothing is refused")

	handoff := len(b.attempts)

	require.NoError(t, ta.m.SetCoordinator(ctx, "seat-b"))
	ta.pastTheLook()
	ta.step(31 * time.Minute)
	overdue("decision-b")
	seatSilentRaise()
	ta.a.decisionPass(ctx)
	ta.a.tellSeatSilent(ctx, st)

	require.Greater(t, len(b.attempts), handoff)
	for _, m := range b.attempts[handoff:] {
		assert.Equal(t, []string{"seat-b"}, m.To, "after handoff every owner push goes to the new holder")
		assert.NotEqual(t, "nova-sprint", m.From)
		assert.NotEqual(t, "glenn", m.From)
	}
	assert.Empty(t, b.refusals)
}
