package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the version of schema.sql, kept in sprint.meta. Version 2
// adds the card columns author and base_sha. There is no upgrade path: a store
// at version 1 is refused like any other version.
const SchemaVersion = 2

// Schema is the schema file the binary carries; Migrate applies it.
func Schema() string { return schemaSQL }

// PG is the Store in PostgreSQL, schema sprint.
type PG struct {
	pool *pgxpool.Pool
	// afterCommit is a fault-injection seam for the functional tests (kept,
	// not dead code): called after a write commits and before its receipt is
	// returned; an error it returns is reported as ErrUnknown, as a reply lost
	// after the commit would be (guarantee 3). Nil outside tests.
	afterCommit func() error
}

// Open connects to the store at dsn (NOVA_PG_DSN, no password in it) with
// password (NOVA_PG_PASSWORD, may be empty). It never quotes the dsn back.
func Open(ctx context.Context, dsn, password string) (*PG, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("store: the postgres dsn could not be parsed") // its text may hold a password
	}
	if password != "" {
		cfg.ConnConfig.Password = password
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: cannot reach postgres at %s:%d: %w", cfg.ConnConfig.Host, cfg.ConnConfig.Port, err)
	}
	return &PG{pool: pool}, nil
}

// Close closes the connection pool.
func (p *PG) Close() { p.pool.Close() }

// Migrate applies schema.sql to an empty store or one at SchemaVersion, and
// refuses a store at any other version, naming both. Twice is a no-op.
func (p *PG) Migrate(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: a no-op after the commit
	var meta, sprint bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('sprint.meta') IS NOT NULL, to_regclass('sprint.epoch') IS NOT NULL").Scan(&meta, &sprint); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if meta || sprint {
		v := 0 // a sprint schema without sprint.meta predates versions
		if meta {
			err := tx.QueryRow(ctx, "SELECT schema FROM sprint.meta WHERE id = 1").Scan(&v)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("store: migrate: %w", err)
			}
		}
		if v != SchemaVersion {
			return fmt.Errorf("store: migrate: the store is at schema version %d, this binary knows schema version %d", v, SchemaVersion)
		}
	}
	if _, err := tx.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// defect is a constraint violation during the writes: the rules let through
// something the schema refuses. It is an engine bug, not a refusal and not
// unknown: the transaction is rolled back.
type defect struct{ error }

func (d defect) Unwrap() error { return d.error }

// write runs fn in one READ COMMITTED transaction. fn takes the writer lock
// first, so writers run one at a time and nothing needs a retry. Every failure
// after the transaction began is ErrUnknown, except a defect.
func (p *PG) write(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx)) // ignored: the error that got us here is the one returned
		var d defect
		if errors.As(err, &d) {
			return d
		}
		return fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	if p.afterCommit != nil {
		if err := p.afterCommit(); err != nil {
			return fmt.Errorf("%w: %w", ErrUnknown, err)
		}
	}
	return nil
}

// lockEpoch takes the writer lock, held to commit, and reads the epoch.
func lockEpoch(ctx context.Context, tx pgx.Tx) (int, error) {
	if _, err := tx.Exec(ctx, "LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(ctx, "SELECT n FROM sprint.epoch WHERE id = 1").Scan(&n)
	return n, err
}

// Apply runs op in one transaction: the stored receipt when the op id is
// known; else the epoch, the conditions, the writes and the event row.
func (p *PG) Apply(ctx context.Context, op Op) (Receipt, error) {
	if err := check(op); err != nil {
		return Receipt{}, err
	}
	op = normalize(op)
	return p.do(ctx, op, func(t *pgTx) (string, *State, *State, error) { return run(t, t.epoch, op) })
}

// Clear opens a new epoch, carrying every row into it, in one transaction.
func (p *PG) Clear(ctx context.Context, op Op) (Receipt, error) {
	op, err := checkClear(op)
	if err != nil {
		return Receipt{}, err
	}
	op = normalize(op)
	return p.do(ctx, op, func(t *pgTx) (string, *State, *State, error) {
		r, after, err := runClear(t, t.epoch, op)
		return r, nil, after, err
	})
}

// do is the protocol Apply and Clear share.
func (p *PG) do(ctx context.Context, op Op, rules func(*pgTx) (string, *State, *State, error)) (Receipt, error) {
	var r Receipt
	err := p.write(ctx, func(tx pgx.Tx) error {
		var err error
		r, err = doTx(ctx, tx, op, rules)
		return err
	})
	if err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func doTx(ctx context.Context, tx pgx.Tx, op Op, rules func(*pgTx) (string, *State, *State, error)) (Receipt, error) {
	epoch, err := lockEpoch(ctx, tx)
	if err != nil {
		return Receipt{}, err
	}
	if r, ok, err := replayTx(ctx, tx, op.ID); err != nil || ok {
		return r, err
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT writes"); err != nil {
		return Receipt{}, err
	}
	reason, before, after, err := rules(&pgTx{ctx: ctx, tx: tx, epoch: epoch})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && strings.HasPrefix(pe.Code, "23") {
		return Receipt{}, defect{fmt.Errorf("store: engine defect: op %s broke constraint %s: %w", op.ID, pe.ConstraintName, err)}
	}
	if err != nil {
		return Receipt{}, err
	}
	if reason != "" {
		before, after = nil, nil
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT writes"); err != nil {
			return Receipt{}, err
		}
	}
	return logTx(ctx, tx, op, reason, before, after, epoch)
}

// logTx appends the event of op, logged in epoch, and returns its receipt.
// Under the writer lock the next seq is the last plus one.
func logTx(ctx context.Context, tx pgx.Tx, op Op, reason string, before, after *State, epoch int) (Receipt, error) {
	var seq int64
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(seq), 0) + 1 FROM sprint.events").Scan(&seq); err != nil {
		return Receipt{}, err
	}
	r := receipt(op, reason, after, seq)
	_, err := tx.Exec(ctx, `INSERT INTO sprint.events (seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		seq, op.ID, op.Actor, op.Kind, reason == "", reason, jsonArg(before), jsonArg(after), string(encode(r)), op.At, epoch)
	return r, err
}

func jsonArg(s *State) any {
	if s == nil {
		return nil
	}
	return string(encode(s))
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func replayTx(ctx context.Context, q queryRower, opID string) (Receipt, bool, error) {
	var b []byte
	err := q.QueryRow(ctx, "SELECT receipt FROM sprint.events WHERE op = $1", opID).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, err
	}
	var r Receipt
	return r, true, json.Unmarshal(b, &r)
}

// Replay returns the stored receipt of an op id, and false when it is unknown.
func (p *PG) Replay(ctx context.Context, opID string) (Receipt, bool, error) {
	r, ok, err := replayTx(ctx, p.pool, opID)
	if err != nil {
		return Receipt{}, false, fmt.Errorf("store: replay: %w", err)
	}
	return r, ok, nil
}

// Policy returns one policy version; ErrNotFound when there is none.
func (p *PG) Policy(ctx context.Context, version int) (PolicyVersion, error) {
	v, ok, err := one(p.pool.QueryRow(ctx, "SELECT "+policyCols+" FROM sprint.policy WHERE version = $1", version), scanPolicy)
	switch {
	case err != nil:
		return v, fmt.Errorf("store: policy: %w", err)
	case !ok:
		return v, fmt.Errorf("%w: policy version %d", ErrNotFound, version)
	}
	return v, nil
}
