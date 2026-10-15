package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CoordinatorPush.tla: SameSetNotResentWithin and ChangedSetWarnedImmediately.
// The store, sender and clock are all fakes; the READY machine creates no timed
// judgments of its own. Only successful warning sends start the repeat interval.
func TestCoordinatorPushFoldThirtyMinutesAndSetChanges(t *testing.T) {
	t.Parallel()
	const name = "push-fold"
	const subject = "nova-sprint: judgments waiting on the coordinator"
	ta, _, _ := coordSprint(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	add := func(typ string) {
		t.Helper()
		_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: typ, What: typ, At: ta.a.now(), Review: ta.a.now().Add(4 * time.Hour), ReviewSet: ta.a.now()}))
		require.NoError(t, err)
		ta.step(sprint.JudgmentWarnAfter) // the added judgment enters the warning-eligible set
	}
	closeNote := func(typ string) {
		t.Helper()
		_, err := st.Run(ctx, store.Step{Verb: "fixture close", Plan: func(s *sprint.Snapshot) sprint.Plan {
			p := sprint.Plan{}
			for _, o := range s.Open {
				if o.Note.Type == typ {
					p.Closes = append(p.Closes, o)
				}
			}
			return p
		}})
		require.NoError(t, err)
	}
	warnings := func() []bus.Message {
		t.Helper()
		var out []bus.Message
		for _, m := range ta.pass(name) {
			if m.Subject == subject {
				out = append(out, m)
			}
		}
		return out
	}
	add("fold-a")
	add("fold-b")
	require.Len(t, warnings(), 1)
	for minute := 1; minute <= 29; minute++ {
		ta.step(time.Minute)
		require.Empty(t, warnings(), "unchanged minute %d must stay quiet", minute)
	}
	ta.step(time.Minute)
	repeat := warnings()
	require.Len(t, repeat, 1, "exactly 30m since success permits one repeat")
	assert.Equal(t, "WARNING 2 judgment(s) waiting on the coordinator 10s or longer: j1 j2\n", repeat[0].Body, "the unchanged set repeats only its summary line")
	require.Empty(t, warnings(), "a second look at the same instant stays quiet")
	ta.step(time.Second)
	closeNote("fold-b")
	removed := warnings()
	require.Len(t, removed, 1, "a removal sends at once")
	assert.Contains(t, removed[0].Body, "WARNING 1 judgment(s)")
	assert.Contains(t, removed[0].Body, "j1 ")
	assert.NotContains(t, removed[0].Body, "j2 ")
	ta.step(time.Second)
	add("fold-c")
	require.Len(t, warnings(), 1, "an addition sends at once")

	closeNote("fold-c")
	send := ta.a.bus
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("fake refused send") }
	ta.a.coordinatorPass(ctx)
	ta.a.bus = send
	ta.step(time.Second)
	require.Len(t, warnings(), 1, "a failed changed-set warning retries next look")
	assert.False(t, ta.a.coord.failed)
	for range 29 {
		ta.step(time.Minute)
		require.Empty(t, warnings())
	}
	ta.step(time.Minute)
	require.Len(t, warnings(), 1, "repeat clock starts at the successful retry")
	closeNote("fold-a")
	require.Empty(t, warnings(), "nothing waiting needs no warning")
	add("fold-a")
	require.Len(t, warnings(), 1, "the first warning after empty is immediate")
}

func TestCoordinatorPushFoldUsesIDsNotOrderOrAliases(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 15, 11, 35, 0, 0, time.UTC)
	seen := map[string]bool{"a": true, "b": true}
	ws := []sprint.JudgmentWarning{{ID: "b", Alias: "j9"}, {ID: "a", Alias: "j8"}}
	assert.False(t, sprint.JudgmentWarnDue(ws, seen, at, at.Add(time.Minute)))
	assert.True(t, sprint.JudgmentWarnDue(ws, seen, at, at.Add(30*time.Minute)))
	assert.True(t, sprint.JudgmentWarnDue(ws[:1], seen, at, at.Add(time.Minute)), "removal changes the set")
}
