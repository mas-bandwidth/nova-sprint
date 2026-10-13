package main

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestEveryServerStartupRechecksWithoutVersionOrRollbackState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := store.NewMem()
	n := 0
	st := &store.Store{B: m, Actor: sprint.MachineActor, Now: func() time.Time { return t0 }, NewID: func() string { n++; return strconv.Itoa(n) }}
	require.NoError(t, st.Init(ctx))
	pinned, err := st.Pinned(ctx)
	require.NoError(t, err)
	a := &app{}
	for _, startup := range []string{"first rollout", "restart", "rollback"} {
		if startup != "first rollout" {
			_, _, _, err := st.SetMachine(ctx, startup == "restart")
			require.NoError(t, err)
		}
		_, err := st.Run(ctx, store.Step{Verb: "fixture judgment", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NWorkFailed, Primaries: []string{"absent"}, At: s.Now}}}
		}})
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, a.recheckStartedServer(ctx, pinned, &out), startup)
		assert.Contains(t, out.String(), "SERVER NOTE closed judgment", startup)
		open, err := m.OpenNotes(ctx)
		require.NoError(t, err)
		assert.Empty(t, open, startup)
		out.Reset()
		require.NoError(t, a.recheckStartedServer(ctx, pinned, &out))
		assert.Empty(t, out.String(), "a repeated startup creates no second closure")
	}
}

func TestAnEarlierServerStartupCannotCloseTheClearedSprintsJudgments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := store.NewMem()
	st := &store.Store{B: m, Actor: sprint.MachineActor, Now: func() time.Time { return t0 }, NewID: func() string { return "fixture" }}
	require.NoError(t, st.Init(ctx))
	pinned, err := st.Pinned(ctx)
	require.NoError(t, err)
	_, err = st.Clear(ctx)
	require.NoError(t, err)
	_, err = st.Run(ctx, store.Step{Verb: "fixture judgment", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NWorkFailed, Primaries: []string{"absent"}, At: s.Now}}}
	}})
	require.NoError(t, err)
	var out bytes.Buffer
	err = (&app{}).recheckStartedServer(ctx, pinned, &out)
	require.ErrorContains(t, err, "epoch")
	assert.Empty(t, out.String())
	open, err := m.OpenNotes(ctx)
	require.NoError(t, err)
	assert.Len(t, open, 1, "the new sprint's judgment remains open")
}

func TestARefusedListenerStartupDoesNotRecheckJudgments(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1 --readers reader-a")
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	_, err = st.Run(context.Background(), store.Step{Verb: "fixture judgment", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NWorkFailed, Primaries: []string{"absent"}, At: s.Now}}}
	}})
	require.NoError(t, err)
	code, out, errs := ta.do("run --listen 0.0.0.0:6390")
	assert.Equal(t, 2, code, out+errs)
	assert.Contains(t, errs, "--listen wants one address")
	assert.NotContains(t, out, "closed judgment")
	open, err := ta.m.OpenNotes(context.Background())
	require.NoError(t, err)
	assert.Len(t, open, 1, "listener refusal leaves the obsolete judgment open")
}
