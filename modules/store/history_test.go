package store

// Round 21: ValidateHistory, the whole-history check RestoreEnd runs. One
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

// TestRebuildKeepsAConsumersModel: a card's model is set when it is cut
// and never changed (putCard's upsert leaves it). A worker binary from
// before the column reported cards with no model in its after-image while
// the table kept the cut's: the live store's 14 done cards (seq 4921-5018,
// e.g. schema-moo-cards.w4: cut by tick 4934 with inception/mercury-3,
// reported by amd-2 at 4942 with none). The rebuild keeps the cut's model.
func TestRebuildKeepsAConsumersModel(t *testing.T) {
	t.Parallel()
	cut := Card{ID: "p1.w4", Kind: "work", Of: "p1", Stream: "s", State: "working", Holder: "amd-2", Model: "inception/mercury-3"}
	done := cut
	done.State, done.Model, done.Gen = "done", "", 1
	got := Rebuild([]Event{
		{Seq: 1, Accepted: true, Kind: "tick", After: &State{Cards: map[string]Card{cut.ID: cut}}},
		{Seq: 2, Accepted: true, Kind: "report", After: &State{Cards: map[string]Card{cut.ID: done}}},
	})
	if c := got.Cards[cut.ID]; c.Model != cut.Model || c.State != "done" {
		t.Fatalf("rebuilt %+v, want the cut's model %q and state done", c, cut.Model)
	}
	again := Rebuild([]Event{ // a card cut anew in a later epoch takes its own model
		{Seq: 1, Accepted: true, Kind: "tick", After: &State{Cards: map[string]Card{cut.ID: cut}}},
		{Seq: 2, Accepted: true, Kind: "clear", After: &State{Epoch: 2}},
		{Seq: 3, Accepted: true, Kind: "tick", After: &State{Cards: map[string]Card{cut.ID: done}}},
	})
	if c := again.Cards[cut.ID]; c.Model != "" {
		t.Fatalf("a new epoch's card kept the old one's model: %+v", c)
	}
}
