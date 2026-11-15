package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// RestoreLostRows puts back every row of the current epoch that the log
// holds and sprint.rows lacks, and returns their names in order (none: it
// wrote nothing). Only a statement run outside the store loses a row this
// way: no write deletes one (a row nova-config no longer names is retired,
// round 32, and stays in the table). A lost row makes the log rebuild to a
// state the tables do not hold, so the first backup's restore verification
// and check --integrity refuse the store (guarantee 5).
//
// The rows come back through one op of the protocol (actor store-repair,
// kind repair), so the log and the table agree again and a run already
// running sees the row: its view was read cold and then only folds events,
// so a row put back with no event would stay out of its ticks (defect,
// 2026-11-15: the put back space3 was never retired). The op puts each row
// with its last after-image's session and role (PutRow), then its state,
// hold and flaps; the rest is telemetry (the heartbeat, cleared) and the gen,
// which goes on from the new row. A row the table holds with columns the log
// does not give is not touched: that is not a lost row, and the integrity
// check still names it. It holds the writer lock, so no op lands between
// the fold and the put, and a second run finds none.
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
	var names []string
	var writes []Write
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
		names, writes = append(names, name), append(writes, putBack(got.Rows[name])...)
	}
	if len(names) == 0 {
		return nil, nil
	}
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&now); err != nil {
		return nil, err
	}
	op := Op{ID: fmt.Sprintf("store-repair-%d-%s", snap.Seq, strings.Join(names, ",")), Epoch: snap.Epoch, Actor: "store-repair", Kind: "repair", At: at(now),
		Note: "rows lost from sprint.rows outside the store, put back from the log: " + strings.Join(names, ", "), Writes: writes}
	r, err := (&PG{}).inTx(ctx, tx, op, check, func(t txn, epoch int) (string, *State, *State, error) { return run(t, epoch, op) })
	switch {
	case err != nil:
		return nil, fmt.Errorf("putting back rows %s: %w", strings.Join(names, ", "), err)
	case !r.Accepted:
		return nil, fmt.Errorf("putting back rows %s was refused: %s", strings.Join(names, ", "), r.Reason)
	}
	return names, nil
}

// putBack is the writes that put row r back as a new row: its session and
// role, then its state, hold and flaps where they are not a new row's.
func putBack(r Row) []Write {
	w := []Write{PutRow{Name: r.Name, Session: r.Session, Role: r.Role}}
	if r.State != "down" {
		w = append(w, SetRowState{Row: r.Name, State: r.State})
	}
	if r.Held {
		w = append(w, SetRowHeld{Row: r.Name, Held: true, Reason: r.Reason})
	}
	if r.Flaps != 0 {
		w = append(w, SetRowFlaps{Row: r.Name, Flaps: r.Flaps})
	}
	return w
}
