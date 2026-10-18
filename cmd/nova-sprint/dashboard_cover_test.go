package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverCounter is an underlying writer that tallies its writes: how many were inside it
// at once (lockedWriter's one writer at a time) and how many bytes went through.
type coverCounter struct {
	inFlight atomic.Int64
	most     atomic.Int64
	bytes    atomic.Int64
}

func (c *coverCounter) Write(p []byte) (int, error) {
	n := c.inFlight.Add(1)
	for {
		most := c.most.Load()
		if n <= most || c.most.CompareAndSwap(most, n) {
			break
		}
	}
	c.bytes.Add(int64(len(p)))
	c.inFlight.Add(-1)
	return len(p), nil
}

// coverBroken is an underlying writer that fails: it writes nothing and its error is
// the caller's.
type coverBroken struct{ err error }

func (b coverBroken) Write([]byte) (int, error) { return 0, b.err }

// lockedWriter.Write (dashboard.go:227, the finding's other 0.0%) writes each line whole,
// one writer at a time, and the underlying writer's error is the caller's, never
// swallowed.
func TestDashboardCoverLockedWriterWritesEachLineWholeAndReturnsTheWritersError(t *testing.T) {
	t.Parallel()
	t.Run("each write goes through whole, one writer at a time", func(t *testing.T) {
		var c coverCounter
		l := &lockedWriter{w: &c}
		var expected atomic.Int64
		var wg sync.WaitGroup
		for g := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range 50 {
					line := fmt.Sprintf("DASHBOARD NOTE g%d line %d\n", g, i)
					expected.Add(int64(len(line)))
					_, err := l.Write([]byte(line))
					assert.NoError(t, err, "write")
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, expected.Load(), c.bytes.Load(), "every byte through, whole")
		assert.Equal(t, int64(0), c.inFlight.Load(), "all writes finished")
		assert.Equal(t, int64(1), c.most.Load(), "one writer at a time, never two inside at once")
	})
	t.Run("the underlying writer's error is the caller's", func(t *testing.T) {
		boom := errors.New("the writer is broken")
		n, err := (&lockedWriter{w: coverBroken{err: boom}}).Write([]byte("DASHBOARD STOPPED\n"))
		require.ErrorIs(t, err, boom)
		assert.Zero(t, n)
	})
}
