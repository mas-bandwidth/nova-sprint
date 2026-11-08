package verbs

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/modules/run"
)

// Archive is backup and restore: a backup is returned only after a
// throwaway restore rebuilt its event log and matched the restored
// snapshot (Verify); a restore runs under the chooser lock, in its session.
type Archive interface {
	Backup(ctx context.Context, out string) (BackupInfo, error)
	Verify(ctx context.Context, path string) (BackupInfo, error)
	AcquireRestore(ctx context.Context) (run.RestoreLock, error)
}

// BackupInfo is a verified dump: where it is, the epoch and seq it holds,
// and the statements that passed verification.
type BackupInfo struct {
	Path         string
	PrefixDigest string
	SchemaHash   string
	Verified     *VerifiedDump
	Epoch        int
	Seq          int64
}

// PGArchive is the Archive over PostgreSQL; Lock opens the chooser lock on
// the target database (run.OpenRestoreLock).
type PGArchive struct {
	PGBackups
	Lock func(context.Context) (run.RestoreLock, error)
}

// NewPGArchive is the binary's archive.
func NewPGArchive(dsn, password, version, directory string, lock func(context.Context) (run.RestoreLock, error)) *PGArchive {
	return &PGArchive{PGBackups: PGBackups{DSN: dsn, Password: password, Directory: directory, Version: version, Loader: PGSQLLoader{}}, Lock: lock}
}

// AcquireRestore takes the chooser lock for a restore.
func (p *PGArchive) AcquireRestore(ctx context.Context) (run.RestoreLock, error) {
	return p.Lock(ctx)
}
