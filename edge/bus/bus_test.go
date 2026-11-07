package bus

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEdgeSendAndWhoOverFakeExec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fex := edge.NewFakeExec()
	assert.Equal(t, int64(1000), fex.Now())

	b := New(Config{Exec: fex})

	// 1. Confirmed send parses `<to> <id>`.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 0,
		Stdout:   "amd-1.ds 1794000001001-0\n",
	})
	id, err := b.Send(ctx, "amd-1.ds", "DEAL", "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.NoError(t, err)
	assert.Equal(t, "1794000001001-0", id)

	// 2. Exit 0 without `<to> <id>` confirmation is refused so cursor never moves.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 0,
		Stdout:   "wrong-recipient 1794000001002-0\n",
	})
	_, err = b.Send(ctx, "amd-1.ds", "DEAL", "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing `<recipient> <id>` confirmation")

	// 3. Non-zero exit (`SEND FAILED`) is refused naming the fix.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 1,
		Stderr:   "SEND FAILED: sprint not hooked in\n",
	})
	_, err = b.Send(ctx, "amd-1.ds", "DEAL", "DEAL: 1 card for amd-1.ds (epoch 1)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nova-bus exited 1")
	assert.Contains(t, err.Error(), "nova-bus who")

	// 4. Who parses present rows and optional detail column.
	fex.Enqueue("nova-bus", edge.FakeOutcome{
		ExitCode: 0,
		Stdout:   "WHO OK present=2\namd-1.ds\t14s\nrowan\t5m\trecv holding 99-0\n",
	})
	who, err := b.Who(ctx)
	require.NoError(t, err)
	require.Len(t, who, 2)
	assert.Equal(t, Presence{Name: "amd-1.ds", Uptime: "14s"}, who["amd-1.ds"])
	assert.Equal(t, Presence{Name: "rowan", Uptime: "5m", Detail: "recv holding 99-0"}, who["rowan"])

	// 5. A body over the cap is refused before nova-bus runs.
	_, err = b.Send(ctx, "amd-1.ds", "DEAL", strings.Repeat("x", MaxMessageBytes+1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4096-byte cap")

	// 6. A who header that is not `WHO OK` is refused.
	fex.Enqueue("nova-bus", edge.FakeOutcome{ExitCode: 0, Stdout: "garbage\n"})
	_, err = b.Who(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WHO OK present=<n>")
}
