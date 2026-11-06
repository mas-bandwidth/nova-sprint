package store

// Functional tests: a real throwaway PostgreSQL per test (initdb into
// t.TempDir(), a free port, fsync off). Skipped when initdb is not found.
// Run them alone with: go test -run Functional ./store/

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	out, err := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8").CombinedOutput()
	require.NoError(t, err, "%s", out)
	port := freePort(t)
	cmd := exec.Command(filepath.Join(bin, "postgres"), "-D", filepath.Join(dir, "data"), "-p", port, "-c", "listen_addresses=127.0.0.1", "-c", "unix_socket_directories=", "-c", "fsync=off")
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

func TestFunctionalMigrateTwice(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	p := openPG(t, dsn)
	require.NoError(t, p.Migrate(context.Background()), "a second Migrate is a no-op")
	c := connect(t, dsn)
	var rows, tables int
	require.NoError(t, c.QueryRow(context.Background(), "SELECT count(*) FROM sprint.epoch").Scan(&rows))
	assert.Equal(t, 1, rows)
	require.NoError(t, c.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'sprint'").Scan(&tables))
	assert.Equal(t, 6, tables)
	s, err := p.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, emptySnapshot(0), s)
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

func TestFunctionalConcurrentApplies(t *testing.T) {
	t.Parallel()
	p := openPG(t, startPG(t))
	h := &harness{t: t, s: p, ctx: context.Background()}
	h.seed()
	for i := range 10 {
		id := fmt.Sprintf("k%d", i)
		h.ok(h.op("add", nil, PutCard{Card{ID: id, Stream: "s1", State: "ready"}}))
		ops := []Op{
			h.op("take", []Expect{CardIs{id, "ready", 0}, CardHolder{id, ""}}, SetCardState{id, "working"}, SetCardHolder{id, "w1"}),
			h.op("take", []Expect{CardIs{id, "ready", 0}, CardHolder{id, ""}}, SetCardState{id, "working"}, SetCardHolder{id, "w1"}),
		}
		got := make([]Receipt, 2)
		var wg sync.WaitGroup
		for j := range ops {
			wg.Go(func() {
				r, err := p.Apply(h.ctx, ops[j])
				assert.NoError(t, err)
				got[j] = r
			})
		}
		wg.Wait()
		require.NotEqual(t, got[0].Accepted, got[1].Accepted, "exactly one of two takes of %s is accepted", id)
		for _, r := range got {
			if !r.Accepted {
				assert.Equal(t, "card "+id+" is at gen 1, expected gen 0", r.Reason)
			}
		}
		assert.Equal(t, 1, h.snap().Cards[id].Gen)
	}
}

// blockApplies holds the epoch lock in a transaction of its own, starts apply,
// and waits until that Apply is blocked inside its transaction. It returns the
// Apply's backend pid, the blocker (roll it back to release) and a connection
// to act from (pg_stat_activity is read outside the blocker: it is cached for
// the length of a transaction).
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
			dsn := startPG(t)
			p := openPG(t, dsn)
			h := &harness{t: t, s: p, ctx: context.Background()}
			h.seed()
			o := h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"})
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

func TestFunctionalRebuildFromLog(t *testing.T) {
	t.Parallel()
	p := openPG(t, startPG(t))
	h := &harness{t: t, s: p, ctx: context.Background()}
	h.seed()
	for i := range 20 {
		id := fmt.Sprintf("k%d", i)
		h.ok(h.op("add", []Expect{CardAbsent{id}}, PutCard{Card{ID: id, Stream: "s1", State: "waiting", Score: i, Needs: []string{"c1"}}}))
		h.ok(h.op("ready", []Expect{CardIs{id, "waiting", 0}}, SetCardState{id, "ready"}))
		h.refused(h.op("ready", []Expect{CardIs{id, "waiting", 0}}, SetCardState{id, "ready"}), "card "+id+" is at gen 1, expected gen 0")
		if i == 10 {
			_, err := p.Clear(h.ctx, h.op("clear", nil))
			require.NoError(t, err)
			h.seed()
		}
	}
	h.ok(h.op("land", nil, SetCardState{"k15", "landed"}, SetCardHeadPR{"k15", "deadbeef", 7}, SetRowState{"w1", "down", t0, 2}))
	events := h.events()
	snap := h.snap()
	assert.Equal(t, snap, Rebuild(events))
	var current []Event
	for _, e := range events {
		if e.Epoch == snap.Epoch {
			current = append(current, e)
		}
	}
	assert.Equal(t, snap, Rebuild(current), "the current epoch's events alone rebuild it")
	assert.Len(t, snap.Cards, 10)
}

func TestFunctionalRefusalRow(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	p := openPG(t, dsn)
	h := &harness{t: t, s: p, ctx: context.Background()}
	h.seed()
	o := h.op("take", []Expect{CardIs{"c1", "working", 0}}, SetCardState{"c1", "review"})
	h.refused(o, "card c1 is ready, expected working")
	c := connect(t, dsn)
	var accepted, noBefore, noAfter bool
	var reason, inReceipt string
	require.NoError(t, c.QueryRow(context.Background(), "SELECT accepted, reason, before IS NULL, after IS NULL, receipt->>'reason' FROM sprint.events WHERE op = $1", o.ID).
		Scan(&accepted, &reason, &noBefore, &noAfter, &inReceipt))
	assert.False(t, accepted)
	assert.Equal(t, "card c1 is ready, expected working", reason)
	assert.Equal(t, reason, inReceipt)
	assert.True(t, noBefore)
	assert.True(t, noAfter)
}

// Guarantee 4 is the schema's, not Go's: SQL that skips the store is refused.
func TestFunctionalSchemaEnforces(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	p := openPG(t, dsn)
	h := &harness{t: t, s: p, ctx: context.Background()}
	h.seed()
	c := connect(t, dsn)
	for q, constraint := range map[string]string{
		"UPDATE sprint.cards SET state = 'flying' WHERE id = 'c1'":                                                                                          "cards_state_check",
		"UPDATE sprint.cards SET holder = 'w9' WHERE id = 'c1'":                                                                                             "cards_epoch_holder_fkey",
		"INSERT INTO sprint.cards (id, stream, state, epoch) VALUES ('c2', 's9', 'ready', 0)":                                                               "cards_epoch_stream_fkey",
		"INSERT INTO sprint.cards (id, stream, state, epoch) VALUES ('c2', 's1', 'ready', 1)":                                                               "cards_epoch_stream_fkey",
		"INSERT INTO sprint.rows (name, kind, state, epoch) VALUES ('w2', 'robot', 'up', 0)":                                                                "rows_kind_check",
		"INSERT INTO sprint.rows (name, kind, state, epoch) VALUES ('w2', 'kin', 'asleep', 0)":                                                              "rows_state_check",
		"INSERT INTO sprint.epoch (n) VALUES (5)":                                                                                                           "epoch_one_row",
		"INSERT INTO sprint.events (op, actor, kind, accepted, receipt, at, epoch) SELECT op, actor, kind, accepted, receipt, at, epoch FROM sprint.events": "events_op_key",
	} {
		_, err := c.Exec(context.Background(), q)
		require.Error(t, err, q)
		assert.Contains(t, err.Error(), constraint, q)
	}
}
