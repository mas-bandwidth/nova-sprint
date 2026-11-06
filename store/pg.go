package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// Schema is the schema file the binary carries; Migrate applies it.
func Schema() string { return schemaSQL }

// PG is the Store in PostgreSQL, schema sprint.
type PG struct{ pool *pgxpool.Pool }

// maxTries bounds the retries of a serialization failure inside Apply and Clear.
const maxTries = 8

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

// Migrate applies schema.sql; applying it twice is a no-op.
func (p *PG) Migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// notSent marks an error from before any write could have reached the server.
type notSent struct{ error }

func (e notSent) Unwrap() error { return e.error }

func retryable(err error) bool {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return false
	}
	return pe.Code == "40001" || pe.Code == "40P01" || (pe.Code == "23505" && strings.HasPrefix(pe.ConstraintName, "events_"))
}

func backoff(ctx context.Context, try int) error {
	t := time.NewTimer(time.Duration(try*try) * 5 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// serial runs fn in one SERIALIZABLE transaction, retrying a serialization
// failure; the op id makes a retry safe. Every failure after the transaction
// began is ErrUnknown: the caller replays.
func (p *PG) serial(ctx context.Context, fn func(pgx.Tx) error) error {
	for try := 1; ; try++ {
		err := p.once(ctx, fn)
		var ns notSent
		switch {
		case err == nil:
			return nil
		case errors.As(err, &ns):
			return fmt.Errorf("store: begin: %w", ns.error)
		case !retryable(err) || try == maxTries:
			return fmt.Errorf("%w: %w", ErrUnknown, err)
		}
		if err := backoff(ctx, try); err != nil {
			return fmt.Errorf("%w: %w", ErrUnknown, err)
		}
	}
}

func (p *PG) once(ctx context.Context, fn func(pgx.Tx) error) (err error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return notSent{err}
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx)) // ignored: the error that got us here is the one returned
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// lockEpoch serializes every Apply and Clear and reads the epoch. LOCK TABLE
// takes no snapshot, so everything after it sees every op committed before it:
// the replay check, the conditions and the next seq are never stale.
func lockEpoch(ctx context.Context, tx pgx.Tx) (int, error) {
	if _, err := tx.Exec(ctx, "LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(ctx, "SELECT n FROM sprint.epoch").Scan(&n)
	return n, err
}

// Apply runs op in one SERIALIZABLE transaction: the stored receipt when the
// op id is known; else the conditions, the writes and the event row.
func (p *PG) Apply(ctx context.Context, op Op) (Receipt, error) {
	if op.ID == "" {
		return Receipt{}, errNoID
	}
	op = normalize(op)
	var r Receipt
	err := p.serial(ctx, func(tx pgx.Tx) error {
		var err error
		r, err = applyTx(ctx, tx, op)
		return err
	})
	return r, err
}

func applyTx(ctx context.Context, tx pgx.Tx, op Op) (Receipt, error) {
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
	reason, before, after, err := run(&pgTx{ctx: ctx, tx: tx, epoch: epoch}, epoch, op)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && strings.HasPrefix(pe.Code, "23") { // the schema refused what the rules let through
		reason, err = "the store refused the write: "+pe.ConstraintName, nil
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

// logTx appends the event of op and returns its receipt. The seq is the next
// one under the epoch lock, so seq order is commit order with no gaps; the
// bigserial's sequence is moved up to it so its default never collides.
func logTx(ctx context.Context, tx pgx.Tx, op Op, reason string, before, after *State, epoch int) (Receipt, error) {
	var seq int64
	err := tx.QueryRow(ctx, "SELECT setval(pg_get_serial_sequence('sprint.events', 'seq'), coalesce(max(seq), 0) + 1) FROM sprint.events").Scan(&seq)
	if err != nil {
		return Receipt{}, err
	}
	r := receipt(op, reason, after, seq, epoch)
	_, err = tx.Exec(ctx, `INSERT INTO sprint.events (seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch)
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

func replayTx(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, opID string) (Receipt, bool, error) {
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

// Clear increments the epoch and logs itself, in one transaction.
func (p *PG) Clear(ctx context.Context, op Op) (int, error) {
	if err := clearable(op); err != nil {
		return 0, err
	}
	op = clearOp(normalize(op))
	var epoch int
	err := p.serial(ctx, func(tx pgx.Tx) error {
		n, err := lockEpoch(ctx, tx)
		if err != nil {
			return err
		}
		if r, ok, err := replayTx(ctx, tx, op.ID); err != nil || ok {
			epoch = r.Epoch
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE sprint.epoch SET n = n + 1"); err != nil {
			return err
		}
		epoch = n + 1
		_, err = logTx(ctx, tx, op, "", nil, &State{Epoch: epoch}, epoch)
		return err
	})
	return epoch, err
}
