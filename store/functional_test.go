package store

// Functional tests: a real throwaway PostgreSQL per test (initdb into
// t.TempDir(), a free port, fsync off). Skipped when initdb is not found.
// Run them alone with: go test -run Functional ./store/

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wait = 30 * time.Second

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	return port
}

// pgBin finds initdb: on PATH, else Homebrew's postgresql@18, else Debian's.
func pgBin(t *testing.T) string {
	if p, err := exec.LookPath("initdb"); err == nil {
		return filepath.Dir(p)
	}
	found, _ := filepath.Glob("/opt/homebrew/opt/postgresql@18/bin/initdb") // ignored: a bad pattern is a constant's
	debian, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb")          // ignored: as above
	if found = append(found, debian...); len(found) > 0 {
		return filepath.Dir(found[0])
	}
	t.Skip("functional tests need PostgreSQL: initdb is not on PATH, in /opt/homebrew/opt/postgresql@18/bin or /usr/lib/postgresql/*/bin")
	return ""
}

// startPG starts a PostgreSQL of this test's own and returns the dsn of its
// postgres database. Unix sockets are off: a macOS temp path is too long for one.
func startPG(t *testing.T) string {
	t.Helper()
	bin, dir := pgBin(t), t.TempDir()
	// LC_ALL=C: PostgreSQL 18 on macOS refuses to start under a shell with no valid locale ("postmaster became multithreaded during startup").
	init := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8", "--no-locale")
	init.Env = append(os.Environ(), "LC_ALL=C")
	out, err := init.CombinedOutput()
	require.NoError(t, err, "%s", out)
	port := freePort(t)
	cmd := exec.Command(filepath.Join(bin, "postgres"), "-D", filepath.Join(dir, "data"), "-p", port, "-c", "listen_addresses=127.0.0.1", "-c", "unix_socket_directories=", "-c", "fsync=off")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	require.NoError(t, cmd.Start())
	// SIGQUIT is immediate shutdown: the postmaster stops its children before it exits, so none writes into the removed TempDir.
	t.Cleanup(func() { _ = cmd.Process.Signal(syscall.SIGQUIT); _ = cmd.Wait() }) // ignored: the server is ours and disposable
	dsn := "postgres://nova@127.0.0.1:" + port + "/postgres?sslmode=disable"
	require.Eventually(t, func() bool {
		c, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			_ = c.Close(context.Background()) // ignored: only probing
		}
		return err == nil
	}, wait, 50*time.Millisecond)
	return dsn
}

// openPG opens the store at dsn and applies the schema.
func openPG(t *testing.T, dsn string) *PG {
	t.Helper()
	p, err := Open(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	require.NoError(t, p.Migrate(context.Background()))
	return p
}

func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close(context.Background()) }) // ignored: closing a check connection
	return c
}

func pgHarness(t *testing.T) (*harness, *PG, string) {
	dsn := startPG(t)
	p := openPG(t, dsn)
	return &harness{t: t, s: p, ctx: context.Background(), epoch: 1}, p, dsn
}

func TestFunctionalMigrateTwice(t *testing.T) {
	t.Parallel()
	_, p, dsn := pgHarness(t)
	require.NoError(t, p.Migrate(context.Background()), "a second Migrate is a no-op")
	c := connect(t, dsn)
	var rows, tables, version int
	require.NoError(t, c.QueryRow(context.Background(), "SELECT count(*), (SELECT schema FROM sprint.meta) FROM sprint.epoch").Scan(&rows, &version))
	assert.Equal(t, 1, rows)
	assert.Equal(t, SchemaVersion, version)
	require.NoError(t, c.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'sprint'").Scan(&tables))
	assert.Equal(t, 9, tables)
	s, err := p.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, emptySnapshot(1), s)
}

func TestFunctionalMigrateRefusesOtherVersion(t *testing.T) {
	t.Parallel()
	_, p, dsn := pgHarness(t)
	c := connect(t, dsn)
	_, err := c.Exec(context.Background(), "UPDATE sprint.meta SET schema = 7")
	require.NoError(t, err)
	err = p.Migrate(context.Background())
	require.Error(t, err)
	assert.Equal(t, "store: migrate: the store is at schema version 7, this binary knows schema version 2: run a binary built for schema version 7, or recreate the store", err.Error())
	_, err = c.Exec(context.Background(), "UPDATE sprint.meta SET schema = 1")
	require.NoError(t, err)
	err = p.Migrate(context.Background())
	require.Error(t, err)
	assert.Equal(t, "store: migrate: the store is at schema version 1, this binary knows schema version 2: run a binary built for schema version 1, or recreate the store", err.Error(), "no upgrade path from version 1")

}

