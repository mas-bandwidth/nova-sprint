package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	sprintstore "github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
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

	start := time.Unix(1000, 0)
	prevTimeout := friendStageWithTimeout
	friendStageWithTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		assert.Equal(t, friendStageBound, timeout)
		return friendStageDeadlineContext{parent: parent, deadline: start.Add(timeout)}, func() {}
	}
	defer func() { friendStageWithTimeout = prevTimeout }()

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
		left := dl.Sub(start)
		assert.LessOrEqual(t, left, friendStageBound,
			"the stage's deadline is no later than %s from its start", friendStageBound)
		assert.Greater(t, left, friendStageBound-time.Minute,
			"the stage's deadline is the bound, not an earlier one")
	}
}

// The bounded context must cover store acquisition too. The fake backend
// records the deadline it receives and fails as an expired acquisition; no
// socket or external store is used.
func TestFriendStageStoreAcquisitionUsesBoundedContext(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	start := time.Unix(1000, 0)
	done := make(chan struct{})
	close(done)
	prevTimeout := friendStageWithTimeout
	friendStageWithTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		assert.Equal(t, friendStageBound, timeout)
		return friendStageDeadlineContext{parent: parent, deadline: start.Add(timeout), done: done, err: context.DeadlineExceeded}, func() {}
	}
	defer func() { friendStageWithTimeout = prevTimeout }()

	var sawDeadline bool
	prevBackend := ta.a.backend
	ta.a.backend = func(ctx context.Context, _ string, _ sprint.Names) (sprintstore.Backend, error) {
		deadline, ok := ctx.Deadline()
		sawDeadline = ok && deadline.Equal(start.Add(friendStageBound))
		return nil, context.DeadlineExceeded
	}
	defer func() { ta.a.backend = prevBackend }()

	code, _, stderr := ta.do("friend stage s1-1.w1@1 --as friend.amy --bench m1")
	assert.Equal(t, 1, code, "an expired acquisition is refused")
	assert.True(t, sawDeadline, "store acquisition receives the single bounded context")
	assert.Contains(t, stderr, "gave up after "+friendStageBound.String()+" on m1")
}

// friendStageDeadlineContext exposes a test-clock deadline without starting a
// timer or consulting wall time.
type friendStageDeadlineContext struct {
	parent   context.Context
	deadline time.Time
	done     <-chan struct{}
	err      error
}

func (c friendStageDeadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }
func (c friendStageDeadlineContext) Done() <-chan struct{}       { return c.done }
func (c friendStageDeadlineContext) Err() error                  { return c.err }
func (c friendStageDeadlineContext) Value(key any) any           { return c.parent.Value(key) }

// An expired context during the first store read is a timeout refusal, not a
// generic store-read failure. The store and clock are fake; no SSH runs.
func TestFriendStageExpiredContextDuringStoreReadIsRefused(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	start := time.Unix(1000, 0)
	done := make(chan struct{})
	close(done)
	prevTimeout := friendStageWithTimeout
	friendStageWithTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		assert.Equal(t, friendStageBound, timeout)
		return friendStageDeadlineContext{parent: parent, deadline: start.Add(-time.Second), done: done, err: context.DeadlineExceeded}, func() {}
	}
	defer func() { friendStageWithTimeout = prevTimeout }()

	prevBackend := ta.a.backend
	ta.a.backend = func(context.Context, string, sprint.Names) (sprintstore.Backend, error) {
		return sprintstore.NewMem(), nil // an uninitialized fake makes the first read fail
	}
	defer func() { ta.a.backend = prevBackend }()

	code, _, stderr := ta.do("friend stage s1-1.w1@1 --as friend.amy")
	assert.Equal(t, 1, code, "an expired stage is refused")
	assert.Contains(t, stderr, "gave up after "+friendStageBound.String()+" on local")
}
