package run

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

// configSchemaFixture is config/schema.sql of the nova-tools module go.mod
// requires, the schema the roster reads. The fixture applies it only to
// chooserTestPG's isolated temporary database.
func configSchemaFixture(t *testing.T) []byte {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/mas-bandwidth/nova-tools/v2").Output()
	if err != nil {
		t.Fatalf("nova-tools module: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "config", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func configExec(t *testing.T, ctx context.Context, c *pgx.Conn, sql string) {
	t.Helper()
	if _, err := c.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionalConfigRosterReadGrantsAndClose(t *testing.T) {
	t.Parallel()
	schema := configSchemaFixture(t)
	dsn := chooserTestPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	configExec(t, ctx, admin, string(schema))
	configExec(t, ctx, admin, `CREATE ROLE nova_sprint LOGIN`)
	configExec(t, ctx, admin, `INSERT INTO config.machines (name,"user",roles,cores,os,arch) VALUES ('bench-a','fixture','bench,runner',12,'linux','amd64'),('service','fixture','services',NULL,'linux','amd64')`)
	configExec(t, ctx, admin, `INSERT INTO config.friends (name,harness,model,width,tiers,enabled) VALUES ('friend','codex','fixture-model',3,'flash,pro',true),('disabled','codex','fixture-model',1,'pro',false)`)
	configExec(t, ctx, admin, `GRANT USAGE ON SCHEMA config TO nova_sprint; GRANT SELECT ON config.machines, config.friends, config.services TO nova_sprint`)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("nova_sprint")
	conn, err := config.Open(ctx, u.String(), "fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewConfigRoster(conn, 8)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := reader.ReadRoster(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Workers) != 1 || got.Workers["bench-a"].Width != 8 || len(got.Workers["bench-a"].Tiers) != 4 || len(got.Kin) != 1 || got.Kin["friend"].Width != 3 || got.Kin["friend"].Model != "fixture-model" || got.Kin["friend"].Harness != "codex" {
		t.Fatalf("roster %+v", got)
	}
	if _, err = conn.Conn().Exec(ctx, `INSERT INTO config.friends(name) VALUES ('forbidden')`); err == nil {
		t.Fatal("read-only role wrote config")
	}
	configExec(t, ctx, admin, `REVOKE SELECT ON config.services FROM nova_sprint`)
	if _, err = reader.ReadRoster(ctx); err == nil {
		t.Fatal("missing service read grant was ignored")
	}
	configExec(t, ctx, admin, `GRANT SELECT ON config.services TO nova_sprint`)
	if _, err = reader.ReadRoster(ctx); err != nil {
		t.Fatalf("reader did not recover after restored grant: %v", err)
	}
	reader.Close()
	if !conn.Conn().IsClosed() {
		t.Fatal("owned config connection remains open")
	}
	if _, err = reader.ReadRoster(ctx); err == nil {
		t.Fatal("read succeeded after close")
	}
}
