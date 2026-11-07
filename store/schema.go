package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// schemaRound is the review round whose schema.sql this binary carries: round
// 23 added cards.model (22 dropped the five configuration columns of
// sprint.rows; 17 added cards.red; no round between changed the schema).
const schemaRound = 23

// SchemaIdentity is what CheckSchema found in a database. Expected is the
// schema hash this binary carries, the one Migrate records (the only place it
// is computed, hashSchema); Hash is the one sprint.meta records ("" when there
// is none). Present is schema sprint existing at all. Current is a store at
// SchemaVersion made by this binary's schema; KnownOlder is one Migrate
// upgrades in place (round16Hash, round21Hash, round22Hash). Round is the round whose schema made the
// store (0 when it is neither). Owner is the role that owns schema sprint.
type SchemaIdentity struct {
	Present    bool
	Hash       string
	Expected   string
	Current    bool
	KnownOlder bool
	Owner      string
	Round      int
}

// errForeign is the refusal of a schema sprint nova-sprint did not make.
var errForeign = errors.New("schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere")

// queryRower is what the schema reads need: a transaction or a connection.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// namespace is schema sprint as the catalogs see it: whether it exists, its
// owner, whether sprint.meta is in it, and whether anything at all depends on
// it (any object of any kind is a pg_depend row on the namespace: a relation,
// type, function, operator, collation, text search object, operator family,
// default privileges, ...). Migrate and CheckSchema both decide "foreign" from
// it: used and no meta. It reads only the catalogs, which every role may.
func namespace(ctx context.Context, q queryRower) (present, meta, used bool, owner string, err error) {
	var who *string
	err = q.QueryRow(ctx, `SELECT (SELECT pg_get_userbyid(nspowner)::text FROM pg_namespace WHERE nspname = 'sprint'),
		EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'sprint' AND c.relname = 'meta'),
		EXISTS (SELECT 1 FROM pg_depend d JOIN pg_namespace n ON n.oid = d.refobjid
			WHERE d.refclassid = 'pg_namespace'::regclass AND n.nspname = 'sprint')`).Scan(&who, &meta, &used)
	if err != nil || who == nil {
		return false, false, false, "", err
	}
	return true, meta, used, *who, nil
}

// readMeta reads sprint.meta's version and hash; found is false when it has
// no row. to_jsonb reads hash from a meta made before the column existed as "".
func readMeta(ctx context.Context, q queryRower) (version int, hash string, found bool, err error) {
	err = q.QueryRow(ctx, "SELECT m.schema, coalesce(to_jsonb(m)->>'hash', '') FROM sprint.meta m WHERE id = 1").Scan(&version, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	return version, hash, err == nil, err
}

// metaRefusal is why Migrate refuses a store at version and hash, naming the
// fix, or nil when it takes it (this binary's hash or a knownOlder one).
func metaRefusal(version int, hash string) error {
	if version != SchemaVersion {
		return fmt.Errorf("the store is at schema version %d, this binary knows schema version %d: run a binary built for schema version %d, or recreate the store", version, SchemaVersion, version)
	}
	if hash != schemaHash && !knownOlder(hash) {
		return fmt.Errorf("the store's schema hash is %s, this binary's is %s: this store was made by a different build of schema 2; recreate it (no live store is at v2 yet)", cmp.Or(hash, "none"), schemaHash)
	}
	return nil
}

// CheckSchema reports which schema the database at dsn holds, and changes
// nothing: it reads the catalogs and sprint.meta in one READ ONLY
// transaction, which it rolls back, so a role that may only read can run it.
// The password, if any, comes as pgx takes it (PGPASSWORD or a pgpass file);
// the dsn is never quoted back. A store Migrate would take (current or a
// known older round) is no error. Every other outcome returns what was found
// with an error that names the fix: no database, no schema sprint (or an empty
// one), a foreign schema, a sprint.meta that cannot be read, or a store
// Migrate refuses (another version or hash, its own words).
func CheckSchema(ctx context.Context, dsn string) (SchemaIdentity, error) {
	id := SchemaIdentity{Expected: schemaHash}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return id, dsnParseError("store: check schema: the postgres dsn could not be parsed", err)
	}
	c, err := pgx.ConnectConfig(ctx, cfg)
	var pe *pgconn.PgError
	switch {
	case errors.As(err, &pe) && pe.Code == "3D000": // invalid_catalog_name
		return id, fmt.Errorf("store: check schema: database %q does not exist on %s:%d: create it, or point NOVA_PG_DSN at the store's database", cfg.Database, cfg.Host, cfg.Port)
	case err != nil:
		return id, fmt.Errorf("store: check schema: cannot reach postgres at %s:%d: %w", cfg.Host, cfg.Port, err)
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }() // ignored: a read-only connection
	return identifyIn(ctx, func(ctx context.Context) (pgx.Tx, error) {
		return c.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	})
}

