package store

// ValidateHistory, the whole-history check RestoreEnd runs. One
// mutation of a loaded dump per check, each refused naming the first
// mismatch; a live store passes.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionalValidateHistoryLive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, _, dsn := pgHarness(t)
	restoreSeed(h)
	tx, err := connect(t, dsn).BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: read only
	require.NoError(t, ValidateHistory(ctx, tx))
}

// The seed's log: pol-1 (policy 1 added and active), clear-2 (epoch 2),
// seed-3, reason-4, open-5 (judgement j1, cursor feed at 1).
func TestFunctionalValidateHistoryRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, _, dsn := pgHarness(t)
	restoreSeed(h)
	require.Equal(t, int64(5), h.snap().Seq)
	d := dump(t, dsn)
	const unlocked = "ALTER TABLE sprint.events DISABLE TRIGGER events_append_only; "
	for name, c := range map[string]struct{ db, mutate, msg string }{
		"dropped event":  {"dropped", unlocked + "DELETE FROM sprint.events WHERE seq = 3", "the log has no event at seq 3 (the next is seq 4)"},
		"orphan receipt": {"orphan", unlocked + `UPDATE sprint.events SET receipt = jsonb_set(receipt, '{op}', '"ghost"') WHERE seq = 4`, "the receipt stored with event seq 4 (op reason-4) is not its own: it names op ghost at seq 4"},
		"epoch row":      {"epoch", "UPDATE sprint.epoch SET n = 3", "the epoch row is 3, but the log's last epoch is 2"},
		"policy not in the log": {"policy", "INSERT INTO sprint.policy (version, kind, name, body, active, actor, at) VALUES (2, 'data', 'x', '{}', false, 'mallory', now())",
			"policy version 2 is stored, but no event in the log wrote it"},
		"cursor past the end": {"cursor", "UPDATE sprint.cursors SET seq = 99 WHERE name = 'feed'", "cursor feed is at seq 99, past the log's last seq 5"},
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := connect(t, newDatabase(t, dsn, c.db)).Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }() // ignored: refused
			require.NoError(t, RestoreBegin(ctx, tx, digestOf(d)))
			require.NoError(t, loadDump(ctx, tx, d))
			_, err = tx.Exec(ctx, c.mutate)
			require.NoError(t, err, c.mutate)
			_, err = RestoreEnd(ctx, tx)
			require.Error(t, err)
			assert.Equal(t, "store: restore: the dump does not hold together: "+c.msg, err.Error())
		})
	}
}
