package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

func TestTheBlockerJudgmentGoesToTheOwnerAtOnce(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b,reader-c --members m1 --owner amy")
	ta.a.bus = b.send

	cfg := config.NewMem()
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"width": "2", "tiers": "flash", "mode": "batch"}}, "t")
	require.NoError(t, err)
	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) { return cfg.List(ctx, config.KindFriend) }

	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "coordinator"}
	_, _, _, err = st.SyncFriends(context.Background(), []store.FriendSpec{{Name: "amy", Width: 2, Class: "pro"}})
	require.NoError(t, err)
	_, err = st.Run(context.Background(), store.NoteStep("test", sprint.Note{
		Kind:      sprint.Judgment,
		Type:      sprint.NNeedBlocked,
		Primaries: []string{"a-route-may-be-frontier"},
		What:      "a-route-may-be-frontier (bound: j1 bound, open 3h) blocks 3 cards: muse-is-a-fleet-harness (tools-v1217), a-funded-provider-is-a-config-row-and-always-shown (tools-v1217), the-server-unit-carries-the-bus-address (tools-v1217); decide: rework on a higher tier, drop",
		Decisions: []string{"rework on a higher tier", "drop"},
	}))
	require.NoError(t, err)

	ctx := context.Background()
	// First tick / look:
	ta.a.tellSeatSilent(ctx, st)
	require.Len(t, b.delivered, 1, "the first tick that raises NNeedBlocked sends one message to the owner")
	assert.Contains(t, b.delivered[0].Body, "a-route-may-be-frontier")
	assert.Contains(t, b.delivered[0].Body, "muse-is-a-fleet-harness")
	assert.Equal(t, []string{"amy"}, b.delivered[0].To)

	// Second tick / look:
	ta.pastTheLook()
	ta.a.tellSeatSilent(ctx, st)
	assert.Len(t, b.delivered, 1, "the next tick sends none")
}
