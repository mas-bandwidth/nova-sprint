package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFunctionalBalances: PG and the Fake keep provider balances alike:
// the latest read per provider, exact decimals, and the same refusals.
func TestFunctionalBalances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := openPG(t, startPG(t))
	require.NoError(t, p.Migrate(ctx))
	t0 := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for name, st := range map[string]Balances{"pg": p, "fake": NewFake()} {
		_, ok, err := st.LastBalance(ctx, "deepseek")
		require.NoError(t, err, name)
		assert.False(t, ok, name)
		require.NoError(t, st.AddBalance(ctx, Balance{Provider: "deepseek", At: t0, Seq: 3, USD: "50.25", Raw: `{"a":1}`}), name)
		require.NoError(t, st.AddBalance(ctx, Balance{Provider: "deepseek", At: t0.Add(time.Hour), Seq: 9, USD: "40.000001", Raw: "{}"}), name)
		require.NoError(t, st.AddBalance(ctx, Balance{Provider: "other", At: t0.Add(2 * time.Hour), Seq: 9, USD: "1", Raw: ""}), name)
		b, ok, err := st.LastBalance(ctx, "deepseek")
		require.NoError(t, err, name)
		require.True(t, ok, name)
		assert.Equal(t, int64(9), b.Seq, name)
		assert.Equal(t, "40.000001", b.USD, name)
		assert.True(t, b.At.Equal(t0.Add(time.Hour)), name)
		for _, bad := range []Balance{{USD: "1"}, {Provider: "x", USD: "-1"}, {Provider: "x", USD: "abc"}, {Provider: "x", Seq: -1, USD: "1"}} {
			assert.True(t, errors.Is(st.AddBalance(ctx, bad), ErrInvalid), "%s %+v", name, bad)
		}
	}
}
