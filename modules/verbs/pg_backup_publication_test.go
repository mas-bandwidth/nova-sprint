package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestPublishDumpPreservesBytesAndDoesNotOverwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "backup.sql")
	body := []byte("verified bytes\n")
	if err := publishDump(body, path); err != nil {
		t.Fatal(err)
	}
	if err := publishDump([]byte("other"), path); err == nil {
		t.Fatal("overwrote backup")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("published %q: %v", got, err)
	}
}

func TestPGProcessEnvUsesExplicitPassword(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv)
	t.Setenv("PGPASSWORD", "ambient")
	for _, password := range []string{"", "explicit"} {
		env, err := pgProcessEnv([]string{"PATH=/bin", "PGPASSWORD=ambient"}, "host=example user=backup password=embedded dbname=sprint sslmode=disable", password)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(env, "\n")
		if !strings.Contains(joined, "PGPASSWORD="+password+"\n") || !strings.Contains(joined, "PGPASSFILE=/dev/null") || strings.Contains(joined, "ambient") || strings.Contains(joined, "embedded") {
			t.Fatalf("unexpected environment %q", joined)
		}
	}
}

func TestFunctionalPublishedBackupRoundTrip(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
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
	receipt, err := pg.Apply(ctx, store.Op{ID: "seed", Epoch: 1, Actor: "test", Kind: "seed", At: time.Unix(1000, 0), Writes: []store.Write{store.PutStream{Name: "s"}}})
	if err != nil || !receipt.Accepted {
		t.Fatal(receipt, err)
	}
	backups := PGBackups{DSN: dsn, Directory: t.TempDir(), Version: "v2.0.0", Loader: PGSQLLoader{}}
	info, err := backups.Backup(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := backups.Verify(ctx, info.Path)
	if err != nil || verified.Seq != info.Seq || verified.PrefixDigest != info.PrefixDigest {
		t.Fatalf("verify: %+v %v", verified, err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE target")
	conn.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	targetDSN := "host=" + dir + " dbname=target sslmode=disable"
	archive := NewPGArchive(targetDSN, "", "v2.0.0", t.TempDir(), func(ctx context.Context) (run.RestoreLock, error) {
		return run.OpenRestoreLock(ctx, targetDSN, "", "fixture", 1)
	})
	if err := (&Engine{Archive: archive}).load(ctx, info); err != nil {
		t.Fatal(err)
	}
	snap, seq, digest, _, err := verifyDatabase(ctx, targetDSN, info.Verified.Digest())
	if err != nil || seq != receipt.Seq || digest != info.PrefixDigest {
		t.Fatalf("restore: %+v seq=%d digest=%s err=%v", snap, seq, digest, err)
	}
}

// A second backup at the same epoch and seq (backup, then clear's own) gets
// its own name: the first dump is kept and the second is published.
func TestPublishAtSeqNamesASecondDumpApart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := publishAtSeq([]byte("one\n"), dir, 3, 7)
	if err != nil || filepath.Base(first) != "sprint-e3-s7.sql" {
		t.Fatalf("first %q %v", first, err)
	}
	second, err := publishAtSeq([]byte("two\n"), dir, 3, 7)
	if err != nil || second == first {
		t.Fatalf("second %q %v", second, err)
	}
	if ok, _ := filepath.Match("sprint-e*-s*.sql", filepath.Base(second)); !ok {
		t.Fatalf("second %q is not a backup name the setup walk finds", second)
	}
	for path, want := range map[string]string{first: "one\n", second: "two\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", path, got, err)
		}
	}
}
