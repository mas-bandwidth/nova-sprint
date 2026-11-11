package run

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ChooserStatus is the observed holder, if any. PID is the process ID in its
// application name; BackendPID is the PostgreSQL session's process ID.
// Version is the holder's build version, the fifth word of the name (""
// when a build that wrote none holds it).
type ChooserStatus struct {
	Held                                      bool
	Operation, Host, ApplicationName, Version string
	PID, BackendPID                           int
	Since                                     time.Time
}

// InspectChooser reads the catalog; it never acquires the chooser lock.
func InspectChooser(ctx context.Context, dsn, password string) (ChooserStatus, error) {
	session, err := connectLockSession(ctx, dsn, password, "nova-sprint show run")
	if err != nil {
		return ChooserStatus{}, err
	}
	defer session.Close(ctx)
	var status ChooserStatus
	err = session.conn.QueryRow(ctx, `SELECT a.application_name, a.backend_start, a.pid FROM pg_locks l
 JOIN pg_stat_activity a ON a.pid=l.pid
 WHERE l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND l.classid=$1::oid AND l.objid=$2::oid AND l.objsubid=2 AND l.granted LIMIT 1`, chooserClass, chooserObject).Scan(&status.ApplicationName, &status.Since, &status.BackendPID)
	if err == pgx.ErrNoRows {
		return status, nil
	}
	if err != nil {
		return ChooserStatus{}, fmt.Errorf("run: inspect chooser: %w", err)
	}
	status.Held = true
	status.parseName()
	return status, nil
}

// NameMax is the longest application name PostgreSQL keeps (NAMEDATALEN
// less one); a longer one is cut there.
const NameMax = 63

// VersionUnknown is a holder's build when its name was cut at NameMax: the
// fifth word, or the words before it, may be partial, so the build is never
// read from it (up's run step never restarts on it, or it would loop).
const VersionUnknown = "unknown"

// parseName reads the operation, host, pid and build from the holder's
// application name.
func (status *ChooserStatus) parseName() {
	parts := strings.Fields(status.ApplicationName)
	if (len(parts) == 4 || len(parts) == 5) && parts[0] == "nova-sprint" && (parts[1] == "run" || parts[1] == "restore") {
		status.Operation = parts[1]
		status.Host = parts[2]
		status.PID, _ = strconv.Atoi(parts[3])
		if len(parts) == 5 {
			status.Version = parts[4]
		}
	}
	if len(status.ApplicationName) >= NameMax {
		status.Version = VersionUnknown
	}
}
