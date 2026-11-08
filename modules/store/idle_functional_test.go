package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The store's connections carry IdleTxTimeout; a session that stalls in a
// transaction is ended by the server within the bound (the dsn's own, here,
// so the test is quick) and the operation queued behind its lock proceeds.
func TestFunctionalIdleTransactionIsBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := startPG(t)
	p := openPG(t, dsn)
	var got string
	require.NoError(t, p.pool.QueryRow(ctx, "SHOW idle_in_transaction_session_timeout").Scan(&got))
	assert.Equal(t, "1min", got)

	cfg, err := pgx.ParseConfig(dsn + "&idle_in_transaction_session_timeout=300ms")
	require.NoError(t, err)
	BoundIdleTx(cfg)
	stalled, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	defer stalled.Close(ctx)
	tx, err := stalled.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT count(*) FROM sprint.streams")
	require.NoError(t, err) // holds ACCESS SHARE on streams, then stalls

	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = p.pool.Exec(wait, "LOCK TABLE sprint.streams IN ACCESS EXCLUSIVE MODE; SELECT 1")
	require.NoError(t, err, "the lock waits for the stalled session to be ended, not for TCP")
	_, err = tx.Exec(ctx, "SELECT 1")
	require.Error(t, err, "the stalled session is gone")
}
