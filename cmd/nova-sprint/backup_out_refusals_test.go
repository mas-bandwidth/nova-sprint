package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// refusalBackupOut builds a backupOut that would succeed against st, for one
// test to break a single field of it (backup_out.go, backupOut).
func refusalBackupOut(t *testing.T, st *store.Store) (*backupOut, string) {
	t.Helper()
	for _, bin := range []string{"xz", "split"} {
		_, err := exec.LookPath(bin)
		require.NoError(t, err, "the backup runs the system's %s", bin)
	}
	self, err := os.Executable()
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "backup")
	return &backupOut{
		out: out, partBytes: 600, src: memBackup{st: st}, xz: "xz", split: "split",
		twin: func(context.Context, string) (backupTwin, error) { return &memTwin{names: st.Names}, nil },
		scan: backupScanner{bin: fakeSecrets(t, "nsv_FAKE_FOR_REFUSAL"), store: t.TempDir(), as: "tester", key: "unused", sops: "/bin/true", self: self},
	}, out
}

// requireBackupRefused asserts a refused run left nothing: the error names
// words, --out does not exist and no private work directory is beside it.
func requireBackupRefused(t *testing.T, out string, err error, words string) {
	t.Helper()
	require.Error(t, err)
	assert.Contains(t, err.Error(), words)
	_, serr := os.Stat(out)
	assert.True(t, os.IsNotExist(serr), "--out is not left")
	requireNoWorkDir(t, out)
}

// requireNoWorkDir asserts no .nova-sprint-backup-* directory is beside --out.
func requireNoWorkDir(t *testing.T, out string) {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(filepath.Dir(out), ".nova-sprint-backup-*"))
	require.NoError(t, err)
	assert.Empty(t, left, "no work directory is left beside --out")
}

// dumplessBackup is a backup source whose Take fails.
type dumplessBackup struct{ err error }

func (d dumplessBackup) Take(context.Context) (uint64, []store.DumpKey, map[string]int, error) {
	return 0, nil, nil, d.err
}

// wrongCountsTwin is a throwaway whose restore reports other counts than the
// dump held.
type wrongCountsTwin struct{ memTwin }

func (w *wrongCountsTwin) Restore(ctx context.Context, keys []store.DumpKey) (store.BackupCounts, error) {
	if _, err := w.memTwin.Restore(ctx, keys); err != nil {
		return store.BackupCounts{}, err
	}
	return store.BackupCounts{Keys: 999999}, nil
}

// countsOnlyBackup is a backup source with no state reader: only Take.
type countsOnlyBackup struct{ inner memBackup }

func (c countsOnlyBackup) Take(ctx context.Context) (uint64, []store.DumpKey, map[string]int, error) {
	return c.inner.Take(ctx)
}

// TestBackupRefusesAnOutThatIsARegularFile pins backupOut.run's first step
// (backup_out.go): a --out that is a regular file cannot be read.
func TestBackupRefusesAnOutThatIsARegularFile(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	require.NoError(t, os.WriteFile(out, []byte("not a backup\n"), 0o644))
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be read")
	fi, serr := os.Stat(out)
	require.NoError(t, serr, "the file is left as it was")
	assert.False(t, fi.IsDir())
	requireNoWorkDir(t, out)
}

// TestBackupRefusesWhenTheStoreGivesNoDump pins backupOut.run's dump step
// (backup_out.go): a source that gives no dump is wrapped.
func TestBackupRefusesWhenTheStoreGivesNoDump(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	cause := errors.New("the twin went away")
	b.src = dumplessBackup{err: cause}
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the store gave no dump")
	assert.ErrorIs(t, err, cause, "the dump error is wrapped")
	requireBackupRefused(t, out, err, "the store gave no dump")
}

// TestBackupRefusesWhenXzFailsAndNamesTheFlag pins backupOut.run's xz step
// (backup_out.go): a failed xz is refused naming --xz.
func TestBackupRefusesWhenXzFailsAndNamesTheFlag(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	b.xz = "/bin/false"
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xz -9 failed")
	requireBackupRefused(t, out, err, "--xz")
}

// TestBackupRefusesWhenSplitFailsAndNamesTheFlag pins backupOut.run's split
// step (backup_out.go): a failed split is refused naming --split.
func TestBackupRefusesWhenSplitFailsAndNamesTheFlag(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	b.split = "/bin/false"
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "split failed")
	requireBackupRefused(t, out, err, "--split")
}

// TestBackupRefusesWhenNoThrowawayStoreCanBeMade pins backupOut.run's twin
// step (backup_out.go): a twin factory that errors is refused.
func TestBackupRefusesWhenNoThrowawayStoreCanBeMade(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	b.twin = func(context.Context, string) (backupTwin, error) {
		return nil, errors.New("the throwaway refused to start")
	}
	_, err := b.run(context.Background())
	require.Error(t, err)
	requireBackupRefused(t, out, err, "no throwaway store to restore into")
}

// TestBackupRefusesWhenTheTwinRestoresOtherCounts pins backupOut.run's restore
// check (backup_out.go): a twin whose restore holds other counts is refused.
func TestBackupRefusesWhenTheTwinRestoresOtherCounts(t *testing.T) {
	t.Parallel()
	st := liveFleetTwin(t)
	b, out := refusalBackupOut(t, st)
	b.twin = func(context.Context, string) (backupTwin, error) {
		return &wrongCountsTwin{memTwin{names: st.Names}}, nil
	}
	_, err := b.run(context.Background())
	require.Error(t, err)
	requireBackupRefused(t, out, err, "the dump restored with other counts than the store held")
}

// TestBackupRefusesWhenTheSecretScanDoesNotRun pins backupOut.run's scan step
// (backup_out.go): no secret scan seat is refused.
func TestBackupRefusesWhenTheSecretScanDoesNotRun(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	b.scan = backupScanner{}
	_, err := b.run(context.Background())
	require.Error(t, err)
	requireBackupRefused(t, out, err, "the secret scan did not run")
}

// TestBackupWithoutAStateReaderSaysCountsOnly pins backupOut.run's integrity
// level (backup_out.go): a source with no state reader is compared on counts
// only, and the README says the counts are not a semantic restore.
func TestBackupWithoutAStateReaderSaysCountsOnly(t *testing.T) {
	t.Parallel()
	st := liveFleetTwin(t)
	b, out := refusalBackupOut(t, st)
	b.src = countsOnlyBackup{memBackup{st: st}}
	res, err := b.run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.line, "restore="+store.RestoreIntegrity)
	assert.Contains(t, res.line, "compared=counts")
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "the counts are not a semantic restore")
}

// TestBackupReplacesAnExistingEmptyOut pins backupOut.run's last step
// (backup_out.go): an existing empty --out is replaced and the backup succeeds.
func TestBackupReplacesAnExistingEmptyOut(t *testing.T) {
	t.Parallel()
	b, out := refusalBackupOut(t, liveFleetTwin(t))
	require.NoError(t, os.Mkdir(out, 0o755))
	res, err := b.run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.line, "BACKUP OK out="+out)
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	require.NoError(t, err)
	assert.NotEmpty(t, readme)
}