// A schema sprint that holds anything but no sprint.meta was not made by
// nova-sprint (or predates versions): Migrate refuses it and changes nothing.
// The foreign sprint.cards is from the third cold read's
// TestFunctionalColdRead3MigrateForeignSchema, which found Migrate building
// the store around it.
func TestFunctionalMigrateRefusesForeignSchema(t *testing.T) {
	t.Parallel()
	for name, ddl := range map[string]string{
		"before sprint.meta": "CREATE SCHEMA sprint; CREATE TABLE sprint.epoch (n integer)",
		"foreign cards":      "CREATE SCHEMA sprint; CREATE TABLE sprint.cards (id text PRIMARY KEY)",
		"a sequence only":    "CREATE SCHEMA sprint; CREATE SEQUENCE sprint.s",
		// the fourth cold read's MigrateForeignNonRelation: a type or a
		// function is something too, a same-name one the schema would replace
		"an enum type":                        "CREATE SCHEMA sprint; CREATE TYPE sprint.mood AS ENUM ('a')",
		"a function of the same name":         "CREATE SCHEMA sprint; CREATE FUNCTION sprint.cards_fixed() RETURNS integer LANGUAGE sql AS 'SELECT 1'",
		"a trigger function of the same name": "CREATE SCHEMA sprint; CREATE FUNCTION sprint.events_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END'",
		// the fifth cold read's MigrateForeign: any object in the namespace
		"an operator":         "CREATE SCHEMA sprint; CREATE OPERATOR sprint.<<< (LEFTARG = integer, RIGHTARG = integer, FUNCTION = int4lt)",
		"a collation":         `CREATE SCHEMA sprint; CREATE COLLATION sprint.mine FROM "C"`,
		"a text search conf":  "CREATE SCHEMA sprint; CREATE TEXT SEARCH CONFIGURATION sprint.cfg (COPY = pg_catalog.simple)",
		"an operator family":  "CREATE SCHEMA sprint; CREATE OPERATOR FAMILY sprint.fam USING btree",
		"a domain":            "CREATE SCHEMA sprint; CREATE DOMAIN sprint.d AS integer",
		"an aggregate":        "CREATE SCHEMA sprint; CREATE AGGREGATE sprint.agg (integer) (SFUNC = int4pl, STYPE = integer)",
		"a procedure":         "CREATE SCHEMA sprint; CREATE PROCEDURE sprint.p() LANGUAGE sql AS 'SELECT 1'",
		"a view":              "CREATE SCHEMA sprint; CREATE VIEW sprint.v AS SELECT 1 AS one",
		"a materialized view": "CREATE SCHEMA sprint; CREATE MATERIALIZED VIEW sprint.mv AS SELECT 1 AS one",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dsn := startPG(t)
			c := connect(t, dsn)
			_, err := c.Exec(context.Background(), ddl)
			require.NoError(t, err)
			q, err := Open(context.Background(), dsn, "")
			require.NoError(t, err)
			defer q.Close()
			err = q.Migrate(context.Background())
			require.Error(t, err)
			assert.Equal(t, "store: migrate: schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere", err.Error())
			var meta bool
			require.NoError(t, c.QueryRow(context.Background(), "SELECT to_regclass('sprint.meta') IS NOT NULL").Scan(&meta))
			assert.False(t, meta, "a refused Migrate changes nothing")
		})
	}
	dsn := startPG(t)
	_, err := connect(t, dsn).Exec(context.Background(), "CREATE SCHEMA sprint")
	require.NoError(t, err)
	q, err := Open(context.Background(), dsn, "")
	require.NoError(t, err)
	defer q.Close()
	require.NoError(t, q.Migrate(context.Background()), "an empty schema sprint is an empty store")
}

const staleShape = "this store was made by a different build of schema 2; recreate it (no live store is at v2 yet)"

// Migrate checks the shape, not only the version: a store at version 2 made
// by another build of schema 2 (another hash in sprint.meta, or none) is
// refused naming both hashes, and nothing in it changes.
func TestFunctionalMigrateRefusesOtherShape(t *testing.T) {
	t.Parallel()
	_, p, dsn := pgHarness(t)
	c := connect(t, dsn)
	var hash string
	require.NoError(t, c.QueryRow(context.Background(), "SELECT hash FROM sprint.meta").Scan(&hash))
	assert.Equal(t, schemaHash, hash, "Migrate records the hash of the schema it applied")
	_, err := c.Exec(context.Background(), "UPDATE sprint.meta SET hash = 'abc'")
	require.NoError(t, err)
	err = p.Migrate(context.Background())
	require.Error(t, err)
	assert.Equal(t, "store: migrate: the store's schema hash is abc, this binary's is "+schemaHash+": "+staleShape, err.Error())
	require.NoError(t, c.QueryRow(context.Background(), "SELECT hash FROM sprint.meta").Scan(&hash))
	assert.Equal(t, "abc", hash, "a refused Migrate changes nothing")
}

