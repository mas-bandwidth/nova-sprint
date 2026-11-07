package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	streamCols = "name, repo, base, held, pos, mode, forge"
	cardCols   = `id, kind, coalesce("of", ''), stream, state, gen, attempt, score, needs, after, coalesce(holder, ''), branch, head, pr, brief, reason, author, base_sha, result, prior_head, bounces, cost, priced, priority, tier, paths, done_when, prose, review, finding, red, model`
	rowCols    = "name, state, session, role, flaps, held, gen, reason, load1_centi, cores, slots_used, slots_total"
	judgeCols  = "id, kind, about, question, command, group_key, state, deadline, answer, actor, opened_at, closed_at"
	policyCols = "version, kind, name, body, active, actor, at"
)

func scanStream(r pgx.Row) (Stream, error) {
	var s Stream
	err := r.Scan(&s.Name, &s.Repo, &s.Base, &s.Held, &s.Pos, &s.Mode, &s.Forge)
	return s, err
}

func scanCard(r pgx.Row) (Card, error) {
	var c Card
	err := r.Scan(&c.ID, &c.Kind, &c.Of, &c.Stream, &c.State, &c.Gen, &c.Attempt, &c.Score, &c.Needs, &c.After, &c.Holder, &c.Branch, &c.Head, &c.PR, &c.Brief, &c.Reason, &c.Author, &c.BaseSHA, &c.Result, &c.Prior, &c.Bounces, &c.Cost, &c.Priced, &c.Priority, &c.Tier, &c.Paths, &c.DoneWhen, &c.Prose, &c.Review, &c.Finding, &c.Red, &c.Model)
	c = cardCopy(c)
	return c, err
}

func scanRow(r pgx.Row) (Row, error) {
	var w Row
	err := r.Scan(&w.Name, &w.State, &w.Session, &w.Role, &w.Flaps, &w.Held, &w.Gen, &w.Reason, &w.Load1Centi, &w.Cores, &w.SlotsUsed, &w.SlotsTotal)
	return w, err
}

func scanJudgement(r pgx.Row) (Judgement, error) {
	var j Judgement
	var deadline, closed *time.Time
	err := r.Scan(&j.ID, &j.Kind, &j.About, &j.Question, &j.Command, &j.Group, &j.State, &deadline, &j.Answer, &j.Actor, &j.OpenedAt, &closed)
	j.OpenedAt = at(j.OpenedAt)
	if deadline != nil {
		j.Deadline = at(*deadline)
	}
	if closed != nil {
		j.ClosedAt = at(*closed)
	}
	return j, err
}

