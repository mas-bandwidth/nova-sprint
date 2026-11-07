package store

// RestoreBegin and RestoreEnd, layer 6's restore of a data-only
// dump on one connection in one transaction the caller owns.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows Begin leaves for the dump are schema.sql's only seed rows.
func TestRestoreDDL(t *testing.T) {
	t.Parallel()
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

// digestOf is the dump's digest as layer 6 computes it: sha256 of its bytes.
func digestOf(dump string) string {
	sum := sha256.Sum256([]byte(dump))
	return hex.EncodeToString(sum[:])
}

// restore runs Begin, the load and End in one transaction on one
// connection to dsn, committing only when End accepts.
func restore(t *testing.T, dsn, dump string) (SchemaIdentity, error) {
	return restoreAs(t, dsn, dump, digestOf(dump))
}

// restoreAs is restore with the digest the caller passes.
func restoreAs(t *testing.T, dsn, dump, digest string) (SchemaIdentity, error) {
	ctx := context.Background()
	tx, err := connect(t, dsn).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: a no-op after the commit
	require.NoError(t, RestoreBegin(ctx, tx, digest))
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
	assert.Contains(t, d, "COPY sprint.meta (id, schema, hash, restored_at, restored_digest) FROM stdin;\n1\t2\t"+schemaHash+"\t\\N\t\n")
	assert.Contains(t, d, "COPY sprint.epoch (id, n, stopped, stop_reason, commit_name, commit_email) FROM stdin;\n1\t2\tt\t"+NeverStarted+"\t\t\n")

	target := newDatabase(t, dsn, "restored")
	tx, err := connect(t, target).Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, RestoreBegin(ctx, tx, digestOf(d)))
	_, err = tx.Exec(ctx, "ALTER TABLE sprint.cards ENABLE TRIGGER cards_fixed")
	require.NoError(t, err)
	require.ErrorContains(t, loadDump(ctx, tx, d), "card u1 is a consumer of c1, which is not a producer", "a consumer's row comes before its updated producer's")
	require.NoError(t, tx.Rollback(ctx))

	id, err := restore(t, target, d)
	require.NoError(t, err)
	assert.Equal(t, SchemaIdentity{Present: true, Hash: schemaHash, Expected: schemaHash, Current: true, Owner: "nova", Generation: schemaGeneration}, id)
	q, err := Open(ctx, target, "")
	require.NoError(t, err)
	t.Cleanup(q.Close)
	_, _, ok, err := p.Restored(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "the original was never restored")
	digest, restoredAt, ok, err := q.Restored(ctx)
	require.NoError(t, err)
	require.True(t, ok, "the restore is recorded in sprint.meta")
	assert.Equal(t, digestOf(d), digest, "with the dump's digest: a retry of the same dump can tell it is in")
	assert.False(t, restoredAt.IsZero())
	// The restore unhooked w1 (the seed's sess-a) in one logged op at the
	// restore's time; the same op on the original makes the two stores one.
	unhook, ok := unhookOp(want, digest, restoredAt)
	require.True(t, ok)
	assert.Equal(t, fmt.Sprintf("restore-unhook-%s-%d", digest, want.Seq), unhook.ID)
	u, err := p.Apply(ctx, unhook)
	require.NoError(t, err)
	require.True(t, u.Accepted, u.Reason)
	w1 := want.Rows["w1"]
	want, err = p.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, Row{Name: "w1", State: w1.State, Flaps: w1.Flaps, Held: w1.Held, Reason: w1.Reason, Gen: w1.Gen + 1}, want.Rows["w1"], "unhooked, its gen raised, the rest kept")
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
	err = RestoreBegin(ctx, tx, digestOf(d))
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
	err = RestoreBegin(ctx, tx, digestOf(d))
	require.Error(t, err)
	assert.Equal(t, "store: restore: schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere", err.Error())
}