// The first build of schema 2 (b95a1414: no kind, of, result, priority, row
// gen or held, and no meta hash) is at version 2 by its meta row; Migrate
// refuses it by shape. From the second cold read's
// TestFunctionalColdRead2StaleV2Migrate, which found Migrate accepting it
// and every Apply failing after. The file is git show b95a1414:store/schema.sql,
// kept in testdata so a shallow clone runs the test.
func TestFunctionalMigrateRefusesStaleV2(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile(filepath.Join("testdata", "schema-b95a1414.sql"))
	require.NoError(t, err)
	dsn := startPG(t)
	_, err = connect(t, dsn).Exec(context.Background(), string(old))
	require.NoError(t, err)
	p, err := Open(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	err = p.Migrate(context.Background())
	require.Error(t, err)
	assert.Equal(t, "store: migrate: the store's schema hash is none, this binary's is "+schemaHash+": "+staleShape, err.Error())
}

func TestFunctionalConformance(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	admin := connect(t, dsn)
	var mu sync.Mutex
	var n atomic.Int32
	RunConformance(t, func(t *testing.T) Store {
		name := fmt.Sprintf("conf%d", n.Add(1))
		mu.Lock() // one admin connection, one statement at a time
		_, err := admin.Exec(context.Background(), "CREATE DATABASE "+name)
		mu.Unlock()
		require.NoError(t, err)
		return openPG(t, "postgres://nova@"+admin.Config().Host+":"+strconv.Itoa(int(admin.Config().Port))+"/"+name+"?sslmode=disable")
	})
}

// race applies two ops at once and returns their receipts in order.
func race(t *testing.T, s Store, a, b Op) (Receipt, Receipt) {
	got := make([]Receipt, 2)
	var wg sync.WaitGroup
	for i, o := range []Op{a, b} {
		wg.Go(func() {
			r, err := s.Apply(context.Background(), o)
			assert.NoError(t, err)
			got[i] = r
		})
	}
	wg.Wait()
	return got[0], got[1]
}

func TestFunctionalConcurrentTakes(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	h.ok(h.op("wide", nil, PutRow{Name: "w1", Kind: "worker", Width: 10, Session: "sess-a", Tiers: []string{"pro"}})) // room for every take: the take checks width
	for i := range 10 {
		id := fmt.Sprintf("k%d", i)
		h.ok(h.op("add", nil, PutCard{Tier: "pro", Kind: "work", ID: id, Of: "c1", Stream: "s1", Holder: "w1"}))
		take := func() Op {
			return h.op("take", []Expect{CardGen{id, 0}, CardHolder{id, "w1"}}, SetCardState{id, "working"})
		}
		a, b := race(t, p, take(), take())
		require.NotEqual(t, a.Accepted, b.Accepted, "exactly one of two takes of %s is accepted", id)
		for _, r := range []Receipt{a, b} {
			if !r.Accepted {
				assert.Equal(t, "card "+id+" is at gen 1, expected gen 0", r.Reason)
			}
		}
		assert.Equal(t, 1, h.snap().Cards[id].Gen)
	}
}

// Two deals on a width-1 row: the write into working counts the working cards it holds inside the
// transaction, so one is accepted and one refused, with no width condition.
func TestFunctionalWidthRace(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	for i := range 10 {
		row := fmt.Sprintf("r%d", i)
		h.ok(h.op("row", nil, PutRow{Name: row, Kind: "worker", Width: 1, Tiers: []string{"pro"}}, SetRowState{row, "up"}, PutCard{Tier: "pro", Kind: "work", ID: row + "a", Of: "c1", Stream: "s1", Holder: row}, PutCard{Tier: "pro", Kind: "work", ID: row + "b", Of: "c1", Stream: "s1", Holder: row}))
		deal := func(card string) Op {
			return h.op("deal", []Expect{RowIs{row, "up"}, CardHolder{card, row}}, SetCardState{card, "working"})
		}
		a, b := race(t, p, deal(row+"a"), deal(row+"b"))
		require.NotEqual(t, a.Accepted, b.Accepted, "exactly one deal onto %s is accepted", row)
		for _, r := range []Receipt{a, b} {
			if !r.Accepted {
				assert.Equal(t, "row "+row+" holds 1 working card, its width is 1; raise the width with set row width, or wait for a card to finish", r.Reason)
			}
		}
	}
}

// A deal fenced on NeedsLanded races a write that adds an unlanded need: the
// needs are read inside the transaction, so the deal is accepted only when it
// ran first, and is refused naming the new need otherwise.
func TestFunctionalNeedsLandedRace(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	for i := range 10 {
		id, gate := fmt.Sprintf("n%d", i), fmt.Sprintf("g%d", i)
		h.ok(h.op("add", nil, PutCard{Tier: "pro", Kind: "producer", ID: gate, Stream: "s1"}, PutCard{Tier: "pro", Kind: "producer", ID: id, Stream: "s1", Needs: []string{"c0"}}))
		deal := h.op("deal", []Expect{NeedsLanded{id}}, SetCardState{id, "ready"})
		block := h.op("block", nil, SetCardNeeds{id, []string{"c0", gate}})
		d, b := race(t, p, deal, block)
		require.True(t, b.Accepted)
		if d.Accepted {
			assert.Less(t, d.Seq, b.Seq, "an accepted deal ran before the need was added")
		} else {
			assert.Greater(t, d.Seq, b.Seq)
			assert.Equal(t, "need "+gate+" of card "+id+" is waiting, not landed", d.Reason)
		}
	}
}

// blockApplies holds the writer lock in a transaction of its own, starts
// apply, and waits until that Apply is blocked inside its transaction. It
// returns the Apply's backend pid, the blocker (roll it back to release) and
// a connection to act from (pg_stat_activity is cached for the length of a
// transaction, so it is read outside the blocker).
func blockApplies(t *testing.T, dsn string, apply func()) (int32, pgx.Tx, *pgx.Conn) {
	t.Helper()
	tx, err := connect(t, dsn).Begin(context.Background())
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), "LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE")
	require.NoError(t, err)
	watch := connect(t, dsn)
	go apply()
	var pid int32
	require.Eventually(t, func() bool {
		err := watch.QueryRow(context.Background(), "SELECT pid FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE 'LOCK TABLE sprint.epoch%'").Scan(&pid)
		return err == nil
	}, wait, 10*time.Millisecond)
	return pid, tx, watch
}

func TestFunctionalUnknownThenReplay(t *testing.T) {
	t.Parallel()
	for name, interrupt := range map[string]func(t *testing.T, c *pgx.Conn, pid int32, cancel context.CancelFunc){
		"connection killed": func(t *testing.T, c *pgx.Conn, pid int32, _ context.CancelFunc) {
			var killed bool
			require.NoError(t, c.QueryRow(context.Background(), "SELECT pg_terminate_backend($1)", pid).Scan(&killed))
			require.True(t, killed)
		},
		"context cancelled": func(_ *testing.T, _ *pgx.Conn, _ int32, cancel context.CancelFunc) { cancel() },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, p, dsn := pgHarness(t)
			h.seed()
			o := h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			pid, blocker, watch := blockApplies(t, dsn, func() { _, err := p.Apply(ctx, o); done <- err })
			interrupt(t, watch, pid, cancel)
			err := <-done
			require.ErrorIs(t, err, ErrUnknown, "an Apply cut off inside its transaction is unknown, never unchanged")
			require.NoError(t, blocker.Rollback(context.Background()))
			_, ok, err := p.Replay(h.ctx, o.ID)
			require.NoError(t, err)
			assert.False(t, ok, "it did not land")
			assert.Equal(t, 0, h.snap().Cards["c1"].Gen)
			r := h.ok(o)
			got, ok, err := p.Replay(h.ctx, o.ID)
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, r, got)
			assert.Equal(t, 1, h.snap().Cards["c1"].Gen)
		})
	}
}

