package edge

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeExecClockAndProcessTable(t *testing.T) {
	t.Parallel()
	f := NewFakeExec()
	assert.Equal(t, int64(1000), f.Now())

	f.On("nova-bus", func(call FakeCall) FakeOutcome {
		return FakeOutcome{
			ExitCode: 0,
			Stdout:   "amd-1.ds 1794000001000-0\n",
		}
	})

	res, err := RunArgv(context.Background(), f, 5*time.Second, "/work", []string{"A=1"},
		"nova-bus", "send", "--as", "sprint", "--to", "amd-1.ds", "DEAL", "body")
	require.NoError(t, err)
	assert.True(t, res.OK())
	assert.Equal(t, "amd-1.ds 1794000001000-0\n", res.Stdout)
	assert.Equal(t, int64(1001), f.Now())

	calls := f.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, int64(1001), calls[0].At)
	assert.Equal(t, "/work", calls[0].Cmd.Dir)
	assert.Equal(t, []string{"nova-bus", "send", "--as", "sprint", "--to", "amd-1.ds", "DEAL", "body"}, calls[0].Cmd.Argv)
}

func TestFakeExecRefusalsNameProgramAndFix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := NewFakeExec()

	// 1. Empty argv is refused with program and fix.
	_, err := f.Run(ctx, Cmd{})
	var exErr *ExecError
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrInvalidArgv, exErr.Kind)
	assert.Contains(t, exErr.Error(), "pass a non-empty binary name")

	// 2. NUL byte in argv, dir, or env is refused.
	_, err = f.Run(ctx, Cmd{Argv: []string{"git", "status\x00bad"}})
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrInvalidArgv, exErr.Kind)
	assert.Contains(t, exErr.Error(), "exec git (invalid_argv)")
	assert.Contains(t, exErr.Error(), "remove NUL bytes")

	// 3. Missing nova-* binary names nova-tools / nova-fleet up.
	f.SetMissing("nova-bus", true)
	_, err = f.Run(ctx, Cmd{Argv: []string{"nova-bus", "who"}})
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrMissingBinary, exErr.Kind)
	assert.Equal(t, "nova-bus", exErr.Program)
	assert.Contains(t, exErr.Error(), "nova-fleet up")

	// 4. Missing gh binary names GitHub CLI and auth fix.
	_, err = f.Run(ctx, Cmd{Argv: []string{"gh", "pr", "view"}})
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrMissingBinary, exErr.Kind)
	assert.Equal(t, "gh", exErr.Program)
	assert.Contains(t, exErr.Error(), "GH_TOKEN or GH_CONFIG_DIR")

	// 5. Timeout names the binary, duration, and fix while keeping capped output.
	f.Enqueue("git", FakeOutcome{
		Timeout: true,
		Stdout:  "fetching origin...\n",
		Stderr:  "stalled on pack\n",
	})
	res, err := f.Run(ctx, Cmd{Argv: []string{"git", "fetch"}, Timeout: 60 * time.Second})
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrTimeout, exErr.Kind)
	assert.Equal(t, "git", exErr.Program)
	assert.Contains(t, exErr.Error(), "timed out after 1m0s")
	assert.Contains(t, exErr.Error(), "raise the policy timeout")
	assert.Equal(t, "fetching origin...\n", res.Stdout)
	assert.Equal(t, "stalled on pack\n", res.Stderr)
}