// The restore's identity: Begin refuses a digest that is not a sha256, End
// refuses a transaction Begin's digest is not in, and End refuses a restore
// that would leave a row hooked, naming it. The last is a dump whose own log
// already holds the unhook's id (made here on purpose; a restore-unhook of
// another restore at the same seq would do it): the replayed id unhooks
// nothing.
func TestFunctionalRestoreIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, _, dsn := pgHarness(t)
	restoreSeed(h)
	d := dump(t, dsn)
	tx, err := connect(t, newDatabase(t, dsn, "baddigest")).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: refused
	for _, bad := range []string{"", "abc", strings.ToUpper(digestOf(d))} {
		err = RestoreBegin(ctx, tx, bad)
		require.Error(t, err, bad)
		assert.Equal(t, fmt.Sprintf("store: restore: the dump's digest %q is not a sha256 in lowercase hex (64 digits): pass the sha256 of the dump's bytes", bad), err.Error())
	}

	tx, err = connect(t, newDatabase(t, dsn, "nodigest")).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() // ignored: refused
	require.NoError(t, RestoreBegin(ctx, tx, digestOf(d)))
	require.NoError(t, loadDump(ctx, tx, d))
	_, err = tx.Exec(ctx, "SELECT set_config($1, '', true)", digestSetting)
	require.NoError(t, err)
	_, err = RestoreEnd(ctx, tx)
	require.Error(t, err)
	assert.Equal(t, "store: restore: RestoreBegin did not run in this transaction (no dump digest): run RestoreBegin, the load and RestoreEnd in one transaction", err.Error())

	other := strings.Repeat("ab", 32)
	h.ok(Op{ID: fmt.Sprintf("restore-unhook-%s-%d", other, h.snap().Seq+1), Epoch: h.epoch, Actor: "tester", Kind: "hook", At: t0,
		Writes: []Write{PutRow{Name: "w9", Session: "sess-z", Role: "worker"}}})
	_, err = restoreAs(t, newDatabase(t, dsn, "collide"), dump(t, dsn), other)
	require.Error(t, err)
	assert.Equal(t, "store: restore: row w1 is still hooked after the unhook: the restore would leave a hook from the dump live", err.Error())
}

// unhookOp unhooks every hooked row of a snapshot in one op, raising each
// one's gen and nothing else, and is not needed when no row is hooked
// (unit, the Fake: RestoreEnd runs it on PostgreSQL).
func TestUnhookOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := NewFake()
	when := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	r, err := f.Apply(ctx, Op{ID: "hook", Epoch: 1, At: when, Writes: []Write{
		PutRow{Name: "w1", Session: "x", Role: "worker"}, PutRow{Name: "w2"}, PutRow{Name: "w3", Session: "y", Role: "coordinator"}, SetRowState{"w1", "up"}}})
	require.NoError(t, err)
	require.True(t, r.Accepted, r.Reason)
	before, err := f.Snapshot(ctx)
	require.NoError(t, err)
	digest := strings.Repeat("0f", 32)
	op, ok := unhookOp(before, digest, when)
	require.True(t, ok)
	assert.Equal(t, Op{ID: "restore-unhook-" + digest + "-1", Epoch: 1, Actor: "store-restore", Kind: "unhook", At: when, Writes: []Write{PutRow{Name: "w1"}, PutRow{Name: "w3"}}}, op)
	r, err = f.Apply(ctx, op)
	require.NoError(t, err)
	require.True(t, r.Accepted, r.Reason)
	after, err := f.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, Row{Name: "w1", State: "up", Gen: 1}, after.Rows["w1"])
	assert.Equal(t, before.Rows["w2"], after.Rows["w2"], "an unhooked row is not written")
	assert.Equal(t, Row{Name: "w3", State: "down", Gen: 1}, after.Rows["w3"])
	_, ok = unhookOp(after, digest, when)
	assert.False(t, ok, "nothing hooked: no op")
	_, _, ok, err = f.Restored(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a Fake is never restored")
}
