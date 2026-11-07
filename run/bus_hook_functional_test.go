package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFunctionalNovaBusAdapter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := testRedisBus(t)
	redis := ""
	for _, v := range fixture.env {
		if strings.HasPrefix(v, "NOVA_BUS_REDIS=") {
			redis = strings.TrimPrefix(v, "NOVA_BUS_REDIS=")
		}
	}
	path := filepath.Join(t.TempDir(), "received")
	hookTestName(t, fixture, "alice", `printf '%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_BODY" >> `+shellWord(path))
	bus, err := NewNovaBus(BusConfig{Binary: fixture.bin, Redis: redis, ReplyExecutable: "/usr/bin/true"})
	require.NoError(t, err)
	lease, err := bus.Acquire(ctx, "sprint")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	require.NoError(t, lease.Check(ctx))
	receipt, err := bus.Send(ctx, "alice", "DEAL", "take a card")
	require.NoError(t, err)
	require.Equal(t, "alice", receipt.Recipient)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), receipt.ID+"\ttake a card")
	}, 5*time.Second, 25*time.Millisecond)
	reply, err := bus.ReplyTo(ctx, "alice", receipt.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), reply.ID+"\t"+"replies are verbs: run nova-sprint next --as alice")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, lease.Close(ctx))
	require.Eventually(t, func() bool {
		names, e := bus.Who(ctx)
		if e != nil {
			return false
		}
		for _, n := range names {
			if n == "sprint" {
				return false
			}
		}
		return true
	}, 5*time.Second, 25*time.Millisecond)
}
