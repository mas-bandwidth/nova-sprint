package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/steps"
)

func TestFunctionalNovaBusAdapter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := testRedisBus(t)
	path := filepath.Join(t.TempDir(), "received")
	hookTestName(t, fixture, "alice", `printf '%s\t%s\n' "$NOVA_BUS_ID" "$NOVA_BUS_BODY" >> `+steps.ShellWord(path))
	bus := fixture.edge()
	lease, err := SprintHook{Bin: fixture.bin, Env: fixture.env, Reply: "/usr/bin/true"}.Acquire(ctx, "sprint")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	require.NoError(t, lease.Check(ctx))
	id, err := bus.Send(ctx, "alice", "DEAL", "take a card")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(path)
		return e == nil && strings.Contains(string(b), id+"\ttake a card")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, lease.Close(ctx))
	require.Eventually(t, func() bool {
		names, e := bus.Who(ctx)
		_, held := names["sprint"]
		return e == nil && !held
	}, 5*time.Second, 25*time.Millisecond)
}
