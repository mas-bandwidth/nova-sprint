package dash

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// pgInstance is a throwaway PostgreSQL that the test can stop and start on one port.
type pgInstance struct {
	bin, dir string
	port     int
	running  bool
}

// start and stop run pg_ctl (-m immediate on stop), never a signal.
func (g *pgInstance) start(t *testing.T) {
	t.Helper()
	opts := fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", g.port)
	if out, err := exec.Command(filepath.Join(filepath.Dir(g.bin), "pg_ctl"), "-D", g.dir, "-l", filepath.Join(g.dir, "postgres.log"), "-o", opts, "-w", "start").CombinedOutput(); err != nil {
		t.Fatalf("pg_ctl start: %v\n%s", err, out)
	}
	g.running = true
}

func (g *pgInstance) stop(t *testing.T) {
	if !g.running {
		return
	}
	if out, err := exec.Command(filepath.Join(filepath.Dir(g.bin), "pg_ctl"), "-D", g.dir, "-m", "immediate", "-w", "stop").CombinedOutput(); err != nil {
		t.Logf("pg_ctl stop: %v\n%s", err, out)
	}
	g.running = false
}

func (g *pgInstance) dsn() string {
	return fmt.Sprintf("postgres://nova@127.0.0.1:%d/postgres?sslmode=disable", g.port)
}

func startPGInstance(t *testing.T) *pgInstance {
	t.Helper()
	bin, err := exec.LookPath("initdb")
	if err != nil {
		t.Skip("initdb required")
	}
	g := &pgInstance{bin: bin, dir: t.TempDir()}
	if out, err := exec.Command(bin, "-D", g.dir, "-U", "nova", "--auth=trust", "--no-locale").CombinedOutput(); err != nil {
		t.Fatalf("initdb %v: %s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g.port = l.Addr().(*net.TCPAddr).Port
	l.Close()
	g.start(t)
	t.Cleanup(func() { g.stop(t) })
	return g
}

// applyWithRetry is the test's own write, retried through Replay: the pool may hand the test a
// connection the server already terminated (the test's writer, not the dashboard).
func applyWithRetry(t *testing.T, s store.Store, id string, w ...store.Write) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		r, err := s.Apply(ctx, store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "test", At: time.Now(), Writes: w})
		if err == nil && r.Accepted {
			return
		}
		if r, found, e := s.Replay(ctx, id); e == nil && found && r.Accepted {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: write failed", id)
}

// TestFunctionalPGRestartRecovers checks that terminated backends and a full PostgreSQL restart are
// recovered by later ticks without a second Snapshot, and that the copy equals a fresh Snapshot.
func TestFunctionalPGRestartRecovers(t *testing.T) {
	if os.Getenv("NOVA_SKIP_PG") != "" {
		t.Skip()
	}
	g := startPGInstance(t)
	ctx := context.Background()
	var pg *store.PG
	var err error
	for i := 0; i < 100; i++ {
		if pg, err = store.Open(ctx, g.dsn(), ""); err == nil {
			if err = pg.Migrate(ctx); err == nil {
				break
			}
			pg.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	seedSprint(t, pg, 200)
	s := &tapStore{Store: pg}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	// 1: every other backend terminated
	admin, err := pgx.Connect(ctx, g.dsn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND backend_type = 'client backend'"); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	applyWithRetry(t, pg, "k1", store.SetCardState{Card: "p00001", State: "ready"})
	recovered, tries := false, 0
	for ; tries < 5 && !recovered; tries++ {
		recovered = c.Advance(ctx, s) == nil
	}
	t.Logf("ticks to recover after terminated backends: %d", tries)
	if !recovered {
		t.Fatal("not recovered after terminated backends")
	}
	// 2: PostgreSQL stopped, ticks fail, then started on the same port
	g.stop(t)
	for i := 0; i < 3; i++ {
		if err = c.Advance(ctx, s); err == nil {
			t.Fatal("advance succeeded with the store down")
		}
	}
	if d := c.pageSnapshot().Data; d.OK {
		t.Fatal("ok while the store is down")
	}
	g.start(t)
	deadline, restarted := time.Now().Add(20*time.Second), time.Now()
	for {
		if err = c.Advance(ctx, s); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not recovered after restart: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("recovered %s after the restart", time.Since(restarted))
	applyWithRetry(t, pg, "k2", store.SetCardState{Card: "p00002", State: "ready"})
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	fresh, err := pg.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.state, fresh) || s.snapshots != 1 || !c.pageSnapshot().Data.OK {
		t.Fatalf("after restart: equal=%v snapshots=%d", reflect.DeepEqual(c.state, fresh), s.snapshots)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// TestFunctionalPoolSurvivesOpenContextCancel checks that the pool opened under a context that is then
// cancelled stays usable: Snapshot, Events and the cursor write all succeed afterwards.
func TestFunctionalPoolSurvivesOpenContextCancel(t *testing.T) {
	dsn := startPG(t)
	var s *store.PG
	var err error
	for i := 0; i < 100; i++ {
		openCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		s, err = store.Open(openCtx, dsn, "")
		cancel()
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(ctx, store.Op{ID: "pool-seed", Epoch: 1, Actor: "test", Kind: "seed", At: time.Now(), Writes: []store.Write{store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"}}})
	if err != nil || !r.Accepted {
		t.Fatalf("seed: %v %s", err, r.Reason)
	}
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	d := c.pageSnapshot().Data
	if !d.OK || d.All != 1 || c.state.Cursors[CursorName] == 0 {
		t.Fatalf("after open-cancel: ok=%v all=%d cursor=%d", d.OK, d.All, c.state.Cursors[CursorName])
	}
}
