package store

// CheckSchema and Schema, the read-only schema identity. The
// conformance scenario runs on Fake and PG; the functional tests are the
// outcomes on a real PostgreSQL and the proof that it writes nothing.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// currentSchema is the identity of a store made by this binary.
var currentSchema = SchemaIdentity{Present: true, Hash: schemaHash, Expected: schemaHash, Current: true, Generation: schemaGeneration}

// schemaIdentity is the conformance scenario: both stores report the current
// schema (Fake has no owner; PG's is its role, so it is not compared).
func schemaIdentity(h *harness) {
	s, ok := h.s.(interface {
		Schema(context.Context) (SchemaIdentity, error)
	})
	require.True(h.t, ok, "every store reports its schema")
	h.seed()
	id, err := s.Schema(h.ctx)
	require.NoError(h.t, err)
	id.Owner = ""
	assert.Equal(h.t, currentSchema, id)
}

func TestSchemaFake(t *testing.T) {
	t.Parallel()
	id, err := NewFake().Schema(context.Background())
	require.NoError(t, err)
	assert.Equal(t, currentSchema, id)
	assert.Equal(t, hashSchema(schemaSQL), id.Expected, "the expectation is the hash Migrate records")
	t.Logf("schema hash %s, round %d", id.Expected, id.Generation)
}

func TestFunctionalCheckSchemaCurrent(t *testing.T) {
	t.Parallel()
	_, p, dsn := pgHarness(t)
	want := currentSchema
	want.Owner = "nova"
	id, err := CheckSchema(context.Background(), dsn)
	require.NoError(t, err)
	assert.Equal(t, want, id)
	id, err = p.Schema(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, id, "Schema on the pool is CheckSchema")
}

// A round-16 store is a known older one before Migrate and the current one
// after it.
func TestFunctionalCheckSchemaOlderSchema1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	old, err := os.ReadFile("testdata/schema-older-1.sql")
	require.NoError(t, err)
	dsn := startPG(t)
	c := connect(t, dsn)
	for _, q := range []string{string(old), "UPDATE sprint.meta SET hash = '" + olderSchema1Hash + "' WHERE id = 1"} {
		_, err = c.Exec(ctx, q)
		require.NoError(t, err, q)
	}
	id, err := CheckSchema(ctx, dsn)
	require.NoError(t, err, "Migrate would take it")
	assert.Equal(t, SchemaIdentity{Present: true, Hash: olderSchema1Hash, Expected: schemaHash, KnownOlder: true, Owner: "nova", Generation: 1}, id)
	openPG(t, dsn) // Migrate
	id, err = CheckSchema(ctx, dsn)
	require.NoError(t, err)
	want := currentSchema
	want.Owner = "nova"
	assert.Equal(t, want, id)
}

// Every outcome that is not a store Migrate takes is an error naming the fix,
// with what was found.
func TestFunctionalCheckSchemaRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := startPG(t)
	c := connect(t, dsn)
	check := func(dsn string, want SchemaIdentity, msg string) {
		t.Helper()
		id, err := CheckSchema(ctx, dsn)
		require.Error(t, err)
		assert.Equal(t, msg, err.Error())
		assert.Equal(t, want, id)
	}
	none := SchemaIdentity{Expected: schemaHash}
	found := SchemaIdentity{Present: true, Expected: schemaHash, Owner: "nova"}
	host := strings.TrimSuffix(strings.TrimPrefix(dsn, "postgres://nova@"), "/postgres?sslmode=disable")
	check(strings.Replace(dsn, "/postgres?", "/nope?", 1), none, `store: check schema: database "nope" does not exist on `+host+`: create it, or point NOVA_PG_DSN at the store's database`)
	check(dsn, none, "store: check schema: there is no schema sprint in this database: run Migrate to create the store, or point NOVA_PG_DSN at the store's database")
	_, err := c.Exec(ctx, "CREATE SCHEMA sprint")
	require.NoError(t, err)
	check(dsn, found, "store: check schema: schema sprint is empty: run Migrate to create the store in it")
	_, err = c.Exec(ctx, "CREATE TABLE sprint.cards (id text PRIMARY KEY)")
	require.NoError(t, err)
	check(dsn, found, "store: check schema: schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere")
	_, err = c.Exec(ctx, "DROP SCHEMA sprint CASCADE")
	require.NoError(t, err)
	openPG(t, dsn)
	_, err = c.Exec(ctx, "UPDATE sprint.meta SET hash = 'abc'")
	require.NoError(t, err)
	found.Hash = "abc"
	check(dsn, found, "store: check schema: the store's schema hash is abc, this binary's is "+schemaHash+": "+staleShape)
	_, err = c.Exec(ctx, "UPDATE sprint.meta SET schema = 7")
	require.NoError(t, err)
	check(dsn, found, "store: check schema: the store is at schema version 7, this binary knows schema version 2: run a binary built for schema version 7, or recreate the store")
	_, err = c.Exec(ctx, "CREATE ROLE stranger LOGIN")
	require.NoError(t, err)
	_, err = CheckSchema(ctx, strings.Replace(dsn, "nova@", "stranger@", 1))
	require.ErrorContains(t, err, "store: check schema: sprint.meta could not be read: grant this role USAGE on schema sprint and SELECT on sprint.meta, or check as nova: ")
}

