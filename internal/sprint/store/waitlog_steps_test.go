package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestWaitLogStepsWithoutAWallClock drives the wait by explicit steps so no
// wall clock is read. The test uses LogWait callback to block the wait
// until a commit wakens it.
func TestWaitLogStepsWithoutAWallClock(t *testing.T) {
	t.Parallel()
	m := NewMem()

	// Use a controllable LogWait to block the wait until woken
	var logWaitMu sync.Mutex
	logWaitUnblocked := false
	logWaitBlock := make(chan struct{})
	m.LogWait = func(d time.Duration) {
		logWaitMu.Lock()
		logWaitUnblocked = true
		logWaitMu.Unlock()
		// Block until the test signals us to continue
		<-logWaitBlock
	}

	st := &Store{B: m, Names: sprint.Names{}, Now: func() time.Time { return time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC) }}
	ctx := context.Background()
	wait := time.Hour

	// First wait: empty log, should block in LogWait callback
	done := make(chan struct {
		tail string
		woke bool
		err  error
	}, 1)
	go func() {
		tail, woke, err := st.WaitLog(ctx, 0, "", wait)
		done <- struct {
			tail string
			woke bool
			err  error
		}{tail, woke, err}
	}()

	// Wait for the LogWait callback to be called (wait has begun)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("wait never began")
		default:
			logWaitMu.Lock()
			unblocked := logWaitUnblocked
			logWaitMu.Unlock()
			if unblocked {
				goto began
			}
		}
	}
began:

	// The wait should still be blocked (LogWait callback is blocked on logWaitBlock)
	select {
	case <-done:
		t.Fatal("wait returned before commit")
	default:
	}

	// Commit a line - this should wake the wait (close m.logged channel)
	// The commit happens while the wait is blocked in LogWait
	_, err := st.Run(context.Background(), Step{Verb: "note", Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NMachineStarted, Who: "tester", At: s.Now, What: "a line"}}}
	}})
	require.NoError(t, err)

	// Unblock the LogWait callback - the select <-wake should now complete
	close(logWaitBlock)

	// The wait should now return (select <-wake completes inside LogWait)
	select {
	case o := <-done:
		require.NoError(t, o.err, "wait returned woke %v, err %v", o.woke, o.err)
		require.True(t, o.woke, "wait returned woke %v, err %v", o.woke, o.err)
	case <-ctx.Done():
		t.Fatal("test timed out waiting for wait to return")
	}
}
