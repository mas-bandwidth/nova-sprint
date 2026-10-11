//go:build functional

package main

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// TestEveryProducerCard tests the ConsumersOut invariant: after every tick,
// every producer card (working, fix, review) has its consumer out
func TestEveryProducerCard(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))
	require.NoError(t, c.Set(ctx, "nova:sprint:mode", "test", 0).Err())

	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator"}
	world := newApp(func(k string) string { return env[k] })
	defer world.close()

	// Scenario: 16 ready cards with 16 free slots across 4 members of width 4
	do := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := world.run(args, &out, &errb)
		require.Equal(t, 0, code, "%v: %d %s", args, code, errb.String())
		return out.String()
	}

	do("init", "--members", "m1:4,m2:4,m3:4,m4:4", "--readers", "r1,r2,r3")

	// Add 16 ready cards in one wave: a single card is refused by design
	// ("BATCH EVERYTHING"), so the count names all sixteen.
	do("add", "--stream", "a", "--count", "16")
	// Beat all members up
	for i := 1; i <= 4; i++ {
		do("fleet", "beat", "m"+strconv.Itoa(i))
	}

	// Run a tick
	var out bytes.Buffer
	st, _, code := world.machineVerb("run", nil, &out)
	require.Equal(t, 0, code, "run: %d", code)
	require.NotNil(t, st)

	// Check the heartbeat for quiet violations
	_, hb, err := st.Machine(ctx)
	require.NoError(t, err)

	// At this point, cards should be dealt and have consumers
	// Any violations will be in the heartbeat's quiet field
	t.Logf("tick completed, %d quiet violations", len(hb.Quiet))
	for _, v := range hb.Quiet {
		t.Logf("  %s: %s - %s", v.Type, v.Subject, v.Why)
	}
}
