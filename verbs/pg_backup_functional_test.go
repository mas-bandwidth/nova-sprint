package verbs

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalBackupRoundTrip(t *testing.T) {
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir, dsn := startBackupPG(ctx, t)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := pg.Apply(ctx, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: time.Unix(1000, 0), Writes: []store.Write{store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutRow{Name: "worker", Session: "fixture", Role: "worker"}, store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "task"}, store.PutCard{ID: "p.w1", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Holder: "worker", Attempt: 1, Brief: "task"}}})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	b := PGBackups{DSN: dsn, Directory: t.TempDir(), Version: "v2.0.0", Loader: PGSQLLoader{}}
	info, err := b.Backup(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := EventPrefixDigest(ctx, pg, info.Seq)
	if err != nil || prefix != info.PrefixDigest {
		t.Fatalf("prefix mismatch %s %s %v", prefix, info.PrefixDigest, err)
	}
	if os.Getenv("NOVA_DUMP_SHAPES") == "1" {
		data, _ := os.ReadFile(info.Path)
		_ = ParseDump(bytes.NewReader(data), func(stmt string) error {
			fields := strings.Fields(stripComments(stmt))
			if len(fields) > 4 {
				fields = fields[:4]
			}
			t.Logf("shape %s", strings.Join(fields, " "))
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stripComments(stmt))), "INSERT ") {
				t.Logf("fixture %s", stripComments(stmt))
			}
			return nil
		})
	}
	if info.Epoch != 1 || info.Seq != r.Seq {
		t.Fatal(info)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "CREATE DATABASE target"); err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	conn.Close(ctx)
	targetDSN := "host=" + dir + " dbname=target sslmode=disable"
	archive := NewPGArchive(targetDSN, "", "v2.0.0", t.TempDir(), func(ctx context.Context) (run.RestoreLock, error) {
		return run.OpenRestoreLock(ctx, targetDSN, "", "fixture", 1)
	})
	if err := (&Engine{Archive: archive}).load(ctx, info); err != nil {
		t.Fatal(err)
	}

	target, targetDigest, _, err := verifyDatabase(ctx, targetDSN)
	if err != nil || target.Seq != info.Seq || targetDigest != info.PrefixDigest || len(target.Cards) != 2 {
		t.Fatal(target, targetDigest, err)
	}
	verified, err := b.Verify(ctx, info.Path)
	if err != nil || verified.Epoch != 1 || verified.Seq != r.Seq {
		t.Fatal(verified, err)
	}
	original, err := os.ReadFile(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, attack := range []string{"INSERT INTO sprint.events VALUES (pg_sleep(1));\n", "CREATE TABLE public.hijack (v text);\n", "CREATE FUNCTION sprint.bad() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n"} {
		index := bytes.LastIndex(original, []byte("\\unrestrict "))
		if index < 0 {
			t.Fatal("missing pg_dump wrapper")
		}
		forged := append(append(append([]byte{}, original[:index]...), []byte(attack)...), original[index:]...)
		path := filepath.Join(t.TempDir(), "forged.sql")
		if err = os.WriteFile(path, forged, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = b.Verify(ctx, path); err == nil {
			t.Fatalf("accepted forged statement %q", attack)
		}
	}
	if err = os.WriteFile(info.Path, []byte("not a dump"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Verify(ctx, info.Path); err == nil {
		t.Fatal("corrupt dump verified")
	}
}

// startBackupPG starts a throwaway PostgreSQL on a socket in its own
// directory and returns the directory and its postgres database's dsn. Not
// t.TempDir: its path, with the test's name, is too long for a socket.
func startBackupPG(ctx context.Context, t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "nova-sprint-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) // ignored: a temp dir; runs after the stop below
	data := filepath.Join(dir, "pg")
	if err := runPG(ctx, "", "initdb", "-D", data, "-A", "trust", "-E", "UTF8", "--no-locale", "--no-instructions"); err != nil { // UTF8, as a store is
		t.Fatal(err)
	}
	if err := runPG(ctx, "", "pg_ctl", "-D", data, "-l", filepath.Join(dir, "postgres.log"), "-o", verifyServerOptions(dir), "-w", "start"); err != nil {
		t.Fatal(err, logTail(filepath.Join(dir, "postgres.log")))
	}
	t.Cleanup(func() { _ = runPG(context.Background(), "", "pg_ctl", "-D", data, "-m", "immediate", "-w", "stop") })
	return dir, "host=" + dir + " dbname=postgres sslmode=disable"
}

// round25Hash is the live store's schema hash on 2026-11-07, the deployed
// line's round 25 (store's round25Hash, for its testdata/schema-round25.sql).
const round25Hash = "9c9be7f5f7ae333043ff667a93f473d1bdc6c071bfad043f1e8d9ca1f4433d0e"

// The live store's upgrade path (PR 641, F1): a round 25 store, Migrated,
// holds a consumer with a model and a usage; its backup verifies (loaded
// into a new store, the rebuild equals the snapshot) and loads into a fresh
// store with the source's snapshot, model and usage each in its own column.
// pg_dump --inserts is positional, so a new store's columns must be in the
// migrated store's order.
func TestFunctionalBackupMigratedRound25(t *testing.T) {
	if _, err := exec.LookPath("initdb"); err != nil {
		t.Skip("requires PostgreSQL's initdb on PATH")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir, dsn := startBackupPG(ctx, t)
	old, err := os.ReadFile("../store/testdata/schema-round25.sql")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, q := range []string{string(old), "UPDATE sprint.meta SET hash = '" + round25Hash + "' WHERE id = 1", "CREATE DATABASE target"} {
		if _, err = conn.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// The stop is logged: a round 25 store starts running, and the rebuild
	// starts never started (store.FreshStop), so a log with no stop in it
	// would not rebuild to the store's running state.
	r, err := pg.Apply(ctx, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: time.Unix(1000, 0), Writes: []store.Write{
		store.PutStream{Name: "s", Repo: "owner/repo", Base: "main", Pos: 1}, store.PutRow{Name: "worker", Session: "fixture", Role: "worker"},
		store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "pro", Brief: "task"},
		store.PutCard{ID: "p.w1", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Holder: "worker", Attempt: 1, Brief: "task", Model: "inception/mercury-3"},
		store.SetCardUsage{Card: "p.w1", Usage: "input=3 billing=api charged=0.000005"}, store.SetStopped{Stopped: true, Reason: "pause"}}})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	source, err := pg.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := PGBackups{DSN: dsn, Directory: t.TempDir(), Version: "v2.0.1", Loader: PGSQLLoader{}}
	info, err := b.Backup(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	targetDSN := "host=" + dir + " dbname=target sslmode=disable"
	archive := NewPGArchive(targetDSN, "", "v2.0.1", t.TempDir(), func(ctx context.Context) (run.RestoreLock, error) {
		return run.OpenRestoreLock(ctx, targetDSN, "", "fixture", 1)
	})
	if err = (&Engine{Archive: archive}).load(ctx, info); err != nil {
		t.Fatal(err)
	}
	target, _, _, err := verifyDatabase(ctx, targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := target.Cards["p.w1"], source.Cards["p.w1"]; got.Model != "inception/mercury-3" || got.Usage != want.Usage || !reflect.DeepEqual(target.Cards, source.Cards) {
		t.Fatalf("the restored consumer is %+v, want %+v", got, want)
	}
}