// nullTime is a time as a nullable column: NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
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
		(SELECT coalesce(max(seq), 0) FROM sprint.events), stopped, stop_reason FROM sprint.epoch WHERE id = 1`).Scan(&s.Epoch, &s.Policy, &s.Seq, &s.Stopped, &s.StopReason)
	if err != nil {
		return s, err
	}
	if s.Streams, err = all(ctx, tx, scanStream, func(v Stream) string { return v.Name }, "SELECT "+streamCols+" FROM sprint.streams WHERE epoch = $1", s.Epoch); err != nil {
		return s, err
	}
	if s.Cards, err = all(ctx, tx, scanCard, func(v Card) string { return v.ID }, "SELECT "+cardCols+" FROM sprint.cards WHERE epoch = $1", s.Epoch); err != nil {
		return s, err
	}
	if s.Rows, err = all(ctx, tx, scanRow, func(v Row) string { return v.Name }, "SELECT "+rowCols+" FROM sprint.rows WHERE epoch = $1", s.Epoch); err != nil {
		return s, err
	}
	if s.Judgements, err = all(ctx, tx, scanJudgement, func(v Judgement) string { return v.ID }, "SELECT "+judgeCols+" FROM sprint.judgements WHERE epoch = $1", s.Epoch); err != nil {
		return s, err
	}
	cursors, err := all(ctx, tx, scanCursor, func(v cursorRow) string { return v.name }, "SELECT name, seq FROM sprint.cursors")
	s.Cursors = map[string]int64{}
	for name, c := range cursors {
		s.Cursors[name] = c.seq
	}
	return s, err
}

type cursorRow struct {
	name string
	seq  int64
}

func scanCursor(r pgx.Row) (cursorRow, error) {
	var c cursorRow
	err := r.Scan(&c.name, &c.seq)
	return c, err
}

// Events returns up to max (capped at MaxEvents) events after afterSeq, in
// seq order.
func (p *PG) Events(ctx context.Context, afterSeq int64, max int) ([]Event, error) {
	return readEvents(ctx, p.pool, afterSeq, max)
}

// EventIdentity returns the op id and epoch of the event at seq, reading
// only those two columns (never the images or the receipt).
func (p *PG) EventIdentity(ctx context.Context, seq int64) (string, int, bool, error) {
	var op string
	var epoch int
	err := p.pool.QueryRow(ctx, "SELECT op, epoch FROM sprint.events WHERE seq = $1", seq).Scan(&op, &epoch)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", 0, false, nil
	case err != nil:
		return "", 0, false, fmt.Errorf("store: event identity: %w", err)
	}
	return op, epoch, true, nil
}

// Restored returns the digest of the dump the store was last restored from
// and when (RestoreEnd), and false when it never was. It only reads.
func (p *PG) Restored(ctx context.Context) (string, time.Time, bool, error) {
	var digest string
	var at *time.Time
	if err := p.pool.QueryRow(ctx, "SELECT restored_digest, restored_at FROM sprint.meta WHERE id = 1").Scan(&digest, &at); err != nil {
		return "", time.Time{}, false, fmt.Errorf("store: restored: %w", err)
	}
	if at == nil {
		return "", time.Time{}, false, nil
	}
	return digest, at.UTC(), true, nil
}

// readEvents is Events on q: the pool, or a transaction (RestoreEnd).
func readEvents(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, afterSeq int64, max int) ([]Event, error) {
	rows, err := q.Query(ctx, `SELECT seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch, policy
		FROM sprint.events WHERE seq > $1 ORDER BY seq LIMIT $2`, afterSeq, capEvents(max))
	if err != nil {
		return nil, fmt.Errorf("store: events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var before, after, rec []byte
		if err := rows.Scan(&e.Seq, &e.Op, &e.Actor, &e.Kind, &e.Accepted, &e.Reason, &before, &after, &rec, &e.At, &e.Epoch, &e.Policy); err != nil {
			return nil, fmt.Errorf("store: events: %w", err)
		}
		e.At = at(e.At)
		var err error
		if e.Before, err = readState(before); err != nil {
			return nil, fmt.Errorf("store: events: before-image of seq %d: %w", e.Seq, err)
		}
		if e.After, err = readState(after); err != nil {
			return nil, fmt.Errorf("store: events: after-image of seq %d: %w", e.Seq, err)
		}
		if e.Receipt, err = readReceipt(rec, e.After); err != nil {
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
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.streams (epoch, name, repo, base, held, pos, mode, forge) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (epoch, name) DO UPDATE SET repo = EXCLUDED.repo, base = EXCLUDED.base, held = EXCLUDED.held, pos = EXCLUDED.pos,
		mode = EXCLUDED.mode, forge = EXCLUDED.forge`,
		t.epoch, s.Name, s.Repo, s.Base, s.Held, s.Pos, s.Mode, s.Forge)
	return err
}

func (t *pgTx) putCard(c Card) error {
	// kind, of, stream, holder and tier are set on insert only: no write
	// changes them, and the cards_fixed trigger refuses an update that would.
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.cards (epoch, id, kind, "of", stream, state, gen, attempt, score, needs, after, holder, branch, head, pr, brief, reason, author, base_sha, result, prior_head, bounces, cost, priced, priority, tier, paths, done_when, prose, review, finding, red, model)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''), $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33)
		ON CONFLICT (epoch, id) DO UPDATE SET state = EXCLUDED.state, gen = EXCLUDED.gen, attempt = EXCLUDED.attempt,
		score = EXCLUDED.score, needs = EXCLUDED.needs, after = EXCLUDED.after, branch = EXCLUDED.branch, head = EXCLUDED.head,
		pr = EXCLUDED.pr, brief = EXCLUDED.brief, reason = EXCLUDED.reason, author = EXCLUDED.author, base_sha = EXCLUDED.base_sha, result = EXCLUDED.result,
		prior_head = EXCLUDED.prior_head, bounces = EXCLUDED.bounces, cost = EXCLUDED.cost, priced = EXCLUDED.priced, priority = EXCLUDED.priority,
		paths = EXCLUDED.paths, done_when = EXCLUDED.done_when, prose = EXCLUDED.prose, review = EXCLUDED.review, finding = EXCLUDED.finding, red = EXCLUDED.red`,
		t.epoch, c.ID, c.Kind, c.Of, c.Stream, c.State, c.Gen, c.Attempt, c.Score, list0(c.Needs), list0(c.After), c.Holder, c.Branch, c.Head, c.PR, c.Brief, c.Reason, c.Author, c.BaseSHA, c.Result, c.Prior, c.Bounces, c.Cost, c.Priced, c.Priority,
		c.Tier, list0(c.Paths), list0(c.DoneWhen), c.Prose, c.Review, c.Finding, c.Red, c.Model)
	return err
}

func (t *pgTx) putRow(r Row) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.rows (epoch, name, state, session, role, flaps, held, gen, reason, load1_centi, cores, slots_used, slots_total) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (epoch, name) DO UPDATE SET state = EXCLUDED.state, session = EXCLUDED.session,
		role = EXCLUDED.role, flaps = EXCLUDED.flaps, held = EXCLUDED.held, gen = EXCLUDED.gen, reason = EXCLUDED.reason,
		load1_centi = EXCLUDED.load1_centi, cores = EXCLUDED.cores, slots_used = EXCLUDED.slots_used, slots_total = EXCLUDED.slots_total`,
		t.epoch, r.Name, r.State, r.Session, r.Role, r.Flaps, r.Held, r.Gen, r.Reason, r.Load1Centi, r.Cores, r.SlotsUsed, r.SlotsTotal)
	return err
}

