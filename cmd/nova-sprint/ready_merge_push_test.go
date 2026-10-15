package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
)

// The "ready to merge" happened note, sent to the coordinator on the bus, names only the
// card newly queued and the group's count, one line: a group that has accepted more
// before does not re-list them. It is not sent again once told.
func TestReadyToMergePushNamesOnlyTheNewCard(t *testing.T) {
	t.Parallel()
	const name = "hook-ready-merge"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)

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

	sent := ta.pass(name)
	require.Len(t, sent, 1, "%+v", sent)
	require.Contains(t, sent[0].Body, "s1-2", "the message names the newly queued card: %s", sent[0].Body)
	require.NotContains(t, sent[0].Body, "s1-1", "the message does not re-list earlier accepted cards: %s", sent[0].Body)
	require.Contains(t, sent[0].Body, "x2", "the message carries the group's count: %s", sent[0].Body)

	for _, m := range ta.pass(name) {
		require.NotContains(t, m.Body, "ready to merge", "a told happened note is not sent again: %s", m.Body)
	}
}
