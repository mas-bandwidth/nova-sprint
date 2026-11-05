package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The ask's budget is half the tick context's remaining time when that is less
// than AskBudget, read at the store's clock. The deadline here is a fixed value
// on the fake clock: no real time.
func TestTickAskDeadlineOnAFakeClock(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	dl := now.Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), dl)
	defer cancel()
	require.Equal(t, 1500*time.Millisecond, askBudget(ctx, dl.Add(-3*time.Second)))
	require.Equal(t, AskBudget, askBudget(context.Background(), t0))
}
