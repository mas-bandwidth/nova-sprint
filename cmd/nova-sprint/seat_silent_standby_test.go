package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
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