// Guarantee 3, deterministically: the reply is lost after COMMIT (the
// afterCommit seam fails once). Apply says unknown; Replay finds the accepted
// receipt; a second Apply with the id returns it and logs nothing.
func TestFunctionalLostReplyAfterCommit(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	var failed atomic.Bool
	p.fault = func(stage string) error {
		if stage == "committed" && failed.CompareAndSwap(false, true) {
			return fmt.Errorf("test: reply lost after commit")
		}
		return nil
	}
	o := h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"})
	_, err := p.Apply(h.ctx, o)
	require.ErrorIs(t, err, ErrUnknown)
	assert.Contains(t, err.Error(), "reply lost after commit")
	got, ok, err := p.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	require.True(t, ok, "it landed")
	assert.True(t, got.Accepted)
	again := h.ok(o)
	assert.Equal(t, got, again, "a second Apply returns the stored receipt")
	n := 0
	for _, e := range h.events() {
		if e.Op == o.ID {
			n++
		}
	}
	assert.Equal(t, 1, n, "one event for the op")
	assert.Equal(t, 1, h.snap().Cards["c1"].Gen, "the gen moved once")
}

// A panic inside a write's transaction, with the writer lock held, rolls the
// transaction back and is re-raised: the next writer proceeds (the cold
// read's TestColdFarFutureAtPG left every later writer blocked).
func TestFunctionalPanicReleasesLock(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	var once atomic.Bool
	p.fault = func(stage string) error {
		if stage == "locked" && once.CompareAndSwap(false, true) {
			panic("test: panic with the writer lock held")
		}
		return nil
	}
	o := h.op("boom", nil, SetCardState{"c1", "ready"})
	assert.PanicsWithValue(t, "test: panic with the writer lock held", func() { _, _ = p.Apply(h.ctx, o) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := p.Apply(ctx, h.op("next", nil, SetCardState{"c2", "ready"}))
	require.NoError(t, err, "the next writer after the panic")
	assert.True(t, r.Accepted)
	_, ok, err := p.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	assert.False(t, ok, "the op that panicked did not land")
	assert.Equal(t, "waiting", h.snap().Cards["c1"].State)
}

// Eight Migrates of one fresh store at once all succeed: each holds the
// advisory lock (the cold read's TestFunctionalColdConcurrentMigrate).
func TestFunctionalConcurrentMigrate(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		p, err := Open(context.Background(), dsn, "")
		require.NoError(t, err)
		t.Cleanup(p.Close)
		wg.Go(func() { errs[i] = p.Migrate(context.Background()) })
	}
	wg.Wait()
	for i, err := range errs {
		assert.NoError(t, err, "migrate %d", i)
	}
}

// unguardLog runs sql on the log with its append-only trigger off: how a
// test corrupts an event the store would never let change.
func unguardLog(t *testing.T, dsn, sql string) {
	t.Helper()
	c := connect(t, dsn)
	_, err := c.Exec(context.Background(), "ALTER TABLE sprint.events DISABLE TRIGGER events_append_only")
	require.NoError(t, err)
	_, err = c.Exec(context.Background(), sql)
	require.NoError(t, err)
	_, err = c.Exec(context.Background(), "ALTER TABLE sprint.events ENABLE TRIGGER events_append_only")
	require.NoError(t, err)
}

// Corrupt JSON in an event's image is an error naming the seq from Events,
// never a panic (Emma's read, 2026-11-06).
func TestFunctionalEventsCorruptImage(t *testing.T) {
	t.Parallel()
	h, p, dsn := pgHarness(t)
	h.seed()
	unguardLog(t, dsn, `UPDATE sprint.events SET after = '"not a state"' WHERE seq = 1`)
	var err error
	assert.NotPanics(t, func() {
		_, err = p.Events(context.Background(), 0, 0)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after-image of seq 1")
}

// A tail on Events while eight writers race never sees a gap (the cold
// read's TestFunctionalColdEventsTailNoGap).
func TestFunctionalEventsTailNoGap(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	const writers, each = 8, 25
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				_, err := p.Apply(context.Background(), Op{ID: fmt.Sprintf("w%d-%d", w, i), Epoch: 1, Actor: "t", Kind: "s", At: t0, Writes: []Write{PutStream{Name: fmt.Sprintf("s%d-%d", w, i)}}})
				assert.NoError(t, err)
			}
		})
	}
	var cursor int64
	for deadline := time.Now().Add(wait); cursor < 1+writers*each && time.Now().Before(deadline); {
		page, err := p.Events(context.Background(), cursor, 7)
		require.NoError(t, err)
		for _, e := range page {
			require.Equal(t, cursor+1, e.Seq, "the tail skipped an event")
			cursor = e.Seq
		}
	}
	wg.Wait()
	assert.Equal(t, int64(1+writers*each), cursor)
}

