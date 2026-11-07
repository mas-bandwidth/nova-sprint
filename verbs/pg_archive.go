package verbs

import (
	"context"
	"fmt"
)

// RestoreLockFactory opens the chooser lock on the target database. Its lease
// must execute the verified dump in that same session and one transaction.
type RestoreLockFactory func(context.Context) (RestoreLease, error)

// PGArchive is the concrete backup/verification adapter; the application
// supplies the same-session chooser lock implementation without a run import.
type PGArchive struct {
	PGBackups
	Lock RestoreLockFactory
}

func NewPGArchive(dsn, password, version, directory string, lock RestoreLockFactory) *PGArchive {
	return &PGArchive{PGBackups: PGBackups{DSN: dsn, Password: password, Directory: directory, Version: version, Loader: PGSQLLoader{}}, Lock: lock}
}
func (p *PGArchive) AcquireRestore(ctx context.Context) (RestoreLease, error) {
	if p == nil || p.Lock == nil {
		return nil, fmt.Errorf("restore lock is unavailable")
	}
	return p.Lock(ctx)
}