func (t *pgTx) judgement(id string) (Judgement, bool, error) {
	return one(t.tx.QueryRow(t.ctx, "SELECT "+judgeCols+" FROM sprint.judgements WHERE epoch = $1 AND id = $2", t.epoch, id), scanJudgement)
}

func (t *pgTx) putJudgement(j Judgement) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.judgements (epoch, id, kind, about, question, command, group_key, state, deadline, answer, actor, opened_at, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (epoch, id) DO UPDATE SET state = EXCLUDED.state, answer = EXCLUDED.answer, actor = EXCLUDED.actor, closed_at = EXCLUDED.closed_at`,
		t.epoch, j.ID, j.Kind, j.About, j.Question, j.Command, j.Group, j.State, nullTime(j.Deadline), j.Answer, j.Actor, j.OpenedAt, nullTime(j.ClosedAt))
	return err
}

func (t *pgTx) stop() (Stop, error) {
	var s Stop
	err := t.tx.QueryRow(t.ctx, "SELECT stopped, stop_reason FROM sprint.epoch WHERE id = 1").Scan(&s.Stopped, &s.Reason)
	return s, err
}

func (t *pgTx) putStop(s Stop) error {
	_, err := t.tx.Exec(t.ctx, "UPDATE sprint.epoch SET stopped = $1, stop_reason = $2 WHERE id = 1", s.Stopped, s.Reason)
	return err
}

func (t *pgTx) cursor(name string) (int64, bool, error) {
	c, ok, err := one(t.tx.QueryRow(t.ctx, "SELECT name, seq FROM sprint.cursors WHERE name = $1", name), scanCursor)
	return c.seq, ok, err
}

func (t *pgTx) putCursor(name string, seq int64) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.cursors (name, seq) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET seq = EXCLUDED.seq`, name, seq)
	return err
}

// list0 is a card list as PostgreSQL stores it: '{}', never NULL.
func list0(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func (t *pgTx) replay(opID string) (Receipt, bool, error) {
	return scanReplay(t.tx.QueryRow(t.ctx, replaySQL, opID))
}

func (t *pgTx) discard() error {
	_, err := t.tx.Exec(t.ctx, "ROLLBACK TO SAVEPOINT writes")
	return err
}

// appendEvent inserts the op's event. The after-image is stored once, in
// after; the receipt column leaves it out, and readReceipt puts it back.
func (t *pgTx) appendEvent(e Event) error {
	rec := e.Receipt
	rec.After = nil
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.events (seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch, policy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		e.Seq, e.Op, e.Actor, e.Kind, e.Accepted, e.Reason, jsonArg(e.Before), jsonArg(e.After), string(encode(rec)), e.At, e.Epoch, e.Policy)
	return err
}

func (t *pgTx) putPolicy(p PolicyVersion) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO sprint.policy (version, kind, name, body, active, actor, at) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (version) DO UPDATE SET active = EXCLUDED.active`,
		p.Version, p.Kind, p.Name, p.Body, p.Active, p.Actor, p.At)
	return err
}

// readReceipt is a stored receipt with its after-image, a copy of the
// event's after column (the receipt column leaves it out since round 15), so
// an event's After and its Receipt.After are two values, as on the Fake (the
// seventh cold read). A receipt stored before round 15 carries its own
// after, the same image; the column's copy replaces it.
func readReceipt(b []byte, after *State) (Receipt, error) {
	var r Receipt
	if err := json.Unmarshal(b, &r); err != nil {
		return Receipt{}, err
	}
	if after != nil {
		r.After = after.clone()
	}
	return r, nil
}

// clone is a deep copy of an image: every map and list its own.
func (s *State) clone() *State {
	c := *s
	c.Streams, c.Rows, c.Policies, c.Judgements, c.Cursors = maps.Clone(s.Streams), maps.Clone(s.Rows), maps.Clone(s.Policies), maps.Clone(s.Judgements), maps.Clone(s.Cursors)
	c.Cards = maps.Clone(s.Cards)
	if s.Stop != nil {
		stop := *s.Stop
		c.Stop = &stop
	}
	for k, v := range c.Cards {
		v.Needs, v.After, v.Paths, v.DoneWhen = slices.Clone(v.Needs), slices.Clone(v.After), slices.Clone(v.Paths), slices.Clone(v.DoneWhen)
		c.Cards[k] = v
	}
	return &c
}
