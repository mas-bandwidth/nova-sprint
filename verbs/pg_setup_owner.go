package verbs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// VerifySetupLeaseOwner checks the active setup lock's recorded host and PID.
// This is a correlation check, not authentication; callers must still use the
// scoped credential and revalidate the continuation under the new lease.
func VerifySetupLeaseOwner(ctx context.Context, dsn, password, host string, pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid setup owner")
	}
	app, err := setupIdentity(host, pid)
	if err != nil {
		return false, err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return false, fmt.Errorf("setup owner configuration invalid")
	}
	if password != "" {
		cfg.Password = password
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("setup owner connection unavailable")
	}
	defer closeSetupConnection(conn)
	var found bool
	err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.classid=$1::oid AND l.objid=$2::oid AND l.objsubid=2 AND l.granted AND a.application_name=$3)`, setupLockClass, setupLockObject, app).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("setup owner lookup failed")
	}
	return found, nil
}
