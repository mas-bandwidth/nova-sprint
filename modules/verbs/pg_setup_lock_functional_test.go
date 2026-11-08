package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestFunctionalSetupLock(t *testing.T) {
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
	if err := runPG(ctx, "", "pg_ctl", "-D", data, "-l", filepath.Join(dir, "postgres.log"), "-o", "-k "+dir+" -c listen_addresses=''", "-w", "start"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runPG(context.Background(), "", "pg_ctl", "-D", data, "-m", "immediate", "-w", "stop") })
	lock := PGSetupLock{DSN: "host=" + dir + " dbname=postgres sslmode=disable", Host: "host-a", PID: 42}
	lease, err := lock.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.Connect(ctx, lock.DSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE ROLE sprint_check LOGIN"); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	childDSN := lock.DSN + " user=sprint_check"
	found, err := VerifySetupLeaseOwner(ctx, childDSN, "", "host-a", 42)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	found, err = VerifySetupLeaseOwner(ctx, childDSN, "", "host-a", 43)
	if err != nil || found {
		t.Fatal(found, err)
	}
	_, err = lock.Acquire(ctx)
	var op *OperationError
	if !errors.As(err, &op) || op.Problem != "setup already running on host-a (pid 42)" {
		t.Fatalf("refusal: %#v", err)
	}
	if err = lease.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	found, err = VerifySetupLeaseOwner(ctx, childDSN, "", "host-a", 42)
	if err != nil || found {
		t.Fatal(found, err)
	}
	lease, err = lock.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	observed, stopMonitor := lease.(SetupMonitor).Monitor(ctx)
	defer stopMonitor()
	pid := lease.(*pgSetupLease).conn.PgConn().PID()
	killer, err := pgx.Connect(ctx, lock.DSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = killer.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	_ = killer.Close(ctx)
	select {
	case <-observed.Done():
		if context.Cause(observed) == nil {
			t.Fatal("missing lock-loss cause")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock loss was not observed")
	}
	_ = lease.Close()
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err = lock.Acquire(canceled); err == nil {
		t.Fatal("cancelled lock acquired")
	}
}
