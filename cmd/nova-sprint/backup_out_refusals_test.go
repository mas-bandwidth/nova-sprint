package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// refusalStore is a twin store a backup's source reads: the package's live
// fleet twin (backup_out_live_test.go), so the dump carries the work table.
func refusalStore(t *testing.T) *store.Store {
	t.Helper()
	return liveFleetTwin(t)
}

// refusalBackup is a backupOut with every field working; a test breaks one.
func refusalBackup(t *testing.T, st *store.Store, out string) *backupOut {
	t.Helper()
	for _, bin := range []string{"xz", "split"} {
		_, err := exec.LookPath(bin)
		require.NoError(t, err, "the backup runs the system's %s", bin)
	}
	self, err := os.Executable()
	require.NoError(t, err)
	return &backupOut{
		out: out, partBytes: 600,
		src:   memBackup{st: st},
		twin:  func(context.Context, string) (backupTwin, error) { return &memTwin{names: st.Names}, nil },
		xz:    "xz",
		split: "split",
		scan:  backupScanner{bin: fakeSecrets(t, "nsv_REFUSAL_FAKE_VALUE"), store: t.TempDir(), as: "tester", key: "unused", sops: "/bin/true", self: self},
	}
}

// noDumpSource is a backup source whose dump fails.
type noDumpSource struct{}

func (noDumpSource) Take(context.Context) (uint64, []store.DumpKey, map[string]int, error) {
	return 0, nil, nil, errors.New("the session is gone")
}

// countsOnlyBackup is a backup source whose sprint state cannot be read: it
// embeds only backupSource, so State is not promoted.
type countsOnlyBackup struct{ backupSource }

// wrongCountTwin is a throwaway whose restore holds other counts than the
// source held.
type wrongCountTwin struct{}

func (wrongCountTwin) Restore(context.Context, []store.DumpKey) (store.BackupCounts, error) {
	return store.BackupCounts{Keys: 999, Columns: map[string]int{}}, nil
}
func (wrongCountTwin) Values(context.Context, io.Writer) error { return nil }
func (wrongCountTwin) Library() string                         { return "stub" }
func (wrongCountTwin) Close()                                  {}

// requireNoBackupLeft says a failed backup wrote no --out and left no private
// work directory beside it.
func requireNoBackupLeft(t *testing.T, out string) {
	t.Helper()
	assert.NoDirExists(t, out, "--out was written")
	ents, err := os.ReadDir(filepath.Dir(out))
	require.NoError(t, err)
	for _, e := range ents {
		assert.False(t, strings.HasPrefix(e.Name(), ".nova-sprint-backup-"),
			"a failed backup leaves no work directory: %s", e.Name())
	}
}

// backup --out refuses a regular file beside it: it "cannot be read" and the
// file is left as it was (backup_out.go run, the --out read).
func TestBackupOutRefusesAnOutThatIsARegularFile(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	require.NoError(t, os.WriteFile(out, []byte("not a directory"), 0o644))
	b := refusalBackup(t, st, out)
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be read")
	assert.FileExists(t, out)
	requireNoBackupLeft(t, out)
}

// A source whose dump fails is refused with the error wrapped and nothing
// written (backup_out.go run, "the store gave no dump").
func TestBackupOutRefusesWhenTheSourceGivesNoDump(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.src = noDumpSource{}
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the store gave no dump")
	requireNoBackupLeft(t, out)
}

// xz failing is refused, naming the failed step and the flag that fixes it
// (backup_out.go run, the xz -9 step).
func TestBackupOutRefusesWhenXzFails(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.xz = "/bin/false"
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xz -9 failed")
	assert.Contains(t, err.Error(), "--xz")
	requireNoBackupLeft(t, out)
}

// split failing is refused, naming the failed step and the flag that fixes it
// (backup_out.go run, the split step).
func TestBackupOutRefusesWhenSplitFails(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.split = "/bin/false"
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "split failed")
	assert.Contains(t, err.Error(), "--split")
	requireNoBackupLeft(t, out)
}

// A twin factory that errors is refused as "no throwaway store to restore
// into" with nothing written (backup_out.go run, the twin step).
func TestBackupOutRefusesWhenThereIsNoThrowawayStore(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.twin = func(context.Context, string) (backupTwin, error) { return nil, errors.New("no redis-server") }
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no throwaway store to restore into")
	requireNoBackupLeft(t, out)
}

// A twin whose restore holds other counts than the source held is refused, with
// nothing written (backup_out.go run, the counts check).
func TestBackupOutRefusesWhenTheTwinHoldsOtherCounts(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.twin = func(context.Context, string) (backupTwin, error) { return wrongCountTwin{}, nil }
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the dump restored with other counts than the store held")
	requireNoBackupLeft(t, out)
}

// No secret scan seat is refused "the secret scan did not run" with nothing
// written (backup_out.go run, the scan step).
func TestBackupOutRefusesWhenThereIsNoSecretScanSeat(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.scan = backupScanner{}
	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the secret scan did not run")
	requireNoBackupLeft(t, out)
}

// A source whose sprint state cannot be read is restored on counts alone: the
// result says restore=integrity compared=counts, and the README says the counts
// are not a semantic restore (backup_out.go run, the stateSource check).
func TestBackupOutReportsCountsOnlyWhenTheSourceHasNoStateReader(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	out := filepath.Join(t.TempDir(), "out")
	b := refusalBackup(t, st, out)
	b.src = countsOnlyBackup{memBackup{st: st}}
	res, err := b.run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.line, "restore=integrity compared=counts")
	assert.Contains(t, res.line, "integrity only")
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "the counts are not a semantic restore")
}

// An existing empty --out is replaced and the backup succeeds, its files under
// it (backup_out.go run, the final rename).
func TestBackupOutReplacesAnEmptyOutAndSucceeds(t *testing.T) {
	t.Parallel()
	st := refusalStore(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	require.NoError(t, os.Mkdir(out, 0o755))
	b := refusalBackup(t, st, out)
	res, err := b.run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.line, "BACKUP OK out="+out+" ")
	assert.FileExists(t, filepath.Join(out, "README.md"))
	assert.FileExists(t, filepath.Join(out, "SHA256SUMS"))
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range ents {
		assert.False(t, strings.HasPrefix(e.Name(), ".nova-sprint-backup-"),
			"a finished backup leaves no work directory: %s", e.Name())
	}
}
