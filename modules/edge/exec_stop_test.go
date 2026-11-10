package edge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stopped command's group gets SIGTERM first: git removes its *.lock
// files on SIGTERM and leaves them on SIGKILL (a stale shallow.lock in a
// work clone refused every later fetch). SIGKILL follows only for a group
// still alive after the grace.
func TestStopGroupTermsFirstAndKillsOnlyWhatOutlivesTheGrace(t *testing.T) {
	t.Parallel()
	type sent struct {
		pid int
		sig syscall.Signal
	}
	run := func(aliveProbes int) ([]sent, time.Duration) {
		var got []sent
		var slept time.Duration
		kill := func(pid int, sig syscall.Signal) error {
			got = append(got, sent{pid, sig})
			if sig == 0 {
				if aliveProbes == 0 {
					return syscall.ESRCH
				}
				aliveProbes--
			}
			return nil
		}
		require.NoError(t, stopGroup_internal(42, kill, func(d time.Duration) { slept += d }, time.Second, 250*time.Millisecond))
		return got, slept
	}

	got, slept := run(1) // ends during the grace
	assert.Equal(t, []sent{{-42, syscall.SIGTERM}, {-42, 0}, {-42, 0}}, got)
	assert.Equal(t, 250*time.Millisecond, slept)

	got, slept = run(1 << 20) // ignores SIGTERM
	require.NotEmpty(t, got)
	assert.Equal(t, sent{-42, syscall.SIGTERM}, got[0])
	assert.Equal(t, sent{-42, syscall.SIGKILL}, got[len(got)-1])
	assert.Equal(t, time.Second, slept)
	for _, s := range got[1 : len(got)-1] {
		assert.Equal(t, sent{-42, 0}, s, "only probes between SIGTERM and SIGKILL")
	}
}

// The real thing: a cancelled command's group runs its SIGTERM cleanup
// (as git does for its lock files) before Run returns.
func TestFunctionalOSExecCancelLetsTheGroupRemoveItsLock(t *testing.T) {
	t.Parallel()
	lock := filepath.Join(t.TempDir(), "shallow.lock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan bool, 1)
	go func() {
		defer cancel()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(lock); err == nil {
				seen <- true
				return
			}
		}
		seen <- false
	}()
	_, err := NewOSExec().Run(ctx, Cmd{
		Argv:    []string{"/bin/sh", "-c", `trap 'rm -f "$1"; exit 143' TERM; : > "$1"; sleep 30 & wait`, "sh", lock},
		Timeout: 20 * time.Second,
	})
	require.True(t, <-seen, "the script made its lock before it was stopped")
	var ee *ExecError
	require.True(t, errors.As(err, &ee), "a cancelled run is an ExecError: %v", err)
	assert.Equal(t, ErrCanceled, ee.Kind)
	_, statErr := os.Stat(lock)
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "the group's SIGTERM cleanup ran: %v", statErr)
}
