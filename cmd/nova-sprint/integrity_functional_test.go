package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/pgbin"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestFunctionalLiveHistoryCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	initdb, err := exec.LookPath("initdb")
	if err != nil {
		t.Skip("initdb required")
	}
	dsn := pgbin.Start(t, pgbin.Options{Bin: filepath.Dir(initdb), Stop: "immediate"}).DSN
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
