package run

import (
	"context"
	"fmt"
	"strings"
)

// RestoreLock is the shared chooser advisory lock, without the bus hook.
// It does not make work on a separate database session atomic with this lock.
type RestoreLock interface {
	Check(context.Context) error
	Restore(ctx context.Context, digest string, apply func(RestoreWriter) error) error
	Close(context.Context) error
}

func OpenRestoreLock(ctx context.Context, dsn, password, host string, pid int) (RestoreLock, error) {
	session, err := connectLockSession(ctx, dsn, password, fmt.Sprintf("nova-sprint restore %s %d", host, pid))
	if err != nil {
		return nil, err
	}
	held, err := session.Try(ctx)
	if err != nil {
		_ = session.Close(ctx)
		return nil, fmt.Errorf("restore: acquire chooser lock: %w", err)
	}
	if !held {
		holder, findErr := session.Holder(ctx)
		_ = session.Close(ctx)
		if findErr != nil {
			return nil, fmt.Errorf("restore: identify chooser: %w", findErr)
		}
		if strings.HasPrefix(holder, "nova-sprint restore ") {
			return nil, fmt.Errorf("SPRINT REFUSED restore: restore lock held by %s: wait for it to finish", holder)
		}
		return nil, fmt.Errorf("SPRINT REFUSED restore: run chooses on %s: stop it first", holder)
	}
	return session, nil
}
