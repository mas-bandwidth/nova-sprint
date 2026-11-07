package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalLiveHistoryCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	initdb, err := exec.LookPath("initdb")
	if err != nil {
		t.Skip("initdb required")
	}
	dir := filepath.Join(t.TempDir(), "pg")
	if out, err := exec.Command(initdb, "-D", dir, "-U", "nova", "--auth=trust", "--no-locale").CombinedOutput(); err != nil {
		t.Fatalf("initdb: %v: %s", err, out)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	ctl := filepath.Join(filepath.Dir(initdb), "pg_ctl")
	options := fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", port)
	if out, err := exec.Command(ctl, "-D", dir, "-l", filepath.Join(dir, "log"), "-o", options, "-w", "start").CombinedOutput(); err != nil {
		t.Fatalf("postgres: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command(ctl, "-D", dir, "-m", "immediate", "-w", "stop").Run() })
	dsn := fmt.Sprintf("postgres://nova@127.0.0.1:%d/postgres?sslmode=disable", port)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = pg.Apply(ctx, store.Op{ID: "one", Epoch: 1, Actor: "tester", Kind: "test", At: time.Unix(1000, 0), Writes: []store.Write{store.PutStream{Name: "s"}}})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, "CREATE ROLE nova_sprint LOGIN SUPERUSER"); err != nil {
		t.Fatal(err)
	}
	if err = (doctor{creds: credentials{dsn: dsn, password: "fixture-password"}}).History(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "ALTER TABLE sprint.events DISABLE TRIGGER events_append_only; DELETE FROM sprint.events; ALTER TABLE sprint.events ENABLE TRIGGER events_append_only"); err != nil {
		t.Fatal(err)
	}
	if err = (doctor{creds: credentials{dsn: dsn, password: "fixture-password"}}).History(ctx); err == nil {
		t.Fatal("damaged history passed")
	}
}