func TestCappedOutputPreservesHeadAndTailWithinMaxBytes(t *testing.T) {
	t.Parallel()
	f := NewFakeExec()
	head := "HEAD_START_" + strings.Repeat("A", 120)
	tail := strings.Repeat("Z", 120) + "_TAIL_END"
	f.Enqueue("git", FakeOutcome{
		ExitCode: 1,
		Stdout:   head + tail,
		Stderr:   strings.Repeat("E", 300),
	})

	res, err := f.Run(context.Background(), Cmd{
		Argv:     []string{"git", "log"},
		MaxBytes: 128,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.ExitCode)
	assert.True(t, res.Truncated)
	assert.LessOrEqual(t, len(res.Stdout), 128)
	assert.LessOrEqual(t, len(res.Stderr), 128)
	assert.True(t, strings.HasPrefix(res.Stdout, "HEAD_START_"))
	assert.True(t, strings.HasSuffix(res.Stdout, "_TAIL_END"))
	assert.Contains(t, res.Stdout, "...[truncated]...")
}

func TestFunctionalOSExecStdinClosedTimeoutAndMissingBinary(t *testing.T) {
	t.Parallel()
	ex := NewOSExec()
	ctx := context.Background()

	// 1. Stdin is closed: `cat` with no file argument reads stdin and exits 0 immediately with empty stdout.
	res, err := ex.Run(ctx, Cmd{
		Argv:    []string{"/bin/cat"},
		Timeout: 2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "", res.Stdout)

	// 2. Non-zero exit code returns Result without ExecError.
	res, err = ex.Run(ctx, Cmd{
		Argv:    []string{"/bin/sh", "-c", "printf 'out'; printf 'err' >&2; exit 7"},
		Timeout: 2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 7, res.ExitCode)
	assert.Equal(t, "out", res.Stdout)
	assert.Equal(t, "err", res.Stderr)

	// 3. Timeout kills the process and returns ErrTimeout with partial output kept.
	res, err = ex.Run(ctx, Cmd{
		Argv:    []string{"/bin/sh", "-c", "printf 'before-sleep\n'; sleep 5"},
		Timeout: 100 * time.Millisecond,
	})
	var exErr *ExecError
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrTimeout, exErr.Kind)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Equal(t, "before-sleep\n", res.Stdout)

	// 4. Missing binary returns ErrMissingBinary naming the program and fix.
	_, err = ex.Run(ctx, Cmd{
		Argv:    []string{"nova-nonexistent-binary-xyz"},
		Timeout: 2 * time.Second,
	})
	require.ErrorAs(t, err, &exErr)
	assert.Equal(t, ErrMissingBinary, exErr.Kind)
	assert.Contains(t, exErr.Error(), "nova-fleet up")
}

func TestRequestIDDeterministicAndInFlightDeduplication(t *testing.T) {
	t.Parallel()
	id1 := RequestID(1, "land", "card-1", "aaaa1111")
	id2 := RequestID(1, "land", "card-1", "aaaa1111")
	id3 := RequestID(1, "stop", "card-1", "aaaa1111")
	idEpoch2 := RequestID(2, "land", "card-1", "aaaa1111")
	assert.Equal(t, id1, id2)
	assert.NotEqual(t, id1, id3)
	assert.NotEqual(t, id1, idEpoch2, "same card and sha in epochs 1 and 2 must produce different RequestIDs")

	d := NewDeduplicator()
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0

	req := EdgeRequest{Epoch: 1, Kind: "land", Card: "card-1", SHA: "aaaa1111", After: []string{"op-accept-1"}}

	fn := func(ctx context.Context, r EdgeRequest) (EdgeResult, error) {
		calls++
		if calls == 1 {
			close(started)
			<-release
		}
		return EdgeResult{Outcome: "green"}, nil
	}

	doneErr := make(chan error, 1)
	var secondRes EdgeResult
	go func() {
		<-started
		var err error
		secondRes, err = d.Do(context.Background(), req, fn)
		doneErr <- err
	}()

	go func() {
		<-started
		for i := 0; i < 100000; i++ {
			runtime.Gosched()
		}
		close(release)
	}()

	firstRes, err := d.Do(context.Background(), req, fn)
	require.NoError(t, err)
	require.NoError(t, <-doneErr)
	assert.Equal(t, 1, calls, "in-flight duplicate must coalesce by RequestID")
	assert.Equal(t, id1, firstRes.ID)
	assert.Equal(t, firstRes, secondRes)

	// After completion, a subsequent call re-queries the world rather than returning stale memory.
	thirdRes, err := d.Do(context.Background(), req, fn)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "green", thirdRes.Outcome)
}

func TestRestartRequeryIdempotencyPerFactKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		kind    string
		card    string
		sha     string
		outcome string
	}{
		{kind: "land", card: "prod-1", sha: strings.Repeat("a", 40), outcome: "green"},
		{kind: "stop", card: "stream-a", sha: strings.Repeat("b", 40), outcome: "landed"},
		{kind: "runner", card: "cons-1", sha: strings.Repeat("c", 40), outcome: "recorded"},
	}

	for _, tc := range cases {
		req := EdgeRequest{
			Epoch: 1,
			Kind:  tc.kind,
			Card:  tc.card,
			SHA:   tc.sha,
			After: []string{"op-1"},
		}
		worldQuery := func(ctx context.Context, r EdgeRequest) (EdgeResult, error) {
			return EdgeResult{
				ID:      r.Key(),
				Kind:    r.Kind,
				Card:    r.Card,
				SHA:     r.SHA,
				Outcome: tc.outcome,
			}, nil
		}

		edgeBefore := NewDeduplicator()
		res1, err := edgeBefore.Do(ctx, req, worldQuery)
		require.NoError(t, err)

		// Restart the edge (new struct with zero memory) and query again: same answer.
		edgeAfter := NewDeduplicator()
		res2, err := edgeAfter.Do(ctx, req, worldQuery)
		require.NoError(t, err)

		assert.Equal(t, RequestID(1, tc.kind, tc.card, tc.sha), res1.ID)
		assert.NotEqual(t, RequestID(2, tc.kind, tc.card, tc.sha), res1.ID)
		assert.Equal(t, res1, res2, "restarted edge must return identical answer for fact kind %s", tc.kind)
	}
}

// Defect 73: a command its caller cancels is "canceled", never a timeout;
// a deadline that passes is the timeout.
func TestExecCanceledIsNotATimeout(t *testing.T) {
	t.Parallel()
	_, err := classifyExecErr_internal("nova-sandbox", time.Minute, context.Canceled, nil, Result{})
	var ee *ExecError
	require.True(t, errors.As(err, &ee))
	assert.Equal(t, ErrCanceled, ee.Kind)
	assert.NotContains(t, ee.Error(), "timed out")
	_, err = classifyExecErr_internal("nova-sandbox", time.Minute, context.DeadlineExceeded, nil, Result{})
	require.True(t, errors.As(err, &ee))
	assert.Equal(t, ErrTimeout, ee.Kind)
}
