package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	streamCols = "name, repo, base, held, pos"
	cardCols   = "id, stream, state, gen, score, needs, coalesce(holder, ''), head, pr, brief, reason"
	rowCols    = "name, kind, width, state, seen, flaps"
	policyCols = "version, kind, name, body, active, actor, at"
)

func scanStream(r pgx.Row) (Stream, error) {
	var s Stream
	err := r.Scan(&s.Name, &s.Repo, &s.Base, &s.Held, &s.Pos)
	return s, err
}

func scanCard(r pgx.Row) (Card, error) {
	var c Card
	err := r.Scan(&c.ID, &c.Stream, &c.State, &c.Gen, &c.Score, &c.Needs, &c.Holder, &c.Head, &c.PR, &c.Brief, &c.Reason)
	c.Needs = needs(c.Needs)
	return c, err
}

func scanRow(r pgx.Row) (Row, error) {
	var w Row
	var seen *time.Time
	err := r.Scan(&w.Name, &w.Kind, &w.Width, &w.State, &seen, &w.Flaps)
	if seen != nil {
		w.Seen = at(*seen)
	}
	return w, err
}

func scanPolicy(r pgx.Row) (Policy, error) {
	var p Policy
	err := r.Scan(&p.Version, &p.Kind, &p.Name, &p.Body, &p.Active, &p.Actor, &p.At)
	p.At = at(p.At)
	return p, err
}

// one reads at most one row: false when there is none.
func one[T any](r pgx.Row, scan func(pgx.Row) (T, error)) (T, bool, error) {
	v, err := scan(r)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, err
}

// all reads every row of a query into a map by key.
func all[K comparable, T any](ctx context.Context, tx pgx.Tx, scan func(pgx.Row) (T, error), key func(T) K, sql string, args ...any) (map[K]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[K]T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out[key(v)] = v
	}
	return out, rows.Err()
}

// Snapshot reads the current epoch in one REPEATABLE READ transaction.
func (p *PG) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, fmt.Errorf("store: snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: read only, nothing to keep
	s, err := snapshotTx(ctx, tx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("store: snapshot: %w", err)
	}
	return s, nil
}

func snapshotTx(ctx context.Context, tx pgx.Tx) (Snapshot, error) {
	var s Snapshot
	err := tx.QueryRow(ctx, "SELECT n, (SELECT coalesce(max(version), 0) FROM sprint.policy WHERE active) FROM sprint.epoch").Scan(&s.Epoch, &s.Policy)
	if err != nil {
		return s, err
	}
	if s.Streams, err = all(ctx, tx, scanStream, func(v Stream) string { return v.Name }, "SELECT "+streamCols+" FROM sprint.streams WHERE epoch = $1", s.Epoch); err != nil {
		return s, err
	}
	if s.Cards, err = all(ctx, tx, scanCard, func(v Card) string { return v.ID }, "SELECT "+cardCols+" FROM sprint.cards WHERE epoch = $1", s.Epoch); err != nil {
		return s, err
	}
	s.Rows, err = all(ctx, tx, scanRow, func(v Row) string { return v.Name }, "SELECT "+rowCols+" FROM sprint.rows WHERE epoch = $1", s.Epoch)
	return s, err
}

// Events returns up to max events after afterSeq, in seq order.
func (p *PG) Events(ctx context.Context, afterSeq int64, max int) ([]Event, error) {
	var limit any
	if max > 0 {
		limit = max
	}
	rows, err := p.pool.Query(ctx, `SELECT seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch
		FROM sprint.events WHERE seq > $1 ORDER BY seq LIMIT $2`, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("store: events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var before, after, rec []byte
		if err := rows.Scan(&e.Seq, &e.Op, &e.Actor, &e.Kind, &e.Accepted, &e.Reason, &before, &after, &rec, &e.At, &e.Epoch); err != nil {
			return nil, fmt.Errorf("store: events: %w", err)
		}
		e.At, e.Before, e.After = at(e.At), decodeState(before), decodeState(after)
		if err := json.Unmarshal(rec, &e.Receipt); err != nil {
			return nil, fmt.Errorf("store: events: receipt of %s: %w", e.Op, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: events: %w", err)
	}
	return out, nil
}

// pgTx is the txn of one PostgreSQL transaction: every read locks its row.
type pgTx struct {
	ctx   context.Context
	tx    pgx.Tx
	epoch int
}

func (t *pgTx) card(id string) (Card, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+cardCols+" FROM sprint.cards WHERE epoch = $1 AND id = $2 FOR UPDATE", t.epoch, id), scanCard)
}

func (t *pgTx) stream(name string) (Stream, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+streamCols+" FROM sprint.streams WHERE epoch = $1 AND name = $2 FOR UPDATE", t.epoch, name), scanStream)
}

func (t *pgTx) row(name string) (Row, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+rowCols+" FROM sprint.rows WHERE epoch = $1 AND name = $2 FOR UPDATE", t.epoch, name), scanRow)
}

func (t *pgTx) policy(v int) (Policy, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+policyCols+" FROM sprint.policy WHERE version = $1 FOR UPDATE", v), scanPolicy)
}

func (t *pgTx) policies() (int, []Policy, error) {
	var last int
	if err := t.tx.QueryRow(t.ctx, "SELECT coalesce(max(version), 0) FROM sprint.policy").Scan(&last); err != nil {
		return 0, nil, err
	}
	m, err := all(t.ctx, t.tx, scanPolicy, func(p Policy) int { return p.Version }, "SELECT "+policyCols+" FROM sprint.policy WHERE active FOR UPDATE")
	var active []Policy
	for _, p := range m {
		active = append(active, p)
	}
	return last, active, err
}

func (t *pgTx) putStream(s Stream) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.streams (epoch, name, repo, base, held, pos) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (epoch, name) DO UPDATE SET repo = EXCLUDED.repo, base = EXCLUDED.base, held = EXCLUDED.held, pos = EXCLUDED.pos`,
		t.epoch, s.Name, s.Repo, s.Base, s.Held, s.Pos)
	return err
}

func (t *pgTx) putCard(c Card) error {
	n := c.Needs
	if n == nil {
		n = []string{}
	}
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.cards (epoch, id, stream, state, gen, score, needs, holder, head, pr, brief, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11, $12)
		ON CONFLICT (epoch, id) DO UPDATE SET stream = EXCLUDED.stream, state = EXCLUDED.state, gen = EXCLUDED.gen, score = EXCLUDED.score,
		needs = EXCLUDED.needs, holder = EXCLUDED.holder, head = EXCLUDED.head, pr = EXCLUDED.pr, brief = EXCLUDED.brief, reason = EXCLUDED.reason`,
		t.epoch, c.ID, c.Stream, c.State, c.Gen, c.Score, n, c.Holder, c.Head, c.PR, c.Brief, c.Reason)
	return err
}

func (t *pgTx) putRow(r Row) error {
	var seen any
	if !r.Seen.IsZero() {
		seen = r.Seen
	}
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.rows (epoch, name, kind, width, state, seen, flaps) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (epoch, name) DO UPDATE SET kind = EXCLUDED.kind, width = EXCLUDED.width, state = EXCLUDED.state, seen = EXCLUDED.seen, flaps = EXCLUDED.flaps`,
		t.epoch, r.Name, r.Kind, r.Width, r.State, seen, r.Flaps)
	return err
}

func (t *pgTx) putPolicy(p Policy) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.policy (version, kind, name, body, active, actor, at) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (version) DO UPDATE SET active = EXCLUDED.active`,
		p.Version, p.Kind, p.Name, p.Body, p.Active, p.Actor, p.At)
	return err
}
