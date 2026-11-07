package store

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the version of schema.sql, kept in sprint.meta with the
// schema's hash. Version 2 adds to version 1: the card columns kind (with
// sentinel), of, after, author, base_sha, prior_head, result (with fault),
// bounces, cost, priced, priority, tier, paths, done_when, prose, review, finding and red; the card
// states blocked and done; the row columns gen, held and reason (round 22
// dropped kind, width, tiers, model and harness: configuration, the
// roster's); the tables judgements and cursors; the events' policy; the meta
// hash; the triggers that keep a card's kind, of, stream, holder, tier, id
// and epoch, a row's, stream's or judgement's key and epoch, a policy version's
// columns but active, and the log (TRUNCATE included)
// fixed; and the CHECKs that keep every count at 0 or more. There is no
// upgrade path from version 1: a store at version 1 is refused like any
// other version. Within version 2 there are two: a store made by round 16's
// schema (round16Hash) or by round 17's, the one every round to 21 carried
// (round21Hash), is upgraded by Migrate (round 17's red column, round 22's
// dropped row columns).
const SchemaVersion = 2

// round16Hash is the schema hash of round 16's schema.sql
// (testdata/schema-round16.sql, 36c1bbd6), an earlier build of version 2
// Migrate upgrades: running schema.sql on it adds cards.red (false on every
// card, with its CHECK), drops round 22's five row columns and the
// cards_live index, and changes nothing else, then the new hash is
// recorded. Any hash but these two and schemaHash is still refused.
const round16Hash = "58b09b225469ae56094b2eeafc8631c5e5cc9ae3dc5e1d2894a0f0434187373e"

// round21Hash is the schema hash of round 17's schema.sql, which every
// round to 21 carried (testdata/schema-round21.sql): running schema.sql on
// it drops sprint.rows' kind, width, tiers, model and harness (ALTER TABLE
// DROP COLUMN, round 22) and the cards_live index, in Migrate's one
// transaction, then the new hash is recorded.
const round21Hash = "cbe47774a69cf22749b6d414e1bc5689104820199313088df41975ef1771d491"

// schemaHash is the sha256, in hex, of the schema the binary carries with
// comments, indentation and blank lines stripped, so editing a comment does
// not refuse every store.
var schemaHash = hashSchema(schemaSQL)

// createSchema is schema.sql's one statement that needs CREATE on the
// database: PostgreSQL checks that privilege before IF NOT EXISTS, so a role
// that owns an existing schema sprint but may not create schemas (the
// least-privilege setup) is refused it. Migrate runs it only when the schema
// is absent and otherwise runs schemaObjects, the rest of schema.sql. The
// hash is still of the whole file.
const createSchema = "CREATE SCHEMA IF NOT EXISTS sprint;"

var schemaObjects = strings.Replace(schemaSQL, createSchema, "", 1)