// Guarantee 6: snapshots taken while 50 ops land are each one consistent
// read: the Seq is the last event in it, and folding the events up to Seq
// gives exactly its streams, cards and rows.
func TestFunctionalSnapshotConsistentUnderWrites(t *testing.T) {
	t.Parallel()
	h, p, _ := pgHarness(t)
	h.seed()
	ops := make([]Op, 50)
	for i := range ops {
		id := fmt.Sprintf("k%d", i)
		ops[i] = h.op("add", nil, PutCard{Tier: "pro", Kind: "producer", ID: id, Stream: "s1", Score: i}, SetCardReason{"c1", id})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, o := range ops {
			_, err := p.Apply(context.Background(), o)
			assert.NoError(t, err)
		}
	}()
	var snaps []Snapshot
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		s, err := p.Snapshot(context.Background())
		require.NoError(t, err)
		snaps = append(snaps, s)
	}
	all := h.events()
	require.Len(t, all, 51)
	for _, s := range snaps {
		require.LessOrEqual(t, s.Seq, int64(len(all)))
		assert.Equal(t, s, Rebuild(all[:s.Seq]), "snapshot at seq %d", s.Seq)
	}
	t.Logf("%d snapshots checked", len(snaps))
}

// The unknown rule: any error the server returned before COMMIT, whatever
// its SQLSTATE, is known. It comes back as a plain error naming the SQLSTATE
// and the message (not a refusal, not ErrUnknown, not ErrInvalid), nothing is
// logged, and Replay is not needed: the id is not in the log. Cases: a
// constraint the rules do not know about (23514), a value too large for an
// index (54000) and a column the store writes that is gone (42703).
func TestFunctionalServerErrorIsKnown(t *testing.T) {
	t.Parallel()
	big := ""
	for i := 0; len(big) < 3072; i++ { // 3 KB that pglz cannot shrink under the btree limit
		big += fmt.Sprintf("%x", sha256.Sum256([]byte(strconv.Itoa(i))))
	}
	for _, c := range []struct {
		name, ddl, sqlstate, msg string
		brief                    string
	}{
		{"constraint", "ALTER TABLE sprint.cards ADD CONSTRAINT test_no_x CHECK (brief <> 'x')", "23514", "constraint test_no_x", "x"},
		{"index row too large", "CREATE INDEX test_brief ON sprint.cards (brief)", "54000", "index row", big},
		{"dropped column", "ALTER TABLE sprint.cards DROP COLUMN priced", "42703", "priced", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, p, dsn := pgHarness(t)
			h.seed()
			_, err := connect(t, dsn).Exec(context.Background(), c.ddl)
			require.NoError(t, err)
			before := h.events() // a snapshot cannot read the store once a column is gone
			o := h.op("add", nil, PutStream{Name: "s2"}, PutCard{Tier: "pro", Kind: "producer", ID: "c9", Stream: "s1", Brief: c.brief})
			_, err = p.Apply(h.ctx, o)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "SQLSTATE "+c.sqlstate)
			assert.Contains(t, err.Error(), c.msg)
			assert.Contains(t, err.Error(), "before COMMIT, so nothing landed")
			assert.NotErrorIs(t, err, ErrUnknown)
			assert.NotErrorIs(t, err, ErrInvalid)
			_, ok, err := p.Replay(h.ctx, o.ID)
			require.NoError(t, err)
			assert.False(t, ok, "nothing was logged")
			assert.Equal(t, before, h.events())
		})
	}
}

