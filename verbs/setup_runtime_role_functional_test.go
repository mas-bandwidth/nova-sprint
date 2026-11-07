package verbs

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalSprintRoleSCRAM(t *testing.T) {
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	data := filepath.Join(dir, "pg")
	if err := runPG(ctx, "", "initdb", "-D", data, "-A", "trust", "--no-instructions"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "pg_hba.conf"), []byte("local all all trust\nhost all all 127.0.0.1/32 scram-sha-256\n"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	opts := fmt.Sprintf("-k %s -p %d -c listen_addresses=127.0.0.1", dir, port)
	if err = runPG(ctx, "", "pg_ctl", "-D", data, "-l", filepath.Join(dir, "pg.log"), "-o", opts, "-w", "start"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runPG(context.Background(), "", "pg_ctl", "-D", data, "-m", "immediate", "-w", "stop") })
	socket := "host=" + dir + fmt.Sprintf(" port=%d dbname=postgres sslmode=disable", port)
	super, err := pgx.Connect(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer super.Close(ctx)
	if _, err = super.Exec(ctx, "CREATE ROLE nova_admin CREATEDB CREATEROLE LOGIN PASSWORD 'fixture-admin'; ALTER DATABASE postgres OWNER TO nova_admin"); err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("postgres://nova_admin@127.0.0.1:%d/postgres?sslmode=disable", port)
	if err = ProvisionSprintRole(ctx, dsn, "fixture-admin", "fixture-sprint"); err != nil {
		t.Fatal(err)
	}
	var canSet bool
	if err = super.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_auth_members WHERE roleid='nova_sprint'::regrole AND member='nova_admin'::regrole AND set_option)`).Scan(&canSet); err != nil || canSet {
		t.Fatal("temporary role grant persisted", err)
	}
	var hash1, hash2 string
	if err = super.QueryRow(ctx, "SELECT rolpassword FROM pg_authid WHERE rolname='nova_sprint'").Scan(&hash1); err != nil {
		t.Fatal(err)
	}
	if err = ProvisionSprintRole(ctx, dsn, "fixture-admin", "fixture-sprint"); err != nil {
		t.Fatal(err)
	}
	if err = super.QueryRow(ctx, "SELECT rolpassword FROM pg_authid WHERE rolname='nova_sprint'").Scan(&hash2); err != nil || hash1 != hash2 {
		t.Fatal("repeated setup changed credential", err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.User = "nova_sprint"
	cfg.Password = "fixture-sprint"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("SCRAM login failed")
	}
	_ = conn.Close(ctx)
	t.Setenv("NOVA_PG_DSN", dsn)
	t.Setenv(sprintPasswordName, "fixture-sprint")
	t.Setenv("NOVA_SPRINT_BACKUP_DIR", filepath.Join(dir, "backups"))
	t.Setenv("PGPASSWORD", "wrong-ambient-fixture")
	ready, err := SprintStoreChild(ctx, false, "v2.0.0")
	if err != nil || ready {
		t.Fatal("unmigrated store appeared ready", err)
	}
	ready, err = SprintStoreChild(ctx, true, "v2.0.0")
	if err != nil || !ready {
		t.Fatal("store setup failed", err)
	}
	ready, err = SprintStoreChild(ctx, false, "v2.0.0")
	if err != nil || !ready {
		t.Fatal("store repeat failed", err)
	}
	sprintURL, err := sprintDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pg, err := store.Open(ctx, sprintURL, "fixture-sprint")
	if err != nil {
		t.Fatal("open failed")
	}
	defer pg.Close()
	rec, err := pg.Apply(ctx, store.Op{ID: "test-store", Epoch: 1, Actor: "test", Kind: "test", At: time.Unix(1000, 0), Writes: []store.Write{store.PutStream{Name: "s", Repo: "o/r", Base: "main", Pos: 1}}})
	if err != nil || !rec.Accepted {
		t.Fatal("test write failed", err)
	}
	ready, err = SprintStoreChild(ctx, false, "v2.0.0")
	if err != nil || !ready {
		t.Fatal("later event invalidated backup", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "backups", "attempt-*", "sprint-e*-s*.sql"))
	if err != nil || len(files) != 1 {
		t.Fatal("initial backup missing", err)
	}
	if err = os.WriteFile(files[0], []byte("corrupt fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ready, err = SprintStoreChild(ctx, true, "v2.0.0")
	if err != nil || !ready {
		t.Fatal("backup recovery failed", err)
	}
	files, err = filepath.Glob(filepath.Join(dir, "backups", "attempt-*", "sprint-e*-s*.sql"))
	if err != nil || len(files) != 2 {
		t.Fatal("old backup evidence not preserved", err)
	}
}