// hashSchema strips a -- comment only outside a single-quoted string literal
// and a double-quoted identifier (a doubled quote closes and reopens, so it
// stays inside), across lines, so a '--' in a literal is hashed. Dollar
// quotes ($$ ... $$) are scanned as code, since the schema's are all function
// bodies: their comments are stripped and their literals kept, so a -- in a
// $$ string literal would be stripped (the schema has none). E-prefixed
// escape strings are not read (the schema has none). Two more limits, both
// absent from the schema and asserted by the tests: a quote inside a /* */
// comment opens a literal, and indentation inside a literal that spans lines
// is stripped.
func hashSchema(sql string) string {
	var b strings.Builder
	var quote byte // the open quote: ' a literal, " an identifier, 0 none
	for line := range strings.Lines(sql) {
		cut := len(line)
		for i := 0; i < cut; i++ {
			switch ch := line[i]; {
			case quote == 0 && (ch == '\'' || ch == '"'):
				quote = ch
			case ch == quote:
				quote = 0
			case quote == 0 && strings.HasPrefix(line[i:], "--"):
				cut = i
			}
		}
		if line = strings.TrimSpace(line[:cut]); line != "" {
			b.WriteString(line + "\n")
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// migrateLock is the advisory lock key Migrate holds for its transaction, so
// concurrent Migrates of one store run one at a time.
const migrateLock = 0x6e6f7661 // "nova"

// PG is the Store in PostgreSQL, schema sprint.
type PG struct {
	pool *pgxpool.Pool
	// fault is a fault-injection seam for the functional tests (kept, not
	// dead code). It is called with "locked" inside a write's transaction
	// once the writer lock is held, and with "committed" after COMMIT and
	// before the receipt is returned. An error at committed is ErrUnknown (a
	// reply lost after the commit, guarantee 3), one at locked is known like
	// any other before COMMIT; a panic in it proves the lock is released. Nil
	// outside tests.
	fault func(stage string) error
}

// dsnUserinfo and dsnPassword find a password a DSN's parse error may quote.
var (
	dsnUserinfo = regexp.MustCompile(`://[^/@\s]*@`)
	dsnPassword = regexp.MustCompile(`(?i)\bpassword\s*=\s*\S+`)
)

// dsnParseError is what with the DSN parse's own reason, any password it
// quotes removed (pgx redacts its own; this also covers a quoted URL); err
// is kept for errors.Is and errors.As.
func dsnParseError(what string, err error) error {
	text := dsnUserinfo.ReplaceAllString(err.Error(), "://<redacted>@")
	text = dsnPassword.ReplaceAllString(text, "password=<redacted>")
	return &parseError{text: what + ": " + text, err: err}
}

type parseError struct {
	text string
	err  error
}

func (e *parseError) Error() string { return e.text }
func (e *parseError) Unwrap() error { return e.err }

// Open connects to the store at dsn (NOVA_PG_DSN, no password in it) with
// password (NOVA_PG_PASSWORD, may be empty). It never quotes the dsn back.
func Open(ctx context.Context, dsn, password string) (*PG, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, dsnParseError("store: the postgres dsn could not be parsed", err)
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

// Migrate applies schema.sql to an empty store or one at SchemaVersion made
// by this build of it, and refuses a store at any other version, or at this
// version with another schema hash (sprint.meta), naming both, and a schema
// sprint that holds anything but no sprint.meta (not made by nova-sprint):
// any object of any kind in the namespace, which PostgreSQL records as a
// pg_depend row on it (a relation, type, function, operator, collation, text
// search object, operator family, ...). The two other hashes it takes are
// round16Hash and round21Hash: schema.sql adds cards.red to a round 16
// store and drops the five configuration columns of sprint.rows from either
// (round 22), and the new hash is recorded, in the same transaction. When schema sprint already exists it
// skips CREATE SCHEMA (createSchema), so the schema's owner needs no CREATE
// on the database (round 19). Twice is a no-op, and concurrent Migrates are safe: each holds one
// advisory lock.
func (p *PG) Migrate(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: a no-op after the commit
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLock); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	present, meta, used, _, err := namespace(ctx, tx) // the foreign check CheckSchema shares (schema.go)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if used && !meta {
		return fmt.Errorf("store: migrate: %w", errForeign)
	}
	if meta {
		v, hash, _, err := readMeta(ctx, tx) // no row is version 0, refused below
		if err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		if err := metaRefusal(v, hash); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	sql := schemaSQL
	if present {
		sql = schemaObjects // no CREATE SCHEMA: the owner may lack CREATE on the database
	}
	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE sprint.meta SET hash = $1 WHERE id = 1", schemaHash); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if err := freshStop(ctx, tx, meta); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// freshStop stops a store Migrate has just made (there was no sprint.meta
// before it), with FreshStop's reason, in Migrate's transaction: a fresh
// store moves nothing until the coordinator's first start. The column's
// default stays false, so the schema hash is unchanged and a store made
// before this keeps its own stop.
func freshStop(ctx context.Context, tx pgx.Tx, existed bool) error {
	if existed {
		return nil
	}
	if _, err := tx.Exec(ctx, "UPDATE sprint.epoch SET stopped = $1, stop_reason = $2 WHERE id = 1", FreshStop.Stopped, FreshStop.Reason); err != nil {
		return fmt.Errorf("store: migrate: the fresh store's stop (%s) was not written: %w", FreshStop.Reason, err)
	}
	return nil
}

// known is a failure inside the transaction, before COMMIT, whose outcome
// is known, as a plain error: an invalid body, an error the server returned
// (any SQLSTATE), or one the client raised (a scan, an encode, a receipt that
// does not decode, a counter at the top of its column). The transaction
// rolls back, so nothing landed. It is nil, the outcome unknown, when ctx is
// done, or when the connection was lost after a request may have reached the
// server (pgconn.SafeToRetry says when none did, and that is known).
func known(ctx context.Context, closed bool, op Op, err error) error {
	var pe *pgconn.PgError
	isPG := errors.As(err, &pe)
	switch {
	case errors.Is(err, ErrInvalid):
		return err
	case ctx.Err() != nil:
		return nil
	case pgconn.SafeToRetry(err):
		// Known: the request never reached the server, so nothing can have
		// landed. Leave the switch for the plain error below, as for any
		// error before COMMIT, ahead of the lost-connection case.
	case closed || isPG && lost(pe):
		return nil
	}
	if !isPG {
		return fmt.Errorf("store: op %s failed before COMMIT, so nothing landed: %w", op.ID, err)
	}
	on := ""
	switch {
	case pe.ConstraintName != "":
		on = " on constraint " + pe.ConstraintName
	case pe.ColumnName != "":
		on = " on column " + pe.ColumnName
	case pe.TableName != "":
		on = " on table " + pe.TableName
	}
	return fmt.Errorf("store: op %s: postgres refused it before COMMIT, so nothing landed: SQLSTATE %s%s: %s: %w", op.ID, pe.Code, on, pe.Message, err)
}

// lost is a server error that ends the session rather than answering the
// statement (severity FATAL or PANIC, or a connection exception, class 08):
// a connection error, so the outcome is unknown.
func lost(pe *pgconn.PgError) bool {
	return pe.Severity == "FATAL" || pe.Severity == "PANIC" || strings.HasPrefix(pe.Code, "08")
}

// write runs protocol in one READ COMMITTED transaction holding the writer lock,
// so writers run one at a time and nothing needs a retry. A failure after
// BEGIN and before COMMIT is a plain error unless the connection or ctx went
// (known, above); any error during or after COMMIT is ErrUnknown. A panic rolls the transaction
// back, releasing the lock, and is re-raised.
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
		if k := known(ctx, tx.Conn().IsClosed(), op, err); k != nil {
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
	if _, err := tx.Exec(ctx, "SAVEPOINT writes"); err != nil { // discard rolls back to it
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
func (p *PG) Apply(ctx context.Context, op Op) (Receipt, error) { return applyOp(p.writer(ctx), op) }

// Clear opens a new epoch, carrying every row into it, in one transaction.
func (p *PG) Clear(ctx context.Context, op Op) (Receipt, error) { return clearOp(p.writer(ctx), op) }

func (p *PG) writer(ctx context.Context) writer {
	return func(op Op, valid func(Op) error, rule rules) (Receipt, error) { return p.write(ctx, op, valid, rule) }
}

func jsonArg(s *State) any {
	if s == nil {
		return nil
	}
	return string(encode(s))
}

// replaySQL is the stored receipt of an op id with its event's after-image,
// which the receipt does not repeat (readReceipt).
const replaySQL = "SELECT receipt, after FROM sprint.events WHERE op = $1"

func scanReplay(row pgx.Row) (Receipt, bool, error) {
	var b, after []byte
	err := row.Scan(&b, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, err
	}
	a, err := readState(after)
	if err != nil {
		return Receipt{}, false, err
	}
	r, err := readReceipt(b, a)
	return r, err == nil, err
}

// Replay returns the stored receipt of an op id, and false when it is
// unknown (an id no op could have, empty, over MaxName bytes or holding a NUL, is unknown).
func (p *PG) Replay(ctx context.Context, opID string) (Receipt, bool, error) {
	if checkID(Op{ID: opID}) != nil {
		return Receipt{}, false, nil
	}
	r, ok, err := scanReplay(p.pool.QueryRow(ctx, replaySQL, opID))
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
