package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// These two-key advisory lock values are shared by run and restore, and are
// distinct from the store writer and migration locks.
const chooserClass int32 = 0x4e535052
const chooserObject int32 = 0x52554e

type pgChooser struct{ conn *pgx.Conn }

// OpenChooser opens its own session; it must never return the connection to a pool.
// Its application name carries the holder's host, pid and build version
// (InspectChooser reads it back: up's run step compares the version).
func OpenChooser(ctx context.Context, dsn, password, host string, pid int, version string, bus Hook) (*Chooser, error) {
	session, err := connectLockSession(ctx, dsn, password, strings.TrimSpace(fmt.Sprintf("nova-sprint run %s %d %s", host, pid, version)))
	if err != nil {
		return nil, err
	}
	return acquireChooser(ctx, session, bus)
}
func connectLockSession(ctx context.Context, dsn, password, app string) (*pgChooser, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, edge.Cause("run: PostgreSQL DSN could not be parsed", err)
	}
	if password != "" {
		cfg.Password = password
	}
	cfg.RuntimeParams["application_name"] = app
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("run: connect chooser: %w", ctx.Err())
		}
		return nil, edge.Cause("run: cannot connect to PostgreSQL; check settings and network", err)
	}
	return &pgChooser{conn}, nil
}

func (s *pgChooser) Try(ctx context.Context) (bool, error) {
	var held bool
	err := s.conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1::integer, $2::integer)", chooserClass, chooserObject).Scan(&held)
	return held, err
}
func (s *pgChooser) Holder(ctx context.Context) (string, error) {
	var name string
	var started time.Time
	err := s.conn.QueryRow(ctx, `SELECT a.application_name, a.backend_start FROM pg_locks l
 JOIN pg_stat_activity a ON a.pid = l.pid
 WHERE l.locktype = 'advisory' AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
 AND l.classid = $1::oid AND l.objid = $2::oid AND l.objsubid = 2 AND l.granted LIMIT 1`, chooserClass, chooserObject).Scan(&name, &started)
	if err == pgx.ErrNoRows {
		return "another host (holder just changed)", nil
	}
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "another host (application name unavailable)"
	}
	return describeHolder(name, started), nil
}

func describeHolder(name string, started time.Time) string {
	parts := strings.Fields(name)
	if (len(parts) == 4 || len(parts) == 5) && parts[0] == "nova-sprint" && parts[1] == "run" {
		return fmt.Sprintf("%s (pid %s, since %s)", parts[2], parts[3], started.Local().Format("15:04"))
	}
	return name
}
func (s *pgChooser) Check(ctx context.Context) error {
	var held bool
	err := s.conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = pg_backend_pid()
 AND locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
 AND classid = $1::oid AND objid = $2::oid AND objsubid = 2 AND granted)`, chooserClass, chooserObject).Scan(&held)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("chooser advisory lock is no longer held")
	}
	return nil
}
func (s *pgChooser) Close(_ context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlockErr error
	if !s.conn.IsClosed() {
		var released bool
		unlockErr = s.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1::integer, $2::integer)", chooserClass, chooserObject).Scan(&released)
	}
	closeErr := s.conn.Close(ctx)
	if unlockErr != nil {
		return fmt.Errorf("run: release chooser lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("run: close chooser session: %w", closeErr)
	}
	return nil
}
