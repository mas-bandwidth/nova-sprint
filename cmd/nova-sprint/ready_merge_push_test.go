//go:build functional

// The hook's pushes on a live hook session (hook_test.go's hookOn), read within its
// real-time bounds. A functional test (RULES 6a: no real time in a unit test).
package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The "ready to merge" happened push names only the card newly queued, and the
// group's count, one line: a group that has accepted more before does not
// re-list them. It is not re-pushed on reconnect once acknowledged.
func TestReadyToMergePushNamesOnlyTheNewCard(t *testing.T) {
	t.Parallel()
	const name = "hook-ready-merge"
	ta, _ := hookSprint(t, name)
	first := ta.hookOn(name)
	first.next("hello")
	challenge := first.next("challenge").Challenge
	require.Equal(t, "ok", first.say(hookMsg{T: "prove", Challenge: challenge}).T)

	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	now := ta.a.now()
	inject := func(id string, at time.Time) {
		_, err := st.Run(ctx, store.Step{Verb: "persist ready to merge", Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{
				Kind: sprint.Happened, Type: sprint.NReadyToMerge, Stream: "s1", To: name,
				Count: 1, Who: sprint.MachineActor, At: at,
				What: fmt.Sprintf("1 accepted and queued to merge: %s; run: nova-sprint land --stream s1", id),
			}}}
		}})
		require.NoError(t, err)
	}
	inject("s1-1", now.Add(-2*time.Minute))
	inject("s1-2", now.Add(-time.Minute))

	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(ctx)
	p := first.next("push")
	require.Contains(t, p.Text, "s1-2", "the push names the newly queued card: %s", p.Text)
	require.NotContains(t, p.Text, "s1-1", "the push does not re-list earlier accepted cards: %s", p.Text)
	require.Contains(t, p.Text, "x2", "the push carries the group's count: %s", p.Text)
	require.Equal(t, "ok", first.say(hookMsg{T: "ack", ID: p.ID}).T)

	// On reconnect the acknowledged happened push is not re-pushed.
	require.NoError(t, first.conn.Close())
	ta.waitHook(sprint.HookGone)
	second := ta.hookOn(name)
	second.next("hello")
	challenge = second.next("challenge").Challenge
	require.Equal(t, "ok", second.say(hookMsg{T: "prove", Challenge: challenge}).T)
	s = ta.hookSession()
	require.NotNil(t, s)
	s.look(ctx)
	for _, rp := range collectHookPushes(second) {
		require.NotContains(t, rp.Text, "ready to merge", "an acknowledged happened push is not re-pushed: %s", rp.Text)
	}
}

// hook --quiet-happened: the seat receives judgments and challenges only, not
// the informational HAPPENED pushes.
func TestHookQuietHappenedOmitsInformationalPushes(t *testing.T) {
	t.Parallel()
	const name = "hook-quiet"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	challenge := c.next("challenge").Challenge
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: challenge}).T)

	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	_, err = st.Run(ctx, store.Step{Verb: "persist happened", Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NReadyToMerge, Stream: "s1", To: name,
			Count: 1, Who: sprint.MachineActor, At: ta.a.now(),
			What: "1 accepted and queued to merge: s1-1; run: nova-sprint land --stream s1"}}}
	}})
	require.NoError(t, err)

	s := ta.hookSession()
	require.NotNil(t, s)
	s.quietHappened = true
	s.look(ctx)
	for _, p := range collectHookPushes(c) {
		require.NotContains(t, p.Text, "ready to merge", "quiet-happened delivered an informational push: %s", p.Text)
	}
}
