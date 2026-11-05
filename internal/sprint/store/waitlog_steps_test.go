package store

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestWaitLogStepsWithoutAWallClock drives a wait on the log by explicit steps
// on the synctest bubble's clock, so no wall clock is read. With LogWait nil
// the wait blocks on a commit's wake, and the commit must wake it at once.
func TestWaitLogStepsWithoutAWallClock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m := NewMem()
		st := &Store{B: m, Names: sprint.Names{}, Now: func() time.Time { return time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC) }}
		type outcome struct {
			tail string
			woke bool
			err  error
		}
		done := make(chan outcome, 1)
		go func() {
			tail, woke, err := st.WaitLog(context.Background(), 0, "", time.Hour)
			done <- outcome{tail, woke, err}
		}()

		// The wait is blocked on the log: it has no runnable goroutine.
		synctest.Wait()
		select {
		case o := <-done:
			t.Fatalf("the wait returned before a line: tail %q woke %v err %v", o.tail, o.woke, o.err)
		default:
		}

		// A commit appends a line and wakes the wait.
		_, err := st.Run(context.Background(), Step{Verb: "note", Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NMachineStarted, Who: "tester", At: s.Now, What: "a line"}}}
		}})
		require.NoError(t, err)

		synctest.Wait()
		select {
		case o := <-done:
			require.NoError(t, o.err, "the wait returned woke %v, err %v: a commit during it wakes it at once", o.woke, o.err)
			require.True(t, o.woke, "the wait returned woke %v, err %v: a commit during it wakes it at once", o.woke, o.err)
			require.NotEmpty(t, o.tail, "the wait returned woke %v, err %v: a commit during it wakes it at once", o.woke, o.err)
		default:
			t.Fatal("a commit did not wake the wait")
		}
	})
}
