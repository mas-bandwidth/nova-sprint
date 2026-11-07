package verbs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestFunctionalBackupRoundTrip(t *testing.T) {
	t.Parallel()
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
	if err := runPG(ctx, "", "pg_ctl", "-D", data, "-o", "-k "+dir+" -c listen_addresses=''", "-w", "start"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runPG(context.Background(), "", "pg_ctl", "-D", data, "-m", "immediate", "-w", "stop") })
	dsn := "host=" + dir + " dbname=postgres sslmode=disable"
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
