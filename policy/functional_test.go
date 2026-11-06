package policy

// Functional tests: the verb suite on a throwaway PostgreSQL of each test's
// own (initdb into t.TempDir(), a free port, fsync off; skipped when initdb
// is not found, as store/functional_test.go), and the load measurement,
// which reads real time. Run them alone with: go test -run Functional ./policy/

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalVerbsPG(t *testing.T) {
	t.Parallel()
	verbSuite(t, func(t testing.TB) store.Store { return startPG(t) })
}

// startPG starts a PostgreSQL of this test's own and returns the store on it, migrated.
func startPG(t testing.TB) *store.PG {
	t.Helper()
	bin, dir := pgBin(t), t.TempDir()
	init := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8", "--no-locale")
	init.Env = append(os.Environ(), "LC_ALL=C")
	out, err := init.CombinedOutput()
	require.NoError(t, err, "%s", out)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	cmd := exec.Command(filepath.Join(bin, "postgres"), "-D", filepath.Join(dir, "data"), "-p", port, "-c", "listen_addresses=127.0.0.1", "-c", "unix_socket_directories=", "-c", "fsync=off")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Signal(syscall.SIGQUIT); _ = cmd.Wait() }) // ignored: the server is ours and disposable
	dsn := "postgres://nova@127.0.0.1:" + port + "/postgres?sslmode=disable"
	deadline := time.Now().Add(30 * time.Second)
	for {
		c, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			_ = c.Close(context.Background()) // ignored: only probing
			break
		}
		require.True(t, time.Now().Before(deadline), "postgres did not start: %v", err)
		time.Sleep(50 * time.Millisecond)
	}
	p, err := store.Open(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	require.NoError(t, p.Migrate(context.Background()))
	return p
}

// pgBin finds initdb: on PATH, else Homebrew's postgresql@18, else Debian's.
func pgBin(t testing.TB) string {
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

// TestFunctionalLoad: policy's share of a tick at 5,000 ready producers
// over 50 rows, with a cold cache and a warm one. The 100 ms budget is a
// guess; measured 2026-11-06 on amd-3 without -race: cold 132 ms, warm 68 ms. This logs the measurement and fails only far past it (under -race it runs several times slower).
func TestFunctionalLoad(t *testing.T) {
	t.Parallel()
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{}}
	for i := range 10 {
		snap.Streams[fmt.Sprintf("s%d", i)] = store.Stream{Name: fmt.Sprintf("s%d", i)}
	}
	for i := range 50 {
		name := fmt.Sprintf("r%02d", i)
		snap.Rows[name] = store.Row{Name: name, Kind: "worker", Width: 2, State: "up", Tiers: store.Tiers[i%4:]}
	}
	for i := range 5000 {
		id := fmt.Sprintf("p%04d", i)
		snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%d", i%10), State: "ready", Tier: store.Tiers[i%4], Score: i, Priority: "normal"}
	}
	q := FullQuestions(snap, nil)
	e := NewEngine()
	vs := Versions{Active: pv(1, defaultBody)}
	start := time.Now()
	a, fs := e.Answer(vs, snap, q)
	cold := time.Since(start)
	require.Empty(t, fs)
	require.Len(t, a.Rows, 5000)
	start = time.Now()
	_, fs = e.Answer(vs, snap, q)
	warm := time.Since(start)
	require.Empty(t, fs)
	t.Logf("policy at 5000 producers, 50 rows: cold %s, warm %s (budget 100ms, a guess)", cold, warm)
	require.Less(t, warm, 2*time.Second)
}
