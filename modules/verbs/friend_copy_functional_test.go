package verbs

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestFunctionalCopyFriendSettings: on a real store, up's copy reads
// config.friends' width and tiers only while the columns exist, writes each
// friend's once as friend:<name> (never over a friend set), and copies
// nothing the second time; with no config.friends, or one without the
// columns, it copies nothing and fails nothing.
func TestFunctionalCopyFriendSettings(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, dsn := startBackupPG(ctx, t)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	at := time.Unix(1000, 0)
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	copyOnce := func() []string {
		t.Helper()
		lines, err := CopyFriendSettings(ctx, conn, pg, "up", at)
		if err != nil {
			t.Fatal(err)
		}
		return lines
	}
	if lines := copyOnce(); lines != nil {
		t.Fatalf("no config schema: %q", lines)
	}
	exec(`CREATE SCHEMA config; CREATE TABLE config.friends (name text PRIMARY KEY, harness text NOT NULL DEFAULT '')`)
	exec(`INSERT INTO config.friends (name) VALUES ('stella')`)
	if lines := copyOnce(); lines != nil {
		t.Fatalf("no width and tiers columns: %q", lines)
	}
	exec(`ALTER TABLE config.friends ADD COLUMN width integer NOT NULL DEFAULT 1, ADD COLUMN tiers text NOT NULL DEFAULT 'flash,frontier,heavy,pro'`)
	exec(`INSERT INTO config.friends (name, width, tiers) VALUES ('emma', 4, 'heavy,pro'), ('alex', 2, 'pro')`)
	r, err := pg.Apply(ctx, store.Op{ID: "set-alex", Epoch: 1, Actor: "carol", Kind: "friend-set", At: at, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "friend:alex", Body: `{"width":5,"tiers":["flash"]}`}}})
	if err != nil || !r.Accepted {
		t.Fatal(r, err)
	}
	want := []string{"friend emma: width 4 tiers pro,heavy copied from nova-config", "friend stella: width 1 tiers flash,pro,heavy,frontier copied from nova-config"}
	if lines := copyOnce(); !reflect.DeepEqual(lines, want) {
		t.Fatalf("first copy %q", lines)
	}
	if lines := copyOnce(); lines != nil {
		t.Fatalf("second copy %q", lines)
	}
	vs, err := pg.NewestData(ctx, roster.FriendPrefix)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, v := range vs {
		got[v.Name] = v.Body + " by " + v.Actor
	}
	if !reflect.DeepEqual(got, map[string]string{
		"friend:alex":   `{"width":5,"tiers":["flash"]} by carol`,
		"friend:emma":   `{"width":4,"tiers":["pro","heavy"]} by up`,
		"friend:stella": `{"width":1,"tiers":["flash","pro","heavy","frontier"]} by up`,
	}) {
		t.Fatalf("stored %v", got)
	}
}
