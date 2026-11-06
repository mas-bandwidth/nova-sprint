package store

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"

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

// migrateLock is the advisory lock key Migrate holds for its transaction, so
// concurrent Migrates of one store run one at a time.
const migrateLock = 0x6e6f7661 // "nova"

// Schema is the schema file the binary carries; Migrate applies it.
func Schema() string { return schemaSQL }

// PG is the Store in PostgreSQL, schema sprint.
type PG struct {
	pool *pgxpool.Pool
	// fault is a fault-injection seam for the functional tests (kept, not
	// dead code). It is called with "locked" inside a write's transaction
	// once the writer lock is held, and with "committed" after COMMIT and
	// before the receipt is returned. An error it returns is ErrUnknown (a
	// reply lost after the commit, guarantee 3); a panic in it proves the
	// lock is released. Nil outside tests.
	fault func(stage string) error
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
// refuses a store at any other version, naming both. Twice is a no-op, and
// concurrent Migrates are safe: each holds one advisory lock.
func (p *PG) Migrate(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: a no-op after the commit
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLock); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
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

// known is a failure whose outcome is known: nothing was committed. An
// invalid body, or a PostgreSQL data (class 22) or constraint (class 23)
// error during the writes: an engine defect, the rules let through what the
// schema refuses. Never ErrUnknown.
func known(op Op, err error) error {
	var pe *pgconn.PgError
	switch {
	case errors.Is(err, ErrInvalid):
		return err
	case errors.As(err, &pe) && (pe.Code[:2] == "22" || pe.Code[:2] == "23"):
		on := cmp.Or(pe.ConstraintName, pe.ColumnName, pe.TableName)
		return fmt.Errorf("store: engine defect: op %s: SQLSTATE %s on constraint %s: %w", op.ID, pe.Code, on, err)
	}
	return nil
}

// write runs protocol in one READ COMMITTED transaction holding the writer lock,
// so writers run one at a time and nothing needs a retry. A failure after
// the transaction began is ErrUnknown unless it is known (above). A panic
// rolls the transaction back, releasing the lock, and is re-raised.
func (p *PG) write(ctx context.Context, op Op, valid func(Op) error, rule rules) (r Receipt, err error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Receipt{}, fmt.Errorf("store: begin: %w", err)
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback(context.WithoutCancel(ctx)) // ignored: the error or panic that got us here is the one reported
		}
	}()
	if r, err = p.inTx(ctx, tx, op, valid, rule); err != nil {
		if k := known(op, err); k != nil {
			return Receipt{}, k
		}
		return Receipt{}, fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	done = true
	if err := tx.Commit(ctx); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	if err := p.injected("committed"); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	return r, nil
}

func (p *PG) inTx(ctx context.Context, tx pgx.Tx, op Op, valid func(Op) error, rule rules) (Receipt, error) {
	if _, err := tx.Exec(ctx, "LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return Receipt{}, err
	}
	if err := p.injected("locked"); err != nil {
		return Receipt{}, err
	}
	var epoch int
	if err := tx.QueryRow(ctx, "SELECT n FROM sprint.epoch WHERE id = 1").Scan(&epoch); err != nil {
		return Receipt{}, err
	}
	return protocol(&pgTx{ctx: ctx, tx: tx, epoch: epoch}, epoch, op, valid, rule)
}

func (p *PG) injected(stage string) error {
	if p.fault == nil {
		return nil
	}
	return p.fault(stage)
}

// Apply runs op in one transaction (protocol, apply.go).
func (p *PG) Apply(ctx context.Context, op Op) (Receipt, error) {
	if err := checkID(op); err != nil {
		return Receipt{}, err
	}
	op = normalize(op)
	return p.write(ctx, op, check, func(tx txn, epoch int) (string, *State, *State, error) { return run(tx, epoch, op) })
}

// Clear opens a new epoch, carrying every row into it, in one transaction.
func (p *PG) Clear(ctx context.Context, op Op) (Receipt, error) {
	if err := checkID(op); err != nil {
		return Receipt{}, err
	}
	op = normalize(op)
	op.Kind = cmp.Or(op.Kind, "clear")
	return p.write(ctx, op, checkClear, func(tx txn, epoch int) (string, *State, *State, error) { return runClear(tx, epoch, op) })
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

// Replay returns the stored receipt of an op id, and false when it is
// unknown (an id no op could have, such as one holding a NUL, is unknown).
func (p *PG) Replay(ctx context.Context, opID string) (Receipt, bool, error) {
	if badText(opID) {
		return Receipt{}, false, nil
	}
	r, ok, err := replayTx(ctx, p.pool, opID)
	if err != nil {
		return Receipt{}, false, fmt.Errorf("store: replay: %w", err)
	}
	return r, ok, nil
}

// Policy returns one policy version; ErrNotFound when there is none.
func (p *PG) Policy(ctx context.Context, version int) (PolicyVersion, error) {
	if version < 0 || version > math.MaxInt32 {
		return PolicyVersion{}, fmt.Errorf("%w: policy version %d", ErrNotFound, version)
	}
	v, ok, err := one(p.pool.QueryRow(ctx, "SELECT "+policyCols+" FROM sprint.policy WHERE version = $1", version), scanPolicy)
	switch {
	case err != nil:
		return v, fmt.Errorf("store: policy: %w", err)
	case !ok:
		return v, fmt.Errorf("%w: policy version %d", ErrNotFound, version)
	}
	return v, nil
}
