package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	streamCols = "name, repo, base, held, pos"
	cardCols   = "id, stream, state, gen, attempt, score, needs, coalesce(holder, ''), branch, head, pr, brief, reason, author, base_sha"
	rowCols    = "name, kind, width, state, session, flaps"
	policyCols = "version, kind, name, body, active, actor, at"
)

func scanStream(r pgx.Row) (Stream, error) {
	var s Stream
	err := r.Scan(&s.Name, &s.Repo, &s.Base, &s.Held, &s.Pos)
	return s, err
}

func scanCard(r pgx.Row) (Card, error) {
	var c Card
	err := r.Scan(&c.ID, &c.Stream, &c.State, &c.Gen, &c.Attempt, &c.Score, &c.Needs, &c.Holder, &c.Branch, &c.Head, &c.PR, &c.Brief, &c.Reason, &c.Author, &c.BaseSHA)
	c.Needs = needs(c.Needs)
	return c, err
}

func scanRow(r pgx.Row) (Row, error) {
	var w Row
	err := r.Scan(&w.Name, &w.Kind, &w.Width, &w.State, &w.Session, &w.Flaps)
	return w, err
}

func scanPolicy(r pgx.Row) (PolicyVersion, error) {
	var p PolicyVersion
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
	err := tx.QueryRow(ctx, `SELECT n, (SELECT coalesce(max(version), 0) FROM sprint.policy WHERE active),
		(SELECT coalesce(max(seq), 0) FROM sprint.events) FROM sprint.epoch WHERE id = 1`).Scan(&s.Epoch, &s.Policy, &s.Seq)
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

// Events returns up to max (capped at MaxEvents) events after afterSeq, in
// seq order.
func (p *PG) Events(ctx context.Context, afterSeq int64, max int) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `SELECT seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch
		FROM sprint.events WHERE seq > $1 ORDER BY seq LIMIT $2`, afterSeq, capEvents(max))
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

// pgTx is the txn of one PostgreSQL transaction holding the writer lock: at
// READ COMMITTED every read sees every op committed before this one.
type pgTx struct {
	ctx   context.Context
	tx    pgx.Tx
	epoch int
}

func (t *pgTx) card(id string) (Card, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+cardCols+" FROM sprint.cards WHERE epoch = $1 AND id = $2", t.epoch, id), scanCard)
}

func (t *pgTx) stream(name string) (Stream, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+streamCols+" FROM sprint.streams WHERE epoch = $1 AND name = $2", t.epoch, name), scanStream)
}

func (t *pgTx) row(name string) (Row, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+rowCols+" FROM sprint.rows WHERE epoch = $1 AND name = $2", t.epoch, name), scanRow)
}

func (t *pgTx) policy(v int) (PolicyVersion, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+policyCols+" FROM sprint.policy WHERE version = $1", v), scanPolicy)
}

func (t *pgTx) policies() (int, []PolicyVersion, error) {
	var last int
	if err := t.tx.QueryRow(t.ctx, "SELECT coalesce(max(version), 0) FROM sprint.policy").Scan(&last); err != nil {
		return 0, nil, err
	}
	m, err := all(t.ctx, t.tx, scanPolicy, func(p PolicyVersion) int { return p.Version }, "SELECT "+policyCols+" FROM sprint.policy WHERE active")
	var active []PolicyVersion
	for _, p := range m {
		active = append(active, p)
	}
	return last, active, err
}

func (t *pgTx) held(row string) (int, error) {
	var n int
	err := t.tx.QueryRow(t.ctx, "SELECT count(*) FROM sprint.cards WHERE epoch = $1 AND holder = $2", t.epoch, row).Scan(&n)
	return n, err
}

func (t *pgTx) lastSeq() (int64, error) {
	var n int64
	err := t.tx.QueryRow(t.ctx, "SELECT coalesce(max(seq), 0) FROM sprint.events").Scan(&n)
	return n, err
}

func (t *pgTx) rows() ([]Row, error) {
	m, err := all(t.ctx, t.tx, scanRow, func(r Row) string { return r.Name }, "SELECT "+rowCols+" FROM sprint.rows WHERE epoch = $1", t.epoch)
	var out []Row
	for _, r := range m {
		out = append(out, r)
	}
	return out, err
}

func (t *pgTx) open() error {
	if _, err := t.tx.Exec(t.ctx, "UPDATE sprint.epoch SET n = n + 1 WHERE id = 1"); err != nil {
		return err
	}
	t.epoch++
	return nil
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
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.cards (epoch, id, stream, state, gen, attempt, score, needs, holder, branch, head, pr, brief, reason, author, base_sha)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (epoch, id) DO UPDATE SET stream = EXCLUDED.stream, state = EXCLUDED.state, gen = EXCLUDED.gen, attempt = EXCLUDED.attempt,
		score = EXCLUDED.score, needs = EXCLUDED.needs, holder = EXCLUDED.holder, branch = EXCLUDED.branch, head = EXCLUDED.head,
		pr = EXCLUDED.pr, brief = EXCLUDED.brief, reason = EXCLUDED.reason, author = EXCLUDED.author, base_sha = EXCLUDED.base_sha`,
		t.epoch, c.ID, c.Stream, c.State, c.Gen, c.Attempt, c.Score, n, c.Holder, c.Branch, c.Head, c.PR, c.Brief, c.Reason, c.Author, c.BaseSHA)
	return err
}

func (t *pgTx) putRow(r Row) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.rows (epoch, name, kind, width, state, session, flaps) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (epoch, name) DO UPDATE SET kind = EXCLUDED.kind, width = EXCLUDED.width, state = EXCLUDED.state,
		session = EXCLUDED.session, flaps = EXCLUDED.flaps`,
		t.epoch, r.Name, r.Kind, r.Width, r.State, r.Session, r.Flaps)
	return err
}

func (t *pgTx) putPolicy(p PolicyVersion) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.policy (version, kind, name, body, active, actor, at) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (version) DO UPDATE SET active = EXCLUDED.active`,
		p.Version, p.Kind, p.Name, p.Body, p.Active, p.Actor, p.At)
	return err
}
