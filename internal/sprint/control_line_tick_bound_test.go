package sprint

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tick that cannot take the line within its bound gives up holding nothing, and the
// line is the batches' again at once (a-card-read-never-runs-the-deal, docs/SPEC-SPRINT.md
// section 14, The server, "The tick's deadline").
func TestTheTickLockGivesUpWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	var l ControlLine
	l.Lock() // a batch holds the line

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the tick's bound has already ended: no clock decides the race
	_, err := l.TickLockCtx(ctx)
	require.ErrorIs(t, err, context.Canceled)

	l.mu.Lock()
	wants := l.turns.tickWants
	l.mu.Unlock()
	assert.False(t, wants, "a tick that gave up leaves no want behind")

	l.Unlock()
	assert.True(t, l.TryLock(), "the line is free for the next batch")
	l.Unlock()

	d, err := l.TickLockCtx(context.Background())
	require.NoError(t, err, "a free line is the tick's at once")
	assert.GreaterOrEqual(t, d.Nanoseconds(), int64(0))
	l.Unlock()

	_ = l.TickLock() // TickLock is TickLockCtx with no bound
	l.Unlock()
}
