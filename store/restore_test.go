package store

// Round 20: RestoreBegin and RestoreEnd, layer 6's restore of a data-only
// dump on one connection in one transaction the caller owns.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows Begin leaves for the dump are schema.sql's only seed rows.
func TestRestoreDDL(t *testing.T) {
	assert.Equal(t, 1, strings.Count(schemaSQL, seedMeta))
	assert.Equal(t, 1, strings.Count(schemaSQL, seedEpoch))
	assert.Equal(t, 2, strings.Count(schemaSQL, "INSERT INTO"))
	assert.NotContains(t, restoreDDL, "INSERT INTO")
	assert.NotContains(t, restoreDDL, "CREATE SCHEMA")
}

// restoreSeed fills a store at epoch 2 with a policy, judgements, a cursor
// and a producer updated after its consumers (so its row follows theirs in
// the table), ending with the judgement and cursor op.
func restoreSeed(h *harness) {
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "{}"}, SetActivePolicy{1}))
	h.clear()
	h.seed()
	h.ok(h.op("reason", nil, SetCardReason{"c1", "r"}, SetCardReason{"c2", "s"}))
	h.ok(h.op("open", nil, PutJudgement{ID: "j1", Kind: "red", About: "c1"}, SetCursor{"feed", 1}))
}

// dump is pg_dump --data-only --schema=sprint of the database at dsn.
func dump(t *testing.T, dsn string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(pgBin(t), "pg_dump"), "--data-only", "--schema=sprint", "-d", dsn)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stderr = os.Stderr // pg_dump warns that sprint.cards references itself
	out, err := cmd.Output()
	require.NoError(t, err)
	return string(out)
}