func TestFunctionalRebuildFromLog(t *testing.T) {
	t.Parallel()
	h, _, _ := pgHarness(t)
	h.seed()
	for i := range 20 {
		id := fmt.Sprintf("k%d", i)
		h.ok(h.op("add", []Expect{CardAbsent{id}}, PutCard{Tier: "pro", Kind: "producer", ID: id, Stream: "s1", Score: i, Needs: []string{"c1"}}))
		h.ok(h.op("ready", []Expect{CardGen{id, 0}}, SetCardState{id, "ready"}))
		h.refused(h.op("ready", []Expect{CardGen{id, 0}}, SetCardState{id, "ready"}), "card "+id+" is at gen 1, expected gen 0")
		if i == 10 {
			h.clear()
			h.ok(h.op("seed", nil, PutStream{Name: "s1"}, PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1"}, SetRowState{"w1", "up"}))
		}
	}
	h.ok(h.op("land", nil, SetCardState{"k15", "landed"}, SetCardHead{"k15", "deadbeef"}, SetCardPR{"k15", 7}, SetRowFlaps{"w1", 2}))
	snap := h.snap()
	assert.Equal(t, snap, Rebuild(h.events()))
	assert.Len(t, snap.Cards, 10)
	assert.Equal(t, 2, snap.Epoch)
	assert.Equal(t, "up", snap.Rows["w1"].State)
}

func TestFunctionalRefusalRow(t *testing.T) {
	t.Parallel()
	h, _, dsn := pgHarness(t)
	h.seed()
	o := h.op("take", []Expect{CardState{"c1", "working"}}, SetCardState{"c1", "review"})
	h.refused(o, "card c1 is waiting, expected working")
	var accepted, noBefore, noAfter bool
	var reason, inReceipt string
	var epoch int
	require.NoError(t, connect(t, dsn).QueryRow(context.Background(), "SELECT accepted, reason, before IS NULL, after IS NULL, receipt->>'reason', epoch FROM sprint.events WHERE op = $1", o.ID).
		Scan(&accepted, &reason, &noBefore, &noAfter, &inReceipt, &epoch))
	assert.False(t, accepted)
	assert.Equal(t, "card c1 is waiting, expected working", reason)
	assert.Equal(t, reason, inReceipt)
	assert.True(t, noBefore)
	assert.True(t, noAfter)
	assert.Equal(t, 1, epoch)
}

// Guarantee 4 is the schema's, not Go's: SQL that skips the store is refused.
func TestFunctionalSchemaEnforces(t *testing.T) {
	t.Parallel()
	h, _, dsn := pgHarness(t)
	h.seed()
	c := connect(t, dsn)
	for q, constraint := range map[string]string{
		"UPDATE sprint.cards SET state = 'flying' WHERE id = 'c1'":                                                                                   "cards_state_check",
		"UPDATE sprint.cards SET state = NULL WHERE id = 'c1'":                                                                                       `"state"`,
		"UPDATE sprint.cards SET branch = NULL WHERE id = 'c1'":                                                                                      `"branch"`,
		"UPDATE sprint.cards SET author = NULL WHERE id = 'c1'":                                                                                      `"author"`,
		"UPDATE sprint.cards SET base_sha = NULL WHERE id = 'c1'":                                                                                    `"base_sha"`,
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, holder, epoch) VALUES ('k3', 'work', 'pro', 'c1', 's1', 'ready', 'w9', 1)`:   "cards_epoch_holder_fkey",
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, holder, epoch) VALUES ('c3', 'producer', 'pro', 's1', 'ready', 'w1', 1)":           "cards_holder_check",
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, epoch) VALUES ('k3', 'work', 'pro', 'c1', 's1', 'ready', 1)`:                 "cards_holder_check",
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, epoch) VALUES ('c3', 'gate', 'pro', 's1', 'ready', 1)":                             "cards_kind_check",
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, epoch) VALUES ('c3', 'producer', 'ultra', 's1', 'ready', 1)":                       "cards_tier_check",
		"INSERT INTO sprint.cards (id, kind, stream, state, epoch) VALUES ('c3', 'producer', 's1', 'ready', 1)":                                      `"tier"`,
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, holder, epoch) VALUES ('k3', 'work', 'heavy', 'c1', 's1', 'ready', 'w1', 1)`: "cards_consumer_tier_check",
		"UPDATE sprint.cards SET paths = NULL WHERE id = 'c1'":                                                                                       `"paths"`,
		"UPDATE sprint.cards SET done_when = NULL WHERE id = 'c1'":                                                                                   `"done_when"`,
		"UPDATE sprint.cards SET prose = NULL WHERE id = 'c1'":                                                                                       `"prose"`,
		"UPDATE sprint.cards SET result = 'maybe' WHERE id = 'u1'":                                                                                   "cards_result_check",
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, holder, epoch) VALUES ('k3', 'work', 'pro', 's1', 'ready', 'w1', 1)":               "cards_of_check",
		// the cards_fixed trigger: kind, of, stream, holder and tier never
		// change (Johnny's review: no holder write), and of names a producer
		// of the same tier
		"UPDATE sprint.cards SET holder = NULL WHERE id = 'u1'":  "cards_fixed_check",
		"UPDATE sprint.cards SET holder = 'w9' WHERE id = 'u1'":  "cards_fixed_check",
		"UPDATE sprint.cards SET holder = 'w1' WHERE id = 'c1'":  "cards_fixed_check",
		"UPDATE sprint.cards SET tier = 'heavy' WHERE id = 'c1'": "cards_fixed_check",
		// rows, judgements, cursors and the events' policy
		"UPDATE sprint.rows SET tiers = '{flash,ultra}'": "rows_tiers_check",
		"UPDATE sprint.rows SET tiers = NULL":            `"tiers"`,
		"UPDATE sprint.rows SET model = NULL":            `"model"`,
		"UPDATE sprint.rows SET harness = NULL":          `"harness"`,
		"INSERT INTO sprint.judgements (id, kind, state, opened_at, closed_at, epoch) VALUES ('j1', 'red', 'pending', now(), now(), 1)":                 "judgements_state_check",
		"INSERT INTO sprint.judgements (id, kind, state, opened_at, epoch) VALUES ('j1', 'red', 'answered', now(), 1)":                                  "judgements_closed_check",
		"INSERT INTO sprint.judgements (id, kind, state, opened_at, closed_at, epoch) VALUES ('j1', 'red', 'open', now(), now(), 1)":                    "judgements_closed_check",
		"INSERT INTO sprint.judgements (id, kind, state, epoch) VALUES ('j1', 'red', 'open', 1)":                                                        `"opened_at"`,
		"INSERT INTO sprint.cursors (name, seq) VALUES ('push', -1)":                                                                                    "cursors_seq_check",
		"INSERT INTO sprint.cursors (name, seq) VALUES ('push', NULL)":                                                                                  `"seq"`,
		"INSERT INTO sprint.events (seq, op, actor, kind, accepted, receipt, at, epoch, policy) VALUES (99, 'x', 'a', 'k', true, '{}', now(), 1, NULL)": `"policy"`,
		// round 10 (the third cold read's TestFunctionalColdRead3DirectSQL):
		// keys and epochs fixed, a consumer in its producer's stream, every
		// count at 0 or more, a judgement closed after it opened, the log
		// append-only
		"UPDATE sprint.cards SET epoch = 2 WHERE id = 'u1'":      "cards_fixed_check",
		"UPDATE sprint.cards SET id = 'renamed' WHERE id = 'u2'": "cards_fixed_check",
		"UPDATE sprint.rows SET name = 'renamed'":                "rows_key_fixed_check",
		"UPDATE sprint.rows SET epoch = 2":                       "rows_key_fixed_check",
		"UPDATE sprint.streams SET name = 'renamed'":             "streams_key_fixed_check",
		"UPDATE sprint.streams SET epoch = 2":                    "streams_key_fixed_check",
		"UPDATE sprint.rows SET width = -1":                      "rows_width_check",
		"UPDATE sprint.rows SET flaps = -1":                      "rows_flaps_check",
		"UPDATE sprint.rows SET gen = -1":                        "rows_gen_check",
		"UPDATE sprint.streams SET pos = -1":                     "streams_pos_check",
		"UPDATE sprint.cards SET gen = -1 WHERE id = 'c1'":       "cards_gen_check",
		"UPDATE sprint.cards SET attempt = -1 WHERE id = 'c1'":   "cards_attempt_check",
		"UPDATE sprint.cards SET score = -5 WHERE id = 'c1'":     "cards_score_check",
		"UPDATE sprint.cards SET pr = -2 WHERE id = 'c1'":        "cards_pr_check",
		"UPDATE sprint.cards SET bounces = -1 WHERE id = 'c1'":   "cards_bounces_min_check",
		"UPDATE sprint.cards SET review = -1 WHERE id = 'c1'":    "cards_review_min_check",
		"UPDATE sprint.cards SET review = 1 WHERE id = 'u1'":     "cards_review_check",
		"UPDATE sprint.cards SET review = 1 WHERE id = 'u2'":     "cards_fixed_check",
		"UPDATE sprint.cards SET review = NULL WHERE id = 'c1'":  `"review"`,
		"UPDATE sprint.cards SET finding = 'x' WHERE id = 'u2'":  "cards_finding_check",
		"UPDATE sprint.cards SET finding = 'x' WHERE id = 'u1'":  "cards_fixed_check",
		"UPDATE sprint.cards SET finding = NULL WHERE id = 'c1'": `"finding"`,
		"UPDATE sprint.rows SET reason = NULL":                   `"reason"`,
		"UPDATE sprint.cards SET cost = -1 WHERE id = 'u1'":      "cards_cost_check",
		"UPDATE sprint.events SET accepted = NOT accepted":       "events_append_only_check",
		"DELETE FROM sprint.events":                              "events_append_only_check",
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, holder, epoch) VALUES ('k3', 'work', 'pro', 'c1', 's9', 'ready', 'w1', 1)`:           "cards_consumer_stream_check",
		"INSERT INTO sprint.judgements (id, kind, state, opened_at, closed_at, epoch) VALUES ('j1', 'red', 'answered', now(), now() - interval '1 hour', 1)": "judgements_order_check",
		// the cards_fixed trigger: kind, of and stream never change, and of names a producer
		`UPDATE sprint.cards SET "of" = NULL WHERE id = 'u1'`:                                                                                                        "cards_fixed_check",
		`UPDATE sprint.cards SET "of" = 'c0' WHERE id = 'u1'`:                                                                                                        "cards_fixed_check",
		`UPDATE sprint.cards SET "of" = 'c9' WHERE id = 'u1'`:                                                                                                        "cards_fixed_check",
		"UPDATE sprint.cards SET kind = 'sentinel' WHERE id = 'c1'":                                                                                                  "cards_fixed_check",
		"UPDATE sprint.cards SET kind = 'read' WHERE id = 'u1'":                                                                                                      "cards_fixed_check",
		"UPDATE sprint.cards SET stream = 's9' WHERE id = 'c1'":                                                                                                      "cards_fixed_check",
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, epoch) VALUES ('k3', 'read', 'pro', 'u1', 's1', 'ready', 1)`:                                 "cards_of_producer_check",
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, epoch) VALUES ('k3', 'read', 'pro', 'c9', 's1', 'ready', 1)`:                                 "cards_of_producer_check",
		"UPDATE sprint.cards SET needs = '{c1}' WHERE id = 'u1'":                                                                                                     "cards_needs_check",
		"UPDATE sprint.cards SET priority = 'read' WHERE id = 'c1'":                                                                                                  "cards_priority_check",
		"UPDATE sprint.cards SET priority = 'high' WHERE id = 'u1'":                                                                                                  "cards_consumer_priority_check",
		"UPDATE sprint.cards SET prior_head = 'x' WHERE id = 'u2'":                                                                                                   "cards_prior_head_check",
		"UPDATE sprint.cards SET prior_head = 'x' WHERE id = 'c1'":                                                                                                   "cards_prior_head_check",
		"UPDATE sprint.cards SET bounces = 1 WHERE id = 'u1'":                                                                                                        "cards_bounces_check",
		"UPDATE sprint.cards SET cost = 1 WHERE id = 'c1'":                                                                                                           "cards_producer_cost_check",
		"UPDATE sprint.cards SET priced = true WHERE id = 'c1'":                                                                                                      "cards_producer_cost_check",
		"UPDATE sprint.cards SET result = 'ok' WHERE id = 'c1'":                                                                                                      "cards_producer_result_check",
		"UPDATE sprint.cards SET priority = NULL WHERE id = 'c1'":                                                                                                    `"priority"`,
		"UPDATE sprint.cards SET prior_head = NULL WHERE id = 'u1'":                                                                                                  `"prior_head"`,
		"UPDATE sprint.cards SET bounces = NULL WHERE id = 'c1'":                                                                                                     `"bounces"`,
		"UPDATE sprint.cards SET cost = NULL WHERE id = 'u1'":                                                                                                        `"cost"`,
		"UPDATE sprint.cards SET priced = NULL WHERE id = 'u1'":                                                                                                      `"priced"`,
		"UPDATE sprint.cards SET after = '{c1}' WHERE id = 'u1'":                                                                                                     "cards_after_check",
		"UPDATE sprint.cards SET after = NULL WHERE id = 'c1'":                                                                                                       `"after"`,
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, brief, epoch) VALUES ('g3', 'sentinel', 'pro', 's1', 'ready', 'x', 1)":                             "cards_brief_check",
		`INSERT INTO sprint.cards (id, kind, tier, "of", stream, state, epoch) VALUES ('g3', 'sentinel', 'pro', 'c1', 's1', 'ready', 1)`:                             "cards_of_check",
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, holder, epoch) VALUES ('g3', 'sentinel', 'pro', 's1', 'ready', 'w1', 1)":                           "cards_holder_check",
		"UPDATE sprint.rows SET held = NULL":                                                                                                                         `"held"`,
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, epoch) VALUES ('c3', 'producer', 'pro', 's9', 'ready', 1)":                                         "cards_epoch_stream_fkey",
		"INSERT INTO sprint.cards (id, kind, tier, stream, state, epoch) VALUES ('c3', 'producer', 'pro', 's1', 'ready', 2)":                                         "cards_epoch_stream_fkey",
		"INSERT INTO sprint.cards (id, stream, state, epoch) VALUES ('c3', 's1', 'ready', 1)":                                                                        `"kind"`,
		"INSERT INTO sprint.rows (name, kind, state, epoch) VALUES ('w2', 'robot', 'up', 1)":                                                                         "rows_kind_check",
		"INSERT INTO sprint.rows (name, kind, state, epoch) VALUES ('w2', 'kin', 'asleep', 1)":                                                                       "rows_state_check",
		"UPDATE sprint.rows SET state = 'blocked'":                                                                                                                   "rows_state_check",
		"UPDATE sprint.rows SET session = NULL":                                                                                                                      `"session"`,
		"INSERT INTO sprint.epoch (id, n) VALUES (2, 5)":                                                                                                             "epoch_id_check",
		"INSERT INTO sprint.meta (id, schema) VALUES (2, 1)":                                                                                                         "meta_id_check",
		"INSERT INTO sprint.events (seq, op, actor, kind, accepted, receipt, at, epoch) SELECT 99, op, actor, kind, accepted, receipt, at, epoch FROM sprint.events": "events_op_key",
		"INSERT INTO sprint.policy (kind, name, body, active, actor, at) VALUES ('data', 'a', '', true, 'x', now()), ('data', 'b', '', true, 'x', now())":            "policy_one_active",
	} {
		_, err := c.Exec(context.Background(), q)
		require.Error(t, err, q)
		var pe *pgconn.PgError
		require.ErrorAs(t, err, &pe, q)
		assert.Contains(t, err.Error()+" "+pe.ConstraintName, constraint, q)
	}
}

