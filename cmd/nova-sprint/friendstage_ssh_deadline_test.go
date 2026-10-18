package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// friend stage never prompts and never hangs on an unreachable or prompting
// bench: every ssh runs BatchMode=yes and a ConnectTimeout of 10 seconds before
// the host, and the whole stage runs under one 10-minute bound, so the context
// the runner seam receives carries that deadline and the verb help says the
// stage gives up after it. No socket and no host fact: the runner is the seam,
// and no test waits.
func TestFriendStageSSHNeverPromptsAndHasADeadline(t *testing.T) {
	args := friendSSHArgs("m1", "true")
	require.Equal(t, []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "m1", "true"}, args,
		"ssh is handed BatchMode and ConnectTimeout before the host and the script")
	hostAt := slices.Index(args, "m1")
	require.Greater(t, hostAt, 0)
	assert.Contains(t, args[:hostAt], "BatchMode=yes")
	assert.Contains(t, args[:hostAt], "ConnectTimeout=10")

	assert.Contains(t, friendStageWords, "gives up after")
	assert.Contains(t, friendStageWords, friendStageBound.String(), "the help names the bound the stage runs under")

	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	var ctxs []context.Context
	prev := friendStageRun
	friendStageRun = func(ctx context.Context, op friendOp) (string, error) {
		ctxs = append(ctxs, ctx)
		if op.Read {
			return "", &os.PathError{Path: filepath.Join(op.Dir, "JOB.md"), Op: "open", Err: os.ErrNotExist}
		}
		return "", nil
	}
	defer func() { friendStageRun = prev }()

	ta.ok("friend stage s1-1.w1@1 --as friend.amy --bench m1 --dir " + filepath.Join(t.TempDir(), "jobs"))
	require.NotEmpty(t, ctxs, "the stage went through the runner seam")
	for _, ctx := range ctxs {
		dl, ok := ctx.Deadline()
		require.True(t, ok, "the runner receives a context with a deadline")
		left := time.Until(dl)
		assert.LessOrEqual(t, left, friendStageBound,
			"the stage's deadline is no later than %s from its start", friendStageBound)
		assert.Greater(t, left, friendStageBound-time.Minute,
			"the stage's deadline is the bound, not an earlier one")
	}
}
