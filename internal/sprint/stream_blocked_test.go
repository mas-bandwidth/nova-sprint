package sprint_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// TestABlockedStreamIsNotAHeldStream pins the stream clock's two facts
// (a-blocked-stream-is-not-a-held-stream): Held is a coordinator hold only
// (FieldHeld); a stream whose every card waits and none moves is Blocked,
// with BlockedBy naming the sentinel or card of another stream its first
// waiting card waits on, else "needs". Both at once read both. On an
// in-memory store; no real time, sockets, network or services.
func TestABlockedStreamIsNotAHeldStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := store.NewMem()
	var mu sync.Mutex
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	n := 0
	st := &store.Store{B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now:   func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
		NewID: func() string { mu.Lock(); defer mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	require.NoError(t, st.Init(ctx))
	must := func(step store.Step) {
		t.Helper()
		res, err := st.Run(ctx, step)
		require.NoError(t, err, "%s", step.Verb)
		require.Empty(t, res.Refused, "%s refused", step.Verb)
	}
	clockOf := func(stream string) sprint.StreamClock {
		t.Helper()
		clocks, err := st.StreamClocks(ctx)
		require.NoError(t, err)
		for _, c := range clocks {
			if c.Stream == stream {
				return c
			}
		}
		t.Fatalf("no clock for %s in %v", stream, clocks)
		return sprint.StreamClock{}
	}

	// gates holds the sentinel every waiter below waits on.
	must(store.AddStep(sprint.AddReq{Stream: "gates", IDs: []string{"gate"}, Sentinel: true}))
	// waiters: every card waits on the gates stream's sentinel; nothing moves.
	must(store.AddStep(sprint.AddReq{Stream: "waiters", IDs: []string{"w1", "w2"}, Needs: []string{"gate"}}))
	// mixed: one card waits on the sentinel, one is ready and moves.
	must(store.AddStep(sprint.AddReq{Stream: "mixed", IDs: []string{"m1"}, Needs: []string{"gate"}}))
	must(store.AddStep(sprint.AddReq{Stream: "mixed", IDs: []string{"m2"}}))
	// helds: a ready stream the coordinator holds; nothing waits.
	must(store.AddStep(sprint.AddReq{Stream: "helds", IDs: []string{"h1"}}))
	// boths: every card waits on the sentinel, and the coordinator holds it too.
	must(store.AddStep(sprint.AddReq{Stream: "boths", IDs: []string{"b1"}, Needs: []string{"gate"}}))
	for _, s := range []string{"helds", "boths"} {
		_, err := st.Hold(ctx, sprint.HoldReq{Names: []string{s}, Kind: sprint.HoldStream, Reason: "pause", Who: "coordinator"})
		require.NoError(t, err, "hold %s", s)
	}

	blocked := clockOf("waiters")
	assert.True(t, blocked.Blocked, "every card waits and none moves: blocked")
	assert.Equal(t, "gate", blocked.BlockedBy, "blocked by the sentinel its first waiting card waits on")
	assert.False(t, blocked.Held, "waiting on another stream is not a coordinator hold")
	raw, err := json.Marshal(blocked)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"blocked":true`)
	assert.Contains(t, string(raw), `"blocked_by":"gate"`)
	assert.NotContains(t, string(raw), `"Held"`, "Held is absent for a merely blocked stream: %s", raw)

	mixed := clockOf("mixed")
	assert.False(t, mixed.Blocked, "a stream with a moving card is not blocked")
	assert.Empty(t, mixed.BlockedBy)
	assert.False(t, mixed.Held)

	held := clockOf("helds")
	assert.True(t, held.Held, "a coordinator hold reads Held")
	assert.False(t, held.Blocked, "a held stream whose cards do not all wait is not blocked")
	assert.Empty(t, held.BlockedBy)

	both := clockOf("boths")
	assert.True(t, both.Held, "held and blocked at once: Held reads")
	assert.True(t, both.Blocked, "held and blocked at once: Blocked reads")
	assert.Equal(t, "gate", both.BlockedBy)
	assert.True(t, strings.Contains(string(mustJSON(t, both)), `"blocked_by":"gate"`))
}

func mustJSON(t *testing.T, c sprint.StreamClock) []byte {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	return raw
}