// The unknown rule, final form (round 10): inside the transaction, every
// error but a lost connection or a done ctx is known, a plain error, and
// nothing is logged. A scan error (a column whose type changed under the
// store) and a lock timeout are from the third cold read's
// TestFunctionalColdRead3ScanErrorIsKnown and ...LockTimeoutIsKnown.
func TestFunctionalClientErrorIsKnown(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		setup string // as the store's owner, behind its back
		w     Write
		want  string
	}{
		"scan error":    {"ALTER TABLE sprint.rows ALTER COLUMN held DROP DEFAULT, ALTER COLUMN held TYPE integer USING held::int", SetRowFlaps{"w1", 1}, "failed before COMMIT, so nothing landed: can't scan"},
		"card gen full": {"UPDATE sprint.cards SET gen = 2147483647 WHERE id = 'c1'", SetCardReason{"c1", "r"}, "store: card c1 gen is 2147483647, the most its column holds; it cannot rise"},
		"row gen full":  {"UPDATE sprint.rows SET gen = 2147483647 WHERE name = 'w1'", SetRowFlaps{"w1", 1}, "store: row w1 gen is 2147483647, the most its column holds; it cannot rise"},
		"policy full":   {"INSERT INTO sprint.policy (version, kind, name, body, actor, at) VALUES (2147483647, 'data', 'p', '', 'x', now())", PutPolicy{"data", "q", ""}, "store: policy version is 2147483647, the most its column holds; it cannot rise"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, _, dsn := pgHarness(t)
			h.seed()
			_, err := connect(t, dsn).Exec(context.Background(), c.setup)
			require.NoError(t, err)
			p, err := Open(context.Background(), dsn, "") // a fresh pool: no statement cached before the setup
			require.NoError(t, err)
			t.Cleanup(p.Close)
			o := h.op("x", nil, c.w)
			for range 2 { // the same each time: nothing landed, nothing to replay
				_, err = p.Apply(h.ctx, o)
				require.Error(t, err)
				assert.NotErrorIs(t, err, ErrUnknown)
				assert.NotErrorIs(t, err, ErrInvalid)
				assert.Contains(t, err.Error(), c.want)
				_, found, rerr := p.Replay(h.ctx, o.ID)
				require.NoError(t, rerr)
				assert.False(t, found, "nothing logged")
			}
		})
	}
}

