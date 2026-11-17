package store

// A row removed from sprint.rows by a statement outside the store (the live
// store, 2026-11-15: space3, a held, unhooked row, deleted by hand) leaves a
// log that rebuilds to a row the table lacks: check --integrity and the
// first backup's restore verification refuse the store "in rows".
// RestoreLostRows puts it back as the log has it, and only it.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionalRestoreLostRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, p, dsn := pgHarness(t)
	h.clear() // a later epoch, as the live store's
	h.seed()
	h.ok(h.op("hook", nil, PutRow{Name: "lost", Session: "sess-l", Role: "worker"}, SetRowState{"lost", "up"}, SetRowHeld{Row: "lost", Held: true, Reason: "measurement machine; held"}))
	h.ok(h.op("beat", nil, SetRowLoad{Row: "lost", CPUPeakDeci: 168, Tiers: "flash,pro", Version: "v2.0.42"}))
	h.ok(h.op("unhook", nil, PutRow{Name: "lost"}))
	h.ok(h.op("gone", nil, PutRow{Name: "gone"}))
	h.ok(h.op("retire", nil, RetireRow{Row: "gone", Reason: "not in nova-config"}))
	want := h.snap()
	require.Contains(t, want.Rows, "lost")
	require.NotContains(t, want.Rows, "gone")

	validate := func() error {
		tx, err := connect(t, dsn).BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }() // ignored: read only
		return ValidateHistory(ctx, tx)
	}
	names, err := p.RestoreLostRows(ctx)
	require.NoError(t, err)
	assert.Empty(t, names, "a whole store has no lost row, and a retired row is not one")
	require.NoError(t, validate())

	_, err = connect(t, dsn).Exec(ctx, "DELETE FROM sprint.rows WHERE name = 'lost'")
	require.NoError(t, err)
	err = validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs from the snapshot in rows")

	names, err = p.RestoreLostRows(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"lost"}, names)
	require.NoError(t, validate(), "the table is the log's again")
	after := h.snap()
	back := after.Rows["lost"]
	old := want.Rows["lost"]
	assert.Greater(t, back.Gen, old.Gen, "the gen goes on from the row's last gen: the RowGen fence never sees an old gen again")
	assert.Equal(t, old.Gen+1, back.Gen)
	next := old
	next.Gen = back.Gen
	assert.Equal(t, next, back, "the row as the log last had it, hold and heartbeat included, at the next gen")
	assert.True(t, old.Held && old.CPUPeakDeci == 168, "the fixture row is held and has beaten")
	assert.Equal(t, want.Seq+1, after.Seq, "put back through one op, so a running view folds it")
	last := h.events()[len(h.events())-1]
	assert.Equal(t, "store-repair", last.Actor)
	assert.Contains(t, last.After.Rows, "lost")
	want.Rows["lost"], want.Seq = back, after.Seq
	assert.Equal(t, want, after, "nothing else changed")

	names, err = p.RestoreLostRows(ctx)
	require.NoError(t, err)
	assert.Empty(t, names, "a second run finds none")

	// A row the table holds with columns the log does not give is not lost:
	// it is left, and the check still names rows.
	_, err = connect(t, dsn).Exec(ctx, "UPDATE sprint.rows SET flaps = 9 WHERE name = 'lost'")
	require.NoError(t, err)
	names, err = p.RestoreLostRows(ctx)
	require.NoError(t, err)
	assert.Empty(t, names)
	err = validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs from the snapshot in rows")

	// A row retired in the table but live in the log is not lost either: it is
	// left, not brought back, and the check names rows.
	_, err = connect(t, dsn).Exec(ctx, "UPDATE sprint.rows SET flaps = 0, retired = true WHERE name = 'lost'")
	require.NoError(t, err)
	names, err = p.RestoreLostRows(ctx)
	require.NoError(t, err)
	assert.Empty(t, names)
	var retired bool
	require.NoError(t, connect(t, dsn).QueryRow(ctx, "SELECT retired FROM sprint.rows WHERE name = 'lost'").Scan(&retired))
	assert.True(t, retired, "the retired row is left as it is")
	err = validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs from the snapshot in rows")

	// An epoch the log does not reach: no row is lost in it, nothing is
	// written, and the check names the epoch.
	_, err = connect(t, dsn).Exec(ctx, "UPDATE sprint.rows SET retired = false WHERE name = 'lost'; DELETE FROM sprint.rows WHERE name = 'lost'; UPDATE sprint.epoch SET n = n + 1")
	require.NoError(t, err)
	names, err = p.RestoreLostRows(ctx)
	require.NoError(t, err)
	assert.Empty(t, names)
	var rows int
	require.NoError(t, connect(t, dsn).QueryRow(ctx, "SELECT count(*) FROM sprint.rows WHERE name = 'lost'").Scan(&rows))
	assert.Zero(t, rows, "nothing is put back across an epoch mismatch")
	err = validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "epoch")
}
