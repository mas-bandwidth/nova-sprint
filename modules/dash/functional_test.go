package dash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/pgbin"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// pgEnv is the environment of a PostgreSQL program: this process's, with
// LC_ALL=C (PostgreSQL 18 on macOS refuses to start under a shell with no
// valid locale: "postmaster became multithreaded during startup").
func pgEnv() []string { return append(os.Environ(), "LC_ALL=C") }

func startPG(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(pgbin.Dir(t), "initdb")
	dir := t.TempDir()
	cmd := exec.Command(bin, "-D", dir, "-U", "nova", "--auth=trust", "--no-locale")
	cmd.Env = pgEnv()
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("initdb %v: %s", e, out)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	// pg_ctl starts it and stops it (-m immediate), never a signal.
	ctl := filepath.Join(filepath.Dir(bin), "pg_ctl")
	start := exec.Command(ctl, "-D", dir, "-l", filepath.Join(dir, "postgres.log"), "-o", fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", port), "-w", "start")
	start.Env = pgEnv()
	if out, e := start.CombinedOutput(); e != nil {
		t.Fatalf("pg_ctl start: %v\n%s", e, out)
	}
	t.Cleanup(func() {
		stop := exec.Command(ctl, "-D", dir, "-m", "immediate", "-w", "stop")
		stop.Env = pgEnv()
		if out, e := stop.CombinedOutput(); e != nil {
			t.Logf("pg_ctl stop: %v\n%s", e, out)
		}
	})
	return fmt.Sprintf("postgres://nova@127.0.0.1:%d/postgres?sslmode=disable", port)
}
func TestFunctionalJSONGolden(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	ctx := context.Background()
	var s *store.PG
	var err error
	for i := 0; i < 100; i++ {
		s, err = store.Open(ctx, dsn, "")
		if err == nil {
			err = s.Migrate(ctx)
			if err == nil {
				break
			}
			s.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := apply(t, s, "seed", 1, store.PutStream{Name: "alpha"}, store.PutRow{Name: "w"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "alpha"})
	if !r.Accepted {
		t.Fatal(r.Reason)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFromFeed(Feed{Snap: snap, Roster: testRoster, At: at, Landings: []time.Time{}}, func() time.Time { return at })
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var body any
	if err = json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	body.(map[string]any)["dataAgeSeconds"] = 0
	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err = os.WriteFile("testdata/seed.json", got, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/seed.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
