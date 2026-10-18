package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

func standbySeatSilentRig(t *testing.T, b *tokenBus) (*testApp, *store.Store) {
	t.Helper()
	ta := newTestApp(t)
	ta.mu.Lock()
	ta.now = time.Unix(1000, 0)
	ta.mu.Unlock()
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == busRedisEnv {
			return "bus.test:6379"
		}
		return env(k)
	}
	ta.ok("init --readers reader-a,reader-b,reader-c --members m1 --owner glenn")
	ta.a.bus = b.send
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "coordinator"}
	_, err := st.Run(context.Background(), store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NSeatSilent, StreamLevel: true,
		Primaries: []string{"s1-1"}, What: "the seat is not answering: 1 judgments hold 1 cards past their deadline", Decisions: []string{sprint.SeatSilentDecision}}))
	require.NoError(t, err)
	return ta, st
}

type standbyShapesFail struct {
	store.Backend
	fail bool
}

func (b *standbyShapesFail) AtEpoch(epoch uint64, old bool) store.Backend {
	return &standbyShapesFail{Backend: b.Backend.AtEpoch(epoch, old), fail: b.fail}
}

func (b *standbyShapesFail) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	if b.fail {
		return nil, errors.New("work table shapes unavailable")
	}
	return b.Backend.Shapes(ctx, tables)
}

func TestASeatSilentShapeReadFailureLeavesNeedBlockedDeliveryIndependent(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st := standbySeatSilentRig(t, b)
	_, err := st.Run(context.Background(), store.NoteStep("need blocker", sprint.Note{
		Kind: sprint.Judgment, Type: sprint.NNeedBlocked, StreamLevel: true,
		Primaries: []string{"s1-2"}, What: "a need is blocked",
	}))
	require.NoError(t, err)
	shapes := &standbyShapesFail{Backend: st.B, fail: true}
	st.B = shapes
	var log bytes.Buffer
	ta.a.serveLog = &log

	ta.a.tellSeatSilent(context.Background(), st)
	require.Len(t, b.delivered, 1, "the independent blocked-need group still reaches the holder")
	assert.Equal(t, sprint.NNeedBlocked, b.delivered[0].Subject)
	assert.Equal(t, []string{"coordinator"}, b.delivered[0].To)
	assert.Contains(t, log.String(), "standby seat setting could not be read")

	shapes.fail = false
	ta.pastTheLook()
	ta.a.tellSeatSilent(context.Background(), st)
	require.Len(t, b.delivered, 2, "the seat-silent raise is retried when its setting can be read")
	assert.Equal(t, sprint.NSeatSilent, b.delivered[1].Subject)
	assert.Equal(t, []string{"coordinator"}, b.delivered[1].To)
}

func TestASeatSilentNoStandbyLoginFailureAdvisesOncePerRaise(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st := standbySeatSilentRig(t, b)
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		switch k {
		case busUserEnv:
			return "coordinator"
		case busPasswordEnvEnv:
			return "BAD NAME"
		default:
			return env(k)
		}
	}
	var log bytes.Buffer
	ta.a.serveLog = &log

	ta.a.tellSeatSilent(context.Background(), st)
	assert.Empty(t, b.attempts, "an unresolved login never reaches the mocked bus")
	assert.Equal(t, 1, bytes.Count(log.Bytes(), []byte("no standby seat hears this alarm")))
	assert.Equal(t, 1, bytes.Count(log.Bytes(), []byte("coordinator's bus login is unresolved")))

	ta.pastTheLook()
	ta.a.tellSeatSilent(context.Background(), st)
	assert.Empty(t, b.attempts)
	assert.Equal(t, 1, bytes.Count(log.Bytes(), []byte("no standby seat hears this alarm")), "the advisory is once per raise even when login resolution fails")
	assert.Equal(t, 2, bytes.Count(log.Bytes(), []byte("coordinator's bus login is unresolved")), "login failure is retried on the next look")
}

func TestASeatSilentRaiseReachesTheStandbySeat(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st := standbySeatSilentRig(t, b)
	ta.ok("set --seat-silent-to spare")
	_, err := st.Run(context.Background(), store.NoteStep("need blocker", sprint.Note{
		Kind: sprint.Judgment, Type: sprint.NNeedBlocked, StreamLevel: true,
		Primaries: []string{"s1-2"}, What: "a need is blocked",
	}))
	require.NoError(t, err)

	ta.a.tellSeatSilent(context.Background(), st)
	require.Len(t, b.delivered, 2)
	bySubject := map[string][]string{}
	for _, m := range b.delivered {
		bySubject[m.Subject] = m.To
	}
	assert.Equal(t, []string{"coordinator", "spare"}, bySubject[sprint.NSeatSilent])
	assert.Equal(t, []string{"coordinator"}, bySubject[sprint.NNeedBlocked], "a blocked need can be acted on by the coordinator")
}

func TestASeatSilentRaiseWithNoStandbySaysSoOnce(t *testing.T) {
	t.Parallel()
	b := &tokenBus{}
	ta, st := standbySeatSilentRig(t, b)
	var log bytes.Buffer
	ta.a.serveLog = &log

	ta.a.tellSeatSilent(context.Background(), st)
	ta.pastTheLook()
	ta.a.tellSeatSilent(context.Background(), st)

	require.Len(t, b.delivered, 1, "the raise is still sent to the silent seat once")
	assert.Equal(t, []string{"coordinator"}, b.delivered[0].To)
	assert.Equal(t, 1, bytes.Count(log.Bytes(), []byte("no standby seat hears this alarm")), "the advisory is once per raise across looks")
	assert.Contains(t, log.String(), "nova-sprint set --seat-silent-to <bus name>")
}
