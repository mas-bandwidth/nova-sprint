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
	h.ok(h.op("beat", nil, SetRowLoad{Row: "lost", Load1Centi: 564, Cores: 25, SlotsTotal: 6, CPUPeakDeci: 168, Tiers: "flash,pro", Version: "v2.0.42"}))
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
	assert.Equal(t, want, h.snap(), "the row is back as it was, gen, hold and heartbeat included, and the log has no new event")

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
}