// A receipt that does not decode is a plain error from Apply on its id, not
// ErrUnknown (a caller would replay forever).
func TestFunctionalCorruptReceiptIsKnown(t *testing.T) {
	t.Parallel()
	h, p, dsn := pgHarness(t)
	h.seed()
	o := h.op("x", nil, SetCardReason{"c1", "r"})
	h.ok(o)
	unguardLog(t, dsn, fmt.Sprintf(`UPDATE sprint.events SET receipt = '{"seq": "x"}' WHERE op = '%s'`, o.ID))
	_, err := p.Apply(h.ctx, o)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUnknown)
	assert.Contains(t, err.Error(), "failed before COMMIT, so nothing landed")
}

// A lock timeout is the server answering: known, a plain error, nothing logged.
func TestFunctionalLockTimeoutIsKnown(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	_, err := connect(t, dsn).Exec(context.Background(), "ALTER DATABASE postgres SET lock_timeout = '200ms'")
	require.NoError(t, err)
	p := openPG(t, dsn)
	h := &harness{t: t, s: p, ctx: context.Background(), epoch: 1}
	h.seed()
	tx, err := connect(t, dsn).Begin(context.Background())
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), "LOCK TABLE sprint.epoch IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	o := h.op("x", nil, SetCardReason{"c1", "r"})
	_, err = p.Apply(h.ctx, o)
	require.NoError(t, tx.Rollback(context.Background()))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUnknown)
	assert.Contains(t, err.Error(), "SQLSTATE 55P03")
	_, found, err := p.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	assert.False(t, found)
}