// loadDump runs a plain-format dump on tx's connection as psql would: each
// statement by the simple protocol, each COPY ... FROM stdin with its data
// lines, skipping comments and psql's backslash commands (\restrict).
func loadDump(ctx context.Context, tx pgx.Tx, dump string) error {
	lines := strings.Split(dump, "\n")
	var stmt strings.Builder
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if stmt.Len() == 0 && (line == "" || strings.HasPrefix(line, "--") || strings.HasPrefix(line, `\`)) {
			continue
		}
		stmt.WriteString(line + "\n")
		if !strings.HasSuffix(line, ";") {
			continue
		}
		q := stmt.String()
		stmt.Reset()
		if !strings.HasPrefix(q, "COPY ") {
			if _, err := tx.Conn().PgConn().Exec(ctx, q).ReadAll(); err != nil {
				return err
			}
			continue
		}
		var data strings.Builder
		for i++; lines[i] != `\.`; i++ {
			data.WriteString(lines[i] + "\n")
		}
		if _, err := tx.Conn().PgConn().CopyFrom(ctx, strings.NewReader(data.String()), q); err != nil {
			return err
		}
	}
	return nil
}

// restore runs Begin, the load and End in one transaction on one
// connection to dsn, committing only when End accepts.
func restore(t *testing.T, dsn, dump string) (SchemaIdentity, error) {
	ctx := context.Background()
	tx, err := connect(t, dsn).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: a no-op after the commit
	require.NoError(t, RestoreBegin(ctx, tx))
	require.NoError(t, loadDump(ctx, tx, dump))
	id, err := RestoreEnd(ctx, tx)
	if err == nil {
		require.NoError(t, tx.Commit(ctx))
	}
	return id, err
}

func newDatabase(t *testing.T, dsn, name string) string {
	t.Helper()
	_, err := connect(t, dsn).Exec(context.Background(), "CREATE DATABASE "+name)
	require.NoError(t, err)
	return strings.Replace(dsn, "/postgres?", "/"+name+"?", 1)
}

// A seeded store dumped data-only and restored into an empty database reads
// back the same snapshot and log, takes the next op as the original does,
// and refuses a second restore. Without the trigger disabled for the load,
// the dump does not load (the witness).
func TestFunctionalRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, p, dsn := pgHarness(t)
	restoreSeed(h)
	want := h.snap()
	d := dump(t, dsn)
	for _, line := range strings.Split(d, "\n") {
		if strings.HasPrefix(line, "COPY ") {
			t.Log(line)
		}
	}
	assert.Contains(t, d, "COPY sprint.meta (id, schema, hash) FROM stdin;\n1\t2\t"+schemaHash+"\n")
	assert.Contains(t, d, "COPY sprint.epoch (id, n, stopped, stop_reason) FROM stdin;\n1\t2\tf\t\n")

	target := newDatabase(t, dsn, "restored")
	tx, err := connect(t, target).Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, RestoreBegin(ctx, tx))
	_, err = tx.Exec(ctx, "ALTER TABLE sprint.cards ENABLE TRIGGER cards_fixed")
	require.NoError(t, err)
	require.ErrorContains(t, loadDump(ctx, tx, d), "card u1 is a consumer of c1, which is not a producer", "a consumer's row comes before its updated producer's")
	require.NoError(t, tx.Rollback(ctx))

	id, err := restore(t, target, d)
	require.NoError(t, err)
	assert.Equal(t, SchemaIdentity{Present: true, Hash: schemaHash, Expected: schemaHash, Current: true, Owner: "nova", Round: schemaRound}, id)
	q, err := Open(ctx, target, "")
	require.NoError(t, err)
	t.Cleanup(q.Close)
	got, err := q.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	wantEv, err := p.Events(ctx, 0, 0)
	require.NoError(t, err)
	gotEv, err := q.Events(ctx, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, wantEv, gotEv)
	next := h.op("next", []Expect{AtSeq{want.Seq}}, SetCardState{"u1", "working"}, PutPolicy{"data", "q", "{}"})
	a, err := p.Apply(ctx, next)
	require.NoError(t, err)
	b, err := q.Apply(ctx, next)
	require.NoError(t, err)
	assert.Equal(t, a, b, "the restored store takes the next op as the original does")
	require.NoError(t, q.Migrate(ctx), "Migrate takes the restored store")

	tx, err = connect(t, target).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: refused, nothing to keep
	err = RestoreBegin(ctx, tx)
	require.Error(t, err)
	assert.Equal(t, "store: restore: sprint.cards has rows; a restore loads only into an empty store: use a new database, or drop schema sprint", err.Error())
}

// End refuses a dump whose events do not rebuild to its tables (the last
// event's line cut from the events' COPY: the tables hold its judgement and
// cursor, the log does not; seq is the log's, so it agrees) and a foreign schema is refused
// by Begin.
func TestFunctionalRestoreRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, _, dsn := pgHarness(t)
	restoreSeed(h)
	want := h.snap()
	d := dump(t, dsn)
	head, rest, ok := strings.Cut(d, "COPY sprint.events ")
	require.True(t, ok)
	block, tail, ok := strings.Cut(rest, "\n\\.\n")
	require.True(t, ok)
	block = block[:strings.LastIndex(block, "\n")]
	_, err := restore(t, newDatabase(t, dsn, "cut"), head+"COPY sprint.events "+block+"\n\\.\n"+tail)
	require.Error(t, err)
	assert.Equal(t, "store: restore: the dump does not hold together: rebuilding the "+strconv.FormatInt(want.Seq-1, 10)+" events gives a state that differs from the snapshot in judgements, cursors", err.Error())

	foreign := newDatabase(t, dsn, "alien")
	_, err = connect(t, foreign).Exec(ctx, "CREATE SCHEMA sprint; CREATE TABLE sprint.cards (id text)")
	require.NoError(t, err)
	tx, err := connect(t, foreign).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: refused
	err = RestoreBegin(ctx, tx)
	require.Error(t, err)
	assert.Equal(t, "store: restore: schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere", err.Error())
}