// storeWrites is what a write anywhere in the store would move: the sprint
// tables' insert, update and delete counts, the events, and the next
// transaction id (a read-only transaction never takes one). c has
// stats_fetch_consistency none, so each read is fresh.
func storeWrites(t *testing.T, ctx context.Context, c *pgx.Conn) [5]int64 {
	t.Helper()
	var w [5]int64
	require.NoError(t, c.QueryRow(ctx, `SELECT coalesce(sum(n_tup_ins), 0), coalesce(sum(n_tup_upd), 0), coalesce(sum(n_tup_del), 0),
		(SELECT count(*) FROM sprint.events), pg_snapshot_xmax(pg_current_snapshot())::text::bigint
		FROM pg_stat_user_tables WHERE schemaname = 'sprint'`).Scan(&w[0], &w[1], &w[2], &w[3], &w[4]))
	return w
}

// storeRows is every row in the sprint tables: nothing is ever deleted, so
// once every writer's statistics are flushed the insert count equals it.
func storeRows(t *testing.T, ctx context.Context, c *pgx.Conn) int64 {
	t.Helper()
	tables, err := c.Query(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = 'sprint' AND table_type = 'BASE TABLE'")
	require.NoError(t, err)
	names, err := pgx.CollectRows(tables, pgx.RowTo[string])
	require.NoError(t, err)
	var all int64
	for _, name := range names {
		var n int64
		require.NoError(t, c.QueryRow(ctx, "SELECT count(*) FROM sprint."+pgx.Identifier{name}.Sanitize()).Scan(&n))
		all += n
	}
	return all
}

// CheckSchema writes nothing: on a store with ops in it, as its owner and as
// a role that may only read (default_transaction_read_only, SELECT on meta
// alone), no table's insert, update or delete count moves, the events stay
// as they were, and no transaction id is taken.
func TestFunctionalCheckSchemaWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, p, dsn := pgHarness(t)
	h.seed()
	h.ok(h.op("take", nil, SetCardReason{"c1", "r"}))
	p.Close() // each backend flushes its statistics as it exits
	c := connect(t, dsn)
	for _, q := range []string{"CREATE ROLE reader LOGIN", "ALTER ROLE reader SET default_transaction_read_only = on",
		"GRANT USAGE ON SCHEMA sprint TO reader", "GRANT SELECT ON sprint.meta TO reader"} {
		_, err := c.Exec(ctx, q)
		require.NoError(t, err, q)
	}
	_, err := c.Exec(ctx, "SET stats_fetch_consistency = none")
	require.NoError(t, err)
	all := storeRows(t, ctx, c)
	before := storeWrites(t, ctx, c)
	for deadline := time.Now().Add(wait); before[0] != all; before = storeWrites(t, ctx, c) { // on this goroutine: c is not shared
		require.True(t, time.Now().Before(deadline), "every writer's statistics are flushed: %d inserts, %d rows", before[0], all)
		time.Sleep(50 * time.Millisecond)
	}
	require.Positive(t, before[3], "the store has events")
	want := currentSchema
	want.Owner = "nova"
	for _, as := range []string{dsn, strings.Replace(dsn, "nova@", "reader@", 1)} {
		id, err := CheckSchema(ctx, as)
		require.NoError(t, err)
		assert.Equal(t, want, id)
	}
	for range 20 { // two seconds for a write's statistics to arrive, as above
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, before, storeWrites(t, ctx, c), "CheckSchema changed the store")
	}
}
