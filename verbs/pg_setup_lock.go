package verbs

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// PGSetupLock holds a dedicated PostgreSQL session for the whole setup walk.
// This key is distinct from the chooser and store migration locks. See tla/Setup.tla.
type PGSetupLock struct {
	DSN, Password, Host string
	PID                 int
}

const setupLockClass int32 = 0x6e737570
const setupLockObject int32 = 0x73657475

// SetupMonitor cancels in-flight setup work when the lock connection is lost.
// External operations may still complete concurrently with the first detection.
type SetupMonitor interface {
	Monitor(context.Context) (context.Context, context.CancelFunc)
}

type pgSetupLease struct {
	conn *pgx.Conn
	mu   sync.Mutex
}

func (p PGSetupLock) Acquire(ctx context.Context) (SetupLease, error) {
	if strings.TrimSpace(p.DSN) == "" {
		return nil, &OperationError{Problem: "NOVA_PG_DSN is missing", Fix: "nova-sprint check", Exit: 1}
	}
	cfg, err := pgx.ParseConfig(p.DSN)
	if err != nil {
		return nil, fmt.Errorf("setup lock connection configuration invalid")
	}
	if p.Password != "" {
		cfg.Password = p.Password
	}
	host := p.Host
	if host == "" {
		host, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}
	if host == "" || strings.IndexFunc(host, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_')
	}) >= 0 {
		return nil, fmt.Errorf("invalid setup host")
	}
	pid := p.PID
	if pid == 0 {
		pid = os.Getpid()
	}
	app := fmt.Sprintf("nova-sprint up %s %d", host, pid)
	if pid <= 0 || len(app) > 63 {
		return nil, fmt.Errorf("invalid setup identity")
	}
	cfg.RuntimeParams["application_name"] = app
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("setup lock connection unavailable")
	}
	var acquired bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1, $2)", setupLockClass, setupLockObject).Scan(&acquired)
	if err != nil {
		_ = closeSetupConnection(conn)
		return nil, fmt.Errorf("setup lock unavailable")
	}
	if !acquired {
		err = setupHolder(ctx, conn)
		_ = closeSetupConnection(conn)
		return nil, err
	}
	return &pgSetupLease{conn: conn}, nil
}

func setupHolder(ctx context.Context, conn *pgx.Conn) error {
	var app string
	err := conn.QueryRow(ctx, `SELECT a.application_name FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.classid=$1::oid AND l.objid=$2::oid AND l.objsubid=2 AND l.granted LIMIT 1`, setupLockClass, setupLockObject).Scan(&app)
	fields := strings.Fields(app)
	if err != nil || len(fields) != 4 || fields[0] != "nova-sprint" || fields[1] != "up" {
		return fmt.Errorf("setup lock holder unavailable")
	}
	pid, err := strconv.Atoi(fields[3])
	if err != nil || pid <= 0 {
		return fmt.Errorf("setup lock holder unavailable")
	}
	return &OperationError{Problem: fmt.Sprintf("setup already running on %s (pid %d)", fields[2], pid), Fix: "wait for it to finish", Exit: 1}
}

func (p *pgSetupLease) Check(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn.IsClosed() {
		return fmt.Errorf("setup lock lost")
	}
	if err := p.conn.Ping(ctx); err != nil {
		return fmt.Errorf("setup lock lost")
	}
	return nil
}
func (p *pgSetupLease) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return closeSetupConnection(p.conn)
}

func (p *pgSetupLease) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	monitored, cancel := context.WithCancelCause(ctx)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitored.Done():
				return
			case <-ticker.C:
				probe, stop := context.WithTimeout(monitored, time.Second)
				err := p.Check(probe)
				stop()
				if err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return monitored, func() { cancel(nil) }
}

func closeSetupConnection(conn *pgx.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Close(ctx)
}
