package store

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/jackc/pgx/v5"
)

// RestoreLostRows puts back every row of the current epoch that the log
// holds and sprint.rows lacks, as the log's last after-image has it, and
// returns their names in order (none: it wrote nothing). Only a statement
// run outside the store loses a row this way: no write deletes one (a row
// nova-config no longer names is retired, round 32, and stays in the
// table). A lost row makes the log rebuild to a state the tables do not
// hold, so the first backup's restore verification and check --integrity
// refuse the store (guarantee 5). The log is the record, so the repair is
// the table brought back to it: no event, since the log already says the
// row is there, and nothing else changes. A row the table holds with
// columns the log does not give is not touched: that is not a lost row, and
// the integrity check still names it. It holds the writer lock, so no op
// lands between the fold and the insert, and a second run finds none.
func (p *PG) RestoreLostRows(ctx context.Context) ([]string, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("store: restore lost rows: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: a no-op after the commit
	names, err := restoreLostRows(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("store: restore lost rows: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: restore lost rows: %w", err)
	}
	return names, nil
}

func restoreLostRows(ctx context.Context, tx pgx.Tx) ([]string, error) {
	if _, err := tx.Exec(ctx, "LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE"); err != nil { // the writer lock (inTx)
		return nil, err
	}
	var events []Event
	for after := int64(0); ; {
		page, err := readEvents(ctx, tx, after, MaxEvents)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		events, after = append(events, page...), page[len(page)-1].Seq
	}
	snap, err := snapshotTx(ctx, tx, Scope{All: true})
	if err != nil {
		return nil, err
	}
	got := Rebuild(events)
	if got.Epoch != snap.Epoch {
		return nil, nil // not a lost row: the integrity check names the epoch
	}
	t := &pgTx{ctx: ctx, tx: tx, epoch: snap.Epoch}
	var names []string
	for _, name := range slices.Sorted(maps.Keys(got.Rows)) {
		if _, ok := snap.Rows[name]; ok {
			continue
		}
		var present bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM sprint.rows WHERE epoch = $1 AND name = $2)", snap.Epoch, name).Scan(&present); err != nil {
			return nil, err
		}
		if present { // retired in the table, live in the log: not a lost row, left for the check
			continue
		}
		if err := t.putRow(got.Rows[name]); err != nil {
			return nil, fmt.Errorf("putting back row %s: %w", name, err)
		}
		names = append(names, name)
	}
	return names, nil
}
