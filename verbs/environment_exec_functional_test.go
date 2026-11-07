package verbs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalEnvironmentSchemaIdentity(t *testing.T) {
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	data := filepath.Join(dir, "pg")
	if err := runPG(ctx, "", "initdb", "-D", data, "-A", "trust", "--no-instructions"); err != nil {
		t.Fatal(err)
	}
	if err := runPG(ctx, "", "pg_ctl", "-D", data, "-l", filepath.Join(dir, "pg.log"), "-o", "-k "+dir+" -c listen_addresses=''", "-w", "start"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runPG(context.Background(), "", "pg_ctl", "-D", data, "-m", "immediate", "-w", "stop") })
	dsn := "host=" + dir + " dbname=postgres sslmode=disable"
	e := ExecEnvironment{DSN: dsn}
	if ok, err := e.Schema(ctx); ok || err != nil {
		t.Fatal(ok, err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.Schema(ctx); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err = conn.Exec(ctx, "UPDATE sprint.meta SET hash='different'"); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.Schema(ctx); ok || err == nil {
		t.Fatal(ok, err)
	}
}