// Schema is CheckSchema on the store's own pool.
func (p *PG) Schema(ctx context.Context) (SchemaIdentity, error) {
	return identifyIn(ctx, func(ctx context.Context) (pgx.Tx, error) {
		return p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	})
}

// Schema is the Fake's identity: it is always the current schema, made by
// nothing (no owner).
func (f *Fake) Schema(context.Context) (SchemaIdentity, error) {
	return SchemaIdentity{Present: true, Hash: schemaHash, Expected: schemaHash, Current: true, Round: schemaRound}, nil
}

// identifyIn runs identify in a read-only transaction begun by begin and
// rolls it back.
func identifyIn(ctx context.Context, begin func(context.Context) (pgx.Tx, error)) (SchemaIdentity, error) {
	tx, err := begin(ctx)
	if err != nil {
		return SchemaIdentity{Expected: schemaHash}, fmt.Errorf("store: check schema: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // ignored: nothing to undo
	return identify(ctx, tx)
}

// identify reads what CheckSchema reports, with the same checks Migrate makes
// (namespace, readMeta, metaRefusal), in Migrate's order.
func identify(ctx context.Context, q queryRower) (SchemaIdentity, error) {
	id := SchemaIdentity{Expected: schemaHash}
	present, meta, used, owner, err := namespace(ctx, q)
	switch {
	case err != nil:
		return id, fmt.Errorf("store: check schema: the catalogs could not be read: %w", err)
	case !present:
		return id, errors.New("store: check schema: there is no schema sprint in this database: run Migrate to create the store, or point NOVA_PG_DSN at the store's database")
	}
	id.Present, id.Owner = true, owner
	switch {
	case used && !meta:
		return id, fmt.Errorf("store: check schema: %w", errForeign)
	case !meta:
		return id, errors.New("store: check schema: schema sprint is empty: run Migrate to create the store in it")
	}
	version, hash, found, err := readMeta(ctx, q)
	switch {
	case err != nil:
		return id, fmt.Errorf("store: check schema: sprint.meta could not be read: grant this role USAGE on schema sprint and SELECT on sprint.meta, or check as %s: %w", owner, err)
	case !found:
		return id, errors.New("store: check schema: sprint.meta has no row, so the store's schema is unknown: recreate the store")
	}
	id.Hash = hash
	if err := metaRefusal(version, hash); err != nil {
		return id, fmt.Errorf("store: check schema: %w", err)
	}
	id.Current, id.KnownOlder = hash == schemaHash, knownOlder(hash)
	id.Round = map[string]int{schemaHash: schemaRound, round16Hash: 16, round21Hash: 17, round22Hash: 22}[hash] // round 17's schema, carried to 21
	return id, nil
}

// knownOlder says whether hash is an earlier build of this version that
// Migrate upgrades in place.
func knownOlder(hash string) bool {
	return hash == round16Hash || hash == round21Hash || hash == round22Hash
}
